//go:build !js

package mygo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/fake"
	"github.com/egoist/mygo/internal/platform"
)

// fileWatchTestNotice reports notice for the target at path, found by
// path: on Windows the root's parent is enrolled before the root, so IDs
// differ.
func fileWatchTestNotice(t *testing.T, w *FileWatcher, path string, notice platform.FileWatchNotice) {
	t.Helper()
	native := w.native.(*fake.FileWatch)
	for _, target := range native.Targets() {
		if target.Path == filepath.Join(w.root, filepath.FromSlash(path)) {
			notice.Target = target.ID
			native.Notify(notice)
			return
		}
	}
	t.Fatalf("no watch target for %q", path)
}

// fileWatchTestNext returns the next event that isn't about the root,
// whose metadata changes along with its entries.
func fileWatchTestNext(t *testing.T, w *FileWatcher) FileEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		e, err := w.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if e.Path != "." {
			return e
		}
	}
}

func TestFileWatchLifecycle(t *testing.T) {
	dir := t.TempDir()
	w, err := WatchFiles(context.Background(), dir, FileWatchOptions{Debounce: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	fileWatchTestNotice(t, w, ".", platform.FileWatchNotice{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	e, err := w.Next(ctx)
	// Directory metadata can change alongside the child creation.
	if err == nil && e.Path == "." {
		e, err = w.Next(ctx)
	}
	if err != nil || e.Path != "a" || e.Op != FileCreate {
		t.Fatalf("event = %+v, %v", e, err)
	}
	wait, cancelWait := context.WithCancel(context.Background())
	cancelWait()
	if _, err = w.Next(wait); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	w.Close()
	w.Close()
	if _, err = w.Next(ctx); err != io.EOF {
		t.Fatal(err)
	}
	select {
	case <-w.Done():
	case <-ctx.Done():
		t.Fatal("cleanup timed out")
	}
}

func TestFileWatchParentSentinel(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows enrolls a parent sentinel")
	}
	w, err := WatchFiles(context.Background(), t.TempDir(), FileWatchOptions{Debounce: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := os.WriteFile(filepath.Join(w.root, "a"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	// A hint for a sibling must not scan the root or expose the sibling.
	fileWatchTestNotice(t, w, "..", platform.FileWatchNotice{Name: filepath.Base(w.root) + "-sibling"})
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, err := w.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("sibling hint was not ignored: %v", err)
	}
	fileWatchTestNotice(t, w, "..", platform.FileWatchNotice{Name: filepath.Base(w.root)})
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for {
		e, err := w.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if e.Path == "a" && e.Op == FileCreate {
			break
		}
		if e.Path != "." {
			t.Fatalf("unexpected event: %+v", e)
		}
	}
}

func TestFileWatchStaleFailureIgnored(t *testing.T) {
	w, err := WatchFiles(context.Background(), t.TempDir(), FileWatchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	fb.LastFileWatch().Notify(platform.FileWatchNotice{Target: 99999, Err: errors.New("retired target")})
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if _, err := w.Next(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stale failure stopped watcher: %v", err)
	}
}

func TestFileWatchValidation(t *testing.T) {
	for _, o := range []FileWatchOptions{{Buffer: -1}, {MaxEntries: -1}, {MaxDelay: -1}, {Debounce: time.Second, MaxDelay: time.Millisecond}} {
		if w, err := WatchFiles(context.Background(), t.TempDir(), o); err == nil {
			w.Close()
			t.Fatalf("accepted %+v", o)
		}
	}
	if _, err := WatchFiles(nil, t.TempDir(), FileWatchOptions{}); err == nil {
		t.Fatal("nil context")
	}
	if _, err := WatchFiles(context.Background(), "", FileWatchOptions{}); err == nil {
		t.Fatal("empty path")
	}
}

func TestFileWatchRecursiveLimit(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := WatchFiles(context.Background(), dir, FileWatchOptions{Recursive: true, MaxEntries: 2}); !errors.Is(err, ErrWatchLimit) {
		t.Fatalf("limit: %v", err)
	}
}

func TestFileWatchOverflow(t *testing.T) {
	dir := t.TempDir()
	w, err := WatchFiles(context.Background(), dir, FileWatchOptions{Debounce: -1, Buffer: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for _, p := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(dir, p), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	fileWatchTestNotice(t, w, ".", platform.FileWatchNotice{})
	select {
	case <-w.Done():
	case <-time.After(time.Second):
		t.Fatal("overflow did not stop")
	}
	if _, err = w.Next(context.Background()); !errors.Is(err, ErrWatchOverflow) {
		t.Fatal(err)
	}
}

func TestFileWatchRootLoss(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "root")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	w, err := WatchFiles(context.Background(), dir, FileWatchOptions{Debounce: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	fileWatchTestNotice(t, w, ".", platform.FileWatchNotice{Kind: platform.FileWatchRemove})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	e, err := w.Next(ctx)
	if err != nil || e.Path != "." || e.Op != FileRemove {
		t.Fatalf("%+v %v", e, err)
	}
	if _, err = w.Next(ctx); err == nil || err == io.EOF {
		t.Fatal(err)
	}
}

func TestFileWatchRootReplacement(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "root")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	w, err := WatchFiles(context.Background(), dir, FileWatchOptions{Debounce: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	// The anchored handle must allow renames, but must not silently follow
	// a different directory subsequently installed at the watched path.
	if err := os.Rename(dir, filepath.Join(parent, "retired")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	fileWatchTestNotice(t, w, ".", platform.FileWatchNotice{})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	e, err := w.Next(ctx)
	if err != nil || e.Path != "." || e.Op != FileRemove {
		t.Fatalf("%+v %v", e, err)
	}
	if _, err := w.Next(ctx); !errors.Is(err, errWatchRootChanged) {
		t.Fatalf("replacement: %v", err)
	}
}

// An entry replaced or removed between the scan seeing it and the backend
// enrolling it doesn't stop the watch.
func TestFileWatchEnrollmentRace(t *testing.T) {
	for _, recursive := range []bool{false, true} {
		for _, replace := range []bool{false, true} {
			for _, isDir := range []bool{false, true} {
				name := fmt.Sprintf("recursive=%v/replace=%v/directory=%v", recursive, replace, isDir)
				t.Run(name, func(t *testing.T) {
					dir := t.TempDir()
					w, err := WatchFiles(context.Background(), dir, FileWatchOptions{Recursive: recursive, Debounce: -1})
					if err != nil {
						t.Fatal(err)
					}
					defer w.Close()
					path := filepath.Join(dir, "entry")
					create := func(path string) error {
						if isDir {
							return os.Mkdir(path, 0700)
						}
						return os.WriteFile(path, []byte("data"), 0600)
					}
					raced := false
					w.native.(*fake.FileWatch).AddHook = func(target platform.FileWatchTarget) error {
						if target.Path != filepath.Join(w.root, "entry") || raced {
							return nil
						}
						raced = true
						if !replace {
							return os.Remove(path)
						}
						replacement := filepath.Join(dir, "replacement")
						if err := create(replacement); err != nil {
							return err
						}
						if isDir {
							if err := os.Rename(path, filepath.Join(t.TempDir(), "retired")); err != nil {
								return err
							}
						}
						return os.Rename(replacement, path)
					}
					if err := create(path); err != nil {
						t.Fatal(err)
					}
					fileWatchTestNotice(t, w, ".", platform.FileWatchNotice{Name: "entry", Kind: platform.FileWatchCreate})
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()
					for {
						e, err := w.Next(ctx)
						if err != nil {
							t.Fatalf("enrollment race killed watch: %v", err)
						}
						if !replace || e.Path == "entry" {
							break
						}
					}
					if !raced {
						t.Fatal("the entry was not enrolled")
					}
					if err := os.WriteFile(filepath.Join(dir, "later"), nil, 0600); err != nil {
						t.Fatal(err)
					}
					fileWatchTestNotice(t, w, ".", platform.FileWatchNotice{Name: "later", Kind: platform.FileWatchCreate})
					for {
						e, err := w.Next(ctx)
						if err != nil {
							t.Fatalf("watch did not survive: %v", err)
						}
						if e.Path == "later" && e.Op == FileCreate {
							break
						}
					}
				})
			}
		}
	}
}

// A notice for a file looks only at it, and one for a directory only at
// its entries.
func TestFileWatchLooksOnlyWhereNotified(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"a", "b/c", "d/e"} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	w, err := WatchFiles(context.Background(), dir, FileWatchOptions{Recursive: true, Debounce: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	// Changes the watch isn't told about stay unseen.
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("unseen"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "d", "f"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b", "c"), []byte("seen"), 0600); err != nil {
		t.Fatal(err)
	}
	fileWatchTestNotice(t, w, "b", platform.FileWatchNotice{Name: "c", Kind: platform.FileWatchWrite})
	if e := fileWatchTestNext(t, w); e != (FileEvent{Op: FileWrite, Path: "b/c"}) {
		t.Fatalf("event = %+v", e)
	}
	fileWatchTestNotice(t, w, "d", platform.FileWatchNotice{})
	for {
		e := fileWatchTestNext(t, w)
		if e == (FileEvent{Op: FileCreate, Path: "d/f"}) {
			break
		}
		if e.Path != "d" {
			t.Fatalf("event = %+v", e)
		}
	}
	// Losing changes looks at everything.
	fb.LastFileWatch().Notify(platform.FileWatchNotice{Err: platform.ErrFileWatchOverflow})
	if e := fileWatchTestNext(t, w); e != (FileEvent{Op: FileWrite, Path: "a"}) {
		t.Fatalf("event = %+v", e)
	}
}

// A directory renamed is one rename, not one for everything in it.
func TestFileWatchDirectoryRename(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "b", "c"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	w, err := WatchFiles(context.Background(), dir, FileWatchOptions{Recursive: true, Debounce: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := os.Rename(filepath.Join(dir, "a"), filepath.Join(dir, "z")); err != nil {
		t.Fatal(err)
	}
	native := w.native.(*fake.FileWatch)
	fileWatchTestNotice(t, w, ".", platform.FileWatchNotice{Name: "a", Kind: platform.FileWatchMoveFrom})
	if e := fileWatchTestNext(t, w); e != (FileEvent{Op: FileRename, OldPath: "a", Path: "z", IsDir: true}) {
		t.Fatalf("event = %+v", e)
	}
	// Its directories are enrolled again under their new paths.
	enrolled := map[string]bool{}
	for _, target := range native.Targets() {
		enrolled[target.Path] = true
	}
	for _, p := range []string{"z", "z/b", "z/b/c"} {
		if !enrolled[filepath.Join(w.root, filepath.FromSlash(p))] {
			t.Errorf("%s not enrolled", p)
		}
	}
	if enrolled[filepath.Join(w.root, "a")] {
		t.Error("a still enrolled")
	}
}

// A backend that watches a tree with one target is given only the root,
// and names what changes below by its path.
func TestFileWatchSubtree(t *testing.T) {
	fb.FileWatchSubtree = true
	defer func() { fb.FileWatchSubtree = false }()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0700); err != nil {
		t.Fatal(err)
	}
	w, err := WatchFiles(context.Background(), dir, FileWatchOptions{Recursive: true, Debounce: -1})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	native := w.native.(*fake.FileWatch)
	for _, target := range native.Targets() {
		if target.Path == w.root {
			if !target.Subtree {
				t.Error("the root is not a subtree target")
			}
		} else if target.Path != filepath.Dir(w.root) {
			t.Errorf("%s enrolled", target.Path)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "a", "b", "c", "d"), 0700); err != nil {
		t.Fatal(err)
	}
	fileWatchTestNotice(t, w, ".", platform.FileWatchNotice{Name: "a/b/c", Kind: platform.FileWatchCreate})
	want := []FileEvent{{Op: FileCreate, Path: "a/b/c", IsDir: true}, {Op: FileCreate, Path: "a/b/c/d", IsDir: true}}
	for _, want := range want {
		for {
			e := fileWatchTestNext(t, w)
			if e == want {
				break
			}
			if e.Op != FileWrite {
				t.Fatalf("event = %+v, want %+v", e, want)
			}
		}
	}
	// The root's target lost enrolls it again and looks at everything.
	if err := os.WriteFile(filepath.Join(dir, "a", "f"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	fileWatchTestNotice(t, w, ".", platform.FileWatchNotice{Kind: platform.FileWatchLost})
	for {
		e := fileWatchTestNext(t, w)
		if e == (FileEvent{Op: FileCreate, Path: "a/f"}) {
			break
		}
		if e.Op != FileWrite {
			t.Fatalf("event = %+v", e)
		}
	}
	found := false
	for _, target := range native.Targets() {
		found = found || target.Path == w.root && target.Subtree
	}
	if !found {
		t.Fatal("the root was not enrolled again")
	}
	fileWatchTestNotice(t, w, ".", platform.FileWatchNotice{Name: "../x"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := w.Next(ctx); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a name out of the tree: %v", err)
	}
}

type watchTestInfo struct{ os.FileInfo }

func TestFileWatchDiff(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(a)
	if err != nil {
		t.Fatal(err)
	}
	// Stat through a handle captures the identity now. On Windows os.Stat
	// defers fetching it until SameFile, when the old name no longer exists.
	info, err := f.Stat()
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	old := map[string]os.FileInfo{"a": info}
	if err := os.Rename(a, b); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(b)
	if err != nil {
		t.Fatal(err)
	}
	events := fileWatchDiff(old, map[string]os.FileInfo{"b": info}, map[string]bool{"a": true})
	if len(events) != 2 || events[0].Op != FileRename || events[0].OldPath != "a" || events[1].Op != FileWrite {
		t.Fatalf("%+v", events)
	}
}
