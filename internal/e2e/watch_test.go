package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

func TestFileWatchPluginPages(t *testing.T) {
	if _, err := os.Stat("../../plugins/watch/dist/index.js"); err != nil {
		t.Skip("run bun run build first")
	}
	a := newWindow(t, mygo.WindowOptions{Hidden: true})
	b := newWindow(t, mygo.WindowOptions{Hidden: true})
	start := func(w *mygo.Window) {
		t.Helper()
		if err := w.Page().LoadURL("app://localhost/plugins.html"); err != nil {
			t.Fatal(err)
		}
		waitFor(t, w, "window.plugins && !window.watcher")
		if _, err := w.Page().Eval(`(async()=>{window.watcher=await plugins.watch("project",{debounceMs:0});window.seen=[];window.consume=(async()=>{for await(const e of watcher)seen.push(e.path)})();return true})()`); err != nil {
			t.Fatal(err)
		}
	}
	start(a)
	start(b) // quota is one per page, not one per window service.
	path := filepath.Join(watchPluginRoot, "page-event")
	t.Cleanup(func() { _ = os.Remove(path) })
	if err := os.WriteFile(path, []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, `seen.includes("page-event")`)
	waitFor(t, b, `seen.includes("page-event")`)
	// Navigation cancels the old page's stream and grants a fresh quota generation.
	start(a)
	if _, err := b.Page().Eval(`seen=[]`); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, `seen.includes("page-event")`)
	waitFor(t, b, `seen.includes("page-event")`)
	for _, w := range []*mygo.Window{a, b} {
		if _, err := w.Page().Eval(`watcher.close()`); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFileWatch(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	w, err := mygo.WatchFiles(ctx, dir, mygo.FileWatchOptions{Recursive: true, Debounce: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		w.Close()
		select {
		case <-w.Done():
		case <-ctx.Done():
			t.Error("watch cleanup timed out")
		}
	}()
	wait := func(op mygo.FileOp, path string) {
		t.Helper()
		for {
			e, err := w.Next(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if e.Op == op && e.Path == path {
				return
			}
		}
	}
	file := filepath.Join(dir, "a")
	if err := os.WriteFile(file, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	wait(mygo.FileCreate, "a")
	if err := os.WriteFile(file, []byte("updated"), 0600); err != nil {
		t.Fatal(err)
	}
	wait(mygo.FileWrite, "a")
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	wait(mygo.FileRemove, "a")
	if err := os.Mkdir(filepath.Join(dir, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "child", "nested"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	wait(mygo.FileCreate, "child/nested")
}
