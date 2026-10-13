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

// Resolve the enrollment by path: Windows enrolls a private parent sentinel
// before the root, so target IDs are not portable.
func fileWatchTestNotice(t *testing.T, w *FileWatcher, path string, notice platform.FileWatchNotice) {
	t.Helper()
	found := false
	onMain(func() {
		native := w.native.(*fake.FileWatch)
		for id, target := range native.Targets {
			if target.Path == filepath.Join(w.root, filepath.FromSlash(path)) {
				notice.Target = id
				native.Notify(notice)
				found = true
				break
			}
		}
	})
	if !found {
		t.Fatalf("no watch target for %q", path)
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
	onMain(func() {
		fb.FileWatches[len(fb.FileWatches)-1].Notify(platform.FileWatchNotice{Target: 99999, Err: errors.New("retired target")})
	})
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
					var native *fake.FileWatch
					onMain(func() { native = fb.FileWatches[len(fb.FileWatches)-1]; native.DelayAdd = true })
					path := filepath.Join(dir, "entry")
					create := func(path string) error {
						if isDir {
							return os.Mkdir(path, 0700)
						}
						return os.WriteFile(path, []byte("data"), 0600)
					}
					if err := create(path); err != nil {
						t.Fatal(err)
					}
					fileWatchTestNotice(t, w, ".", platform.FileWatchNotice{})
					deadline := time.Now().Add(time.Second)
					for {
						found := false
						onMain(func() {
							for _, target := range native.Targets {
								if filepath.Base(target.Path) == "entry" {
									found = true
								}
							}
						})
						if found {
							break
						}
						if time.Now().After(deadline) {
							t.Fatal("scan did not reach delayed Add")
						}
						time.Sleep(time.Millisecond)
					}
					if replace {
						replacement := filepath.Join(dir, "replacement")
						if err := create(replacement); err != nil {
							t.Fatal(err)
						}
						if isDir {
							if err := os.Rename(path, filepath.Join(t.TempDir(), "retired")); err != nil {
								t.Fatal(err)
							}
						}
						if err := os.Rename(replacement, path); err != nil {
							t.Fatal(err)
						}
					} else if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					onMain(func() { native.DelayAdd = false; native.CompleteAdds(nil) })
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
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
					if err := os.WriteFile(filepath.Join(dir, "later"), nil, 0600); err != nil {
						t.Fatal(err)
					}
					fileWatchTestNotice(t, w, ".", platform.FileWatchNotice{})
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
