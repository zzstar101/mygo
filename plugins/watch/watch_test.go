package watch

import (
	"context"
	"errors"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/callcontext"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeWatcher struct {
	done   chan struct{}
	once   sync.Once
	events []mygo.FileEvent
	err    error
}

func (w *fakeWatcher) Next(context.Context) (mygo.FileEvent, error) {
	if len(w.events) > 0 {
		e := w.events[0]
		w.events = w.events[1:]
		return e, nil
	}
	if w.err != nil {
		return mygo.FileEvent{}, w.err
	}
	return mygo.FileEvent{}, io.EOF
}
func (w *fakeWatcher) Close()                { w.once.Do(func() { close(w.done) }) }
func (w *fakeWatcher) Done() <-chan struct{} { return w.done }
func (w *fakeWatcher) Err() error            { return w.err }
func pluginFixture(t *testing.T) (*service, context.Context) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := New(Options{Roots: map[string]Root{"project": {Path: dir}}})
	if err = p.Setup(); err != nil {
		t.Fatal(err)
	}
	return p.Service.(*service), context.WithValue(context.Background(), callcontext.PageKey{}, (<-chan struct{})(make(chan struct{})))
}
func TestStreamReadyAndCleanup(t *testing.T) {
	s, ctx := pluginFixture(t)
	w := &fakeWatcher{done: make(chan struct{}), events: []mygo.FileEvent{{Op: mygo.FileCreate, Path: "a"}}}
	s.factory = func(_ context.Context, _ string, o mygo.FileWatchOptions) (watcher, error) {
		if o.Debounce != -1 || o.MaxDelay != 500*time.Millisecond {
			t.Fatalf("options %+v", o)
		}
		return w, nil
	}
	var parts []part
	if err := s.run(ctx, request{Root: "project"}, func(p part) error { parts = append(parts, p); return nil }, func() {}); err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 || parts[0].Type != "ready" || parts[1].Events[0].Path != "a" {
		t.Fatalf("parts %+v", parts)
	}
	select {
	case <-w.Done():
	default:
		t.Fatal("not closed")
	}
	if s.total != 0 || len(s.pages) != 0 {
		t.Fatal("quota leaked")
	}
}
func TestStreamSanitizesFailures(t *testing.T) {
	s, ctx := pluginFixture(t)
	s.factory = func(context.Context, string, mygo.FileWatchOptions) (watcher, error) {
		return nil, &os.PathError{Op: "watch", Path: "/secret/private", Err: errors.New("secret")}
	}
	err := s.run(ctx, request{Root: "project"}, func(part) error { return nil }, func() {})
	if err == nil || strings.Contains(err.Error(), "secret") || !strings.HasPrefix(err.Error(), "watch:io:") {
		t.Fatal(err)
	}
	if s.total != 0 {
		t.Fatal("quota leaked")
	}
}
func TestPermissionsAndQuota(t *testing.T) {
	s, ctx := pluginFixture(t)
	s.factory = func(context.Context, string, mygo.FileWatchOptions) (watcher, error) {
		t.Fatal("unauthorized factory call")
		return nil, nil
	}
	if err := s.run(ctx, request{Root: "project", Recursive: true}, func(part) error { return nil }, func() {}); err == nil {
		t.Fatal("recursive permission")
	}
	page := ctx.Value(callcontext.PageKey{}).(<-chan struct{})
	s.pages[page] = 8
	if err := s.run(ctx, request{Root: "project"}, func(part) error { return nil }, func() {}); err == nil || !strings.HasPrefix(err.Error(), "watch:limit:") {
		t.Fatal(err)
	}
}

func TestDenyUnknownRoot(t *testing.T) {
	p := New(Options{})
	if err := p.Setup(); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{"project", "../", "/tmp", `C:\secret`, "a:b"} {
		err := p.Service.(*service).run(context.Background(), request{Root: root}, func(part) error { t.Fatal("sent unauthorized data"); return nil }, func() {})
		if err == nil || !strings.HasPrefix(err.Error(), "watch:denied:") {
			t.Fatalf("%q: %v", root, err)
		}
	}
}
func TestCanonicalRoot(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "real")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "alias")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	p := New(Options{Roots: map[string]Root{"project": {Path: link}}})
	if err := p.Setup(); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := p.Service.(*service)
	if got := s.opts.Roots["project"].Path; got != want {
		t.Fatalf("root = %q, want %q", got, want)
	}
}

func TestConfiguration(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	roots := map[string]Root{"project": {Path: dir}}
	p := New(Options{Roots: roots})
	roots["project"] = Root{Path: "relative"}
	if err := p.Setup(); err != nil {
		t.Fatal(err)
	}
	for _, opts := range []Options{{Roots: map[string]Root{"../": {Path: dir}}}, {Roots: map[string]Root{"project": {Path: "relative"}}}, {MaxWatchesPerPage: -1}} {
		if err := New(opts).Setup(); err == nil {
			t.Fatalf("accepted %+v", opts)
		}
	}
}
