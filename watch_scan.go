package mygo

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/egoist/mygo/internal/platform"
)

type fileWatchScan struct {
	w        *FileWatcher
	root     *os.Root
	identity os.FileInfo
	targets  map[string]platform.FileWatchTarget
	ids      map[uint64]string
	next     uint64
	races    map[string]bool
}

func newFileWatchScan(w *FileWatcher) *fileWatchScan {
	return &fileWatchScan{w: w, targets: make(map[string]platform.FileWatchTarget), ids: make(map[uint64]string)}
}
func (s *fileWatchScan) close() {
	if s.root != nil {
		s.root.Close()
	}
}
func (s *fileWatchScan) add(path string, info os.FileInfo) error {
	if old, ok := s.targets[path]; ok {
		if os.SameFile(old.Info, info) {
			return nil
		}
		s.remove(path)
	}
	s.next++
	target := platform.FileWatchTarget{ID: s.next, Path: filepath.Join(s.w.root, filepath.FromSlash(path)), Directory: info.IsDir(), Info: info}
	ack := make(chan error, 1)
	if !postMain(func() {
		select {
		case <-s.w.ctx.Done():
			ack <- context.Canceled
		default:
			s.w.native.Add(target, func(e error) { ack <- e })
		}
	}) {
		return errLoopStopped
	}
	select {
	case err := <-ack:
		if err != nil {
			return &os.PathError{Op: "watch", Path: target.Path, Err: err}
		}
	case <-s.w.ctx.Done():
		return s.w.ctx.Err()
	}
	s.targets[path] = target
	s.ids[target.ID] = path
	return nil
}
func (s *fileWatchScan) remove(path string) {
	t := s.targets[path]
	delete(s.targets, path)
	delete(s.ids, t.ID)
	postMain(func() { s.w.native.Remove(t.ID) })
}
func (s *fileWatchScan) scan() (map[string]os.FileInfo, error) {
	if err := s.w.ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Lstat(s.w.root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, &os.PathError{Op: "watch", Path: s.w.root, Err: errWatchRootChanged}
	}
	if s.root == nil {
		s.identity = info
		s.root, err = os.OpenRoot(s.w.root)
		if err != nil {
			return nil, err
		}
	} else if !os.SameFile(info, s.identity) {
		return nil, &os.PathError{Op: "watch", Path: s.w.root, Err: errWatchRootChanged}
	}
	// ReadDirectoryChangesW observes children, not the watched directory itself.
	// Enroll a private parent sentinel; its sibling names are never exposed.
	if runtime.GOOS == "windows" {
		parent := filepath.Dir(s.w.root)
		if parent == s.w.root {
			return nil, &os.PathError{Op: "watch", Path: s.w.root, Err: platform.ErrUnsupported}
		}
		parentInfo, e := os.Lstat(parent)
		if e != nil {
			return nil, e
		}
		if e = s.add("..", parentInfo); e != nil {
			return nil, e
		}
	}
	result := make(map[string]os.FileInfo)
	s.races = make(map[string]bool)
	var visit func(string, os.FileInfo) error
	visit = func(path string, info os.FileInfo) (visitErr error) {
		defer func() {
			if path != "." && errors.Is(visitErr, fs.ErrNotExist) {
				// Stat/ReadDir on an already opened directory can also race removal.
				for p := range result {
					if p == path || strings.HasPrefix(p, path+"/") {
						delete(result, p)
					}
				}
				s.races[filepath.ToSlash(filepath.Dir(path))] = true
				visitErr = nil
			}
			if visitErr == nil && len(s.races) > s.w.opts.MaxEntries {
				visitErr = ErrWatchLimit
			}
		}()
		if err := s.w.ctx.Err(); err != nil {
			return err
		}
		if len(result) >= s.w.opts.MaxEntries {
			return ErrWatchLimit
		}
		// Enrollment crosses the main-thread/native boundary. Ordinary atomic saves
		// and deletions during that round trip must not terminate a healthy watch.
		// Retry replacements a bounded number of times, retaining an invalidation
		// hint even during initial enumeration when there is no previous snapshot.
		var dir *os.File
		stable := false
		for attempt := 0; attempt < 3; attempt++ {
			var addErr error
			if !fileWatchLeaf(info) && (info.Mode().IsRegular() || info.IsDir()) {
				addErr = s.add(path, info)
			} else if _, enrolled := s.targets[path]; enrolled {
				// A replacement may now be a symlink or special-file leaf.
				s.remove(path)
			}
			if errors.Is(addErr, fs.ErrPermission) || errors.Is(addErr, platform.ErrFileWatchOverflow) || errors.Is(addErr, platform.ErrUnsupported) || errors.Is(addErr, context.Canceled) || errors.Is(addErr, context.DeadlineExceeded) {
				return addErr
			}
			checked, err := s.root.Lstat(filepath.FromSlash(path))
			if path != "." && (errors.Is(err, fs.ErrNotExist) || errors.Is(addErr, fs.ErrNotExist)) {
				s.races[filepath.ToSlash(filepath.Dir(path))] = true
				return nil
			}
			if err != nil {
				return err
			}
			if !os.SameFile(info, checked) {
				if path == "." {
					return errWatchRootChanged
				}
				s.races[path] = true
				info = checked
				continue
			}
			if addErr != nil {
				return addErr
			}
			info = checked
			if fileWatchLeaf(info) || !info.IsDir() || path != "." && !s.w.opts.Recursive {
				stable = true
				break
			}
			dir, err = s.root.Open(filepath.FromSlash(path))
			if errors.Is(err, fs.ErrNotExist) && path != "." {
				s.races[filepath.ToSlash(filepath.Dir(path))] = true
				return nil
			}
			if err != nil {
				if path != "." && !errors.Is(err, fs.ErrPermission) {
					checked, checkErr := s.root.Lstat(filepath.FromSlash(path))
					if errors.Is(checkErr, fs.ErrNotExist) {
						s.races[filepath.ToSlash(filepath.Dir(path))] = true
						return nil
					}
					if checkErr == nil && !os.SameFile(info, checked) {
						s.races[path] = true
						info = checked
						continue
					}
				}
				return err
			}
			opened, err := dir.Stat()
			if err != nil {
				dir.Close()
				return err
			}
			if os.SameFile(info, opened) {
				stable = true
				break
			}
			dir.Close()
			dir = nil
			if path == "." {
				return errWatchRootChanged
			}
			s.races[path] = true
			info, err = s.root.Lstat(filepath.FromSlash(path))
			if errors.Is(err, fs.ErrNotExist) {
				s.races[filepath.ToSlash(filepath.Dir(path))] = true
				return nil
			}
			if err != nil {
				return err
			}
		}
		if !stable {
			s.races[filepath.ToSlash(filepath.Dir(path))] = true
			return nil
		}
		result[path] = info
		if dir == nil {
			return nil
		}
		defer dir.Close()
		for {
			entries, e := dir.ReadDir(128)
			for _, entry := range entries {
				child := filepath.Join(filepath.FromSlash(path), entry.Name())
				childInfo, e := s.root.Lstat(child)
				if errors.Is(e, fs.ErrNotExist) {
					s.races[path] = true
					continue
				}
				if e != nil {
					return e
				}
				if e = visit(filepath.ToSlash(child), childInfo); e != nil {
					return e
				}
			}
			if e == io.EOF {
				break
			}
			if e != nil {
				return e
			}
		}
		return nil
	}
	if err = visit(".", info); err != nil {
		return nil, err
	}
	for path := range s.targets {
		if _, ok := result[path]; !ok && path != ".." {
			s.remove(path)
		}
	}
	return result, nil
}

func fileWatchDiff(before, after map[string]os.FileInfo, writes map[string]bool) []FileEvent {
	removed := make(map[string]os.FileInfo)
	created := make(map[string]os.FileInfo)
	for p, a := range before {
		if b, ok := after[p]; !ok || !os.SameFile(a, b) {
			removed[p] = a
		}
	}
	for p, b := range after {
		if a, ok := before[p]; !ok || !os.SameFile(a, b) {
			created[p] = b
		}
	}
	type group struct {
		path   string
		events []FileEvent
	}
	var groups []group
	paired := make(map[string]string)
	keys := make([]string, 0, len(removed))
	for p := range removed {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	for _, old := range keys {
		a := removed[old]
		match := ""
		count := 0
		for p, b := range created {
			if os.SameFile(a, b) {
				match = p
				count++
			}
		}
		if count != 1 {
			continue
		}
		// A hard link present anywhere makes identity-only pairing ambiguous.
		count = 0
		for _, v := range before {
			if os.SameFile(a, v) {
				count++
			}
		}
		if count != 1 {
			continue
		}
		count = 0
		for _, v := range after {
			if os.SameFile(a, v) {
				count++
			}
		}
		if count != 1 {
			continue
		}
		// Replacements keep their remove/create ordering even if an old name survives.
		if _, exists := after[old]; exists {
			continue
		}
		if _, exists := before[match]; exists {
			continue
		}
		descendant := false
		for parent, dest := range paired {
			if strings.HasPrefix(old, parent+"/") && match == dest+strings.TrimPrefix(old, parent) {
				descendant = true
				break
			}
		}
		paired[old] = match
		delete(removed, old)
		b := created[match]
		delete(created, match)
		events := []FileEvent{}
		if !descendant {
			events = append(events, FileEvent{Op: FileRename, OldPath: old, Path: match, IsDir: a.IsDir()})
		}
		if writes[old] || writes[match] || fileInfoChanged(a, b) {
			events = append(events, FileEvent{Op: FileWrite, Path: match, IsDir: b.IsDir()})
		}
		groups = append(groups, group{match, events})
	}
	paths := make(map[string]bool)
	for p := range before {
		paths[p] = true
	}
	for p := range after {
		paths[p] = true
	}
	for p := range paths {
		var events []FileEvent
		if a, ok := removed[p]; ok {
			events = append(events, FileEvent{Op: FileRemove, Path: p, IsDir: a.IsDir()})
		}
		if b, ok := created[p]; ok {
			events = append(events, FileEvent{Op: FileCreate, Path: p, IsDir: b.IsDir()})
		}
		a, aok := before[p]
		b, bok := after[p]
		if aok && bok && os.SameFile(a, b) && (writes[p] || fileInfoChanged(a, b)) {
			events = append(events, FileEvent{Op: FileWrite, Path: p, IsDir: b.IsDir()})
		}
		groups = append(groups, group{p, events})
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].path < groups[j].path })
	var result []FileEvent
	for _, g := range groups {
		result = append(result, g.events...)
	}
	return result
}
func fileInfoChanged(a, b os.FileInfo) bool {
	return a.Size() != b.Size() || a.Mode() != b.Mode() || !a.ModTime().Equal(b.ModTime())
}
