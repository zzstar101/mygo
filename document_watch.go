package mygo

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"time"
)

// Documents notice what other apps do to their files: one WatchFiles per
// directory holding open documents, and a look when a document's window
// is focused, since watches miss changes (VS Code and Notepad++ look
// then too) and stop in directories with too many files. An unedited
// document reads its file again, an edited one is in conflict and asks at
// its next save, a renamed file is followed, and a deleted one is
// written again by the next save.

var docWatches struct {
	sync.Mutex
	dirs map[string]*docDirWatch
}

type docDirWatch struct {
	refs   int
	cancel context.CancelFunc
}

// watch watches the directory of b's file, after it opened or moved.
func (b *docBase) watch() {
	b.mu.Lock()
	dir := ""
	if b.path != "" && !b.closed {
		dir = filepath.Dir(b.path)
	}
	old := b.watched
	b.watched = dir
	b.mu.Unlock()
	if dir == old {
		return
	}
	docWatches.Lock()
	defer docWatches.Unlock()
	if old != "" {
		if w := docWatches.dirs[old]; w != nil {
			if w.refs--; w.refs == 0 {
				w.cancel()
				delete(docWatches.dirs, old)
			}
		}
	}
	if dir == "" {
		return
	}
	if w := docWatches.dirs[dir]; w != nil {
		w.refs++
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	if docWatches.dirs == nil {
		docWatches.dirs = map[string]*docDirWatch{}
	}
	docWatches.dirs[dir] = &docDirWatch{refs: 1, cancel: cancel}
	go watchDocumentDir(ctx, dir)
}

// watchDocumentDir follows the changes of dir until ctx is done.
func watchDocumentDir(ctx context.Context, dir string) {
	for ctx.Err() == nil {
		w, err := WatchFiles(ctx, dir, FileWatchOptions{Debounce: 100 * time.Millisecond})
		if err != nil {
			return // the directory is gone, or too large: focus checks remain
		}
		for {
			e, err := w.Next(ctx)
			if err != nil {
				w.Close()
				if errors.Is(err, ErrWatchOverflow) {
					for _, b := range documentsIn(dir) {
						go b.checkDisk()
					}
					break // watch again
				}
				return
			}
			for _, b := range documentsIn(dir) {
				name := filepath.Base(b.Path())
				switch {
				case e.Op == FileRename && e.OldPath == name:
					b.moved(filepath.Join(dir, filepath.FromSlash(e.Path)))
				case e.Path == name:
					go b.checkDisk()
				}
			}
		}
	}
}

// documentsIn returns the open documents whose files are in dir.
func documentsIn(dir string) []*docBase {
	var in []*docBase
	for _, b := range allDocuments() {
		if p := b.Path(); p != "" && filepath.Dir(p) == dir {
			in = append(in, b)
		}
	}
	return in
}

// checkDisk compares b's file with what b last read or wrote.
func (b *docBase) checkDisk() {
	b.io.Lock()
	b.mu.Lock()
	path, known, edited, closed := b.path, b.stamp, b.edited, b.closed
	wasDeleted := b.deleted
	b.mu.Unlock()
	if path == "" || closed {
		b.io.Unlock()
		return
	}
	now, exists, err := statStamp(path)
	if err == nil && !exists && !wasDeleted {
		// Network shares report files gone that are not (VS Code).
		time.Sleep(100 * time.Millisecond)
		now, exists, err = statStamp(path)
	}
	if err != nil {
		b.io.Unlock()
		return
	}
	switch {
	case !exists:
		b.mu.Lock()
		changed := !b.deleted
		b.deleted = true
		b.mu.Unlock()
		b.io.Unlock()
		if changed {
			b.notify()
		}
	case now == known && !wasDeleted:
		readOnly := !fileWritable(path)
		b.mu.Lock()
		changed := b.readOnly != readOnly
		b.readOnly = readOnly
		b.mu.Unlock()
		b.io.Unlock()
		if changed {
			b.notify()
		}
	case !edited:
		b.io.Unlock()
		_ = b.reload(path)
	default:
		b.mu.Lock()
		changed := !b.conflict || b.deleted
		b.conflict, b.deleted = true, false
		b.mu.Unlock()
		b.io.Unlock()
		if changed {
			b.notify()
		}
	}
}

// moved follows b's file, which another app renamed to path.
func (b *docBase) moved(path string) {
	b.mu.Lock()
	b.path = path
	b.mu.Unlock()
	addRecent(path)
	b.watch()
	b.notify()
}
