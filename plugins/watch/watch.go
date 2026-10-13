// Package watch is the watch plugin of MyGo: it lets pages watch for
// changes to files in the directories the app names (see
// @mygo-plugins/watch). Go code and native UI call mygo.WatchFiles
// directly.
package watch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/internal/callcontext"
	"github.com/egoist/mygo/internal/platform"
)

// Root is a directory pages may watch.
type Root struct {
	// Path is the directory's absolute path.
	Path string
	// Recursive lets pages watch the directories in it too.
	Recursive bool
}

// Options configures the plugin.
type Options struct {
	// Roots are the directories pages may watch, by the names they use:
	// lowercase letters, digits and dashes, starting with a letter.
	Roots map[string]Root
	// Allow, when set, is asked before each watch with the call's context
	// and the root's name, and can refuse it.
	Allow func(ctx context.Context, root string) bool
	// MaxWatchesPerPage is how many watches a page may have at a time: 8
	// by default. All pages may have 128.
	MaxWatchesPerPage int
}

// Plugin is the plugin without roots, which lets pages watch nothing.
var Plugin = New(Options{})

type watcher interface {
	Next(context.Context) (mygo.FileEvent, error)
	Close()
	Done() <-chan struct{}
	Err() error
}
type service struct {
	opts       Options
	identities map[string]os.FileInfo
	factory    func(context.Context, string, mygo.FileWatchOptions) (watcher, error)
	mu         sync.Mutex
	pages      map[<-chan struct{}]int
	total      int
}

// New returns the plugin with opts. mygo.Use panics when a root is not an
// absolute path to a directory, after resolving its symlinks.
func New(opts Options) mygo.Plugin {
	roots := make(map[string]Root, len(opts.Roots))
	for k, v := range opts.Roots {
		roots[k] = v
	}
	opts.Roots = roots
	if opts.MaxWatchesPerPage == 0 {
		opts.MaxWatchesPerPage = 8
	}
	s := &service{opts: opts, identities: make(map[string]os.FileInfo), pages: make(map[<-chan struct{}]int), factory: func(ctx context.Context, path string, o mygo.FileWatchOptions) (watcher, error) {
		return mygo.WatchFiles(ctx, path, o)
	}}
	return mygo.Plugin{Name: "watch", Service: s, Setup: s.setup}
}

var alias = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

func (s *service) setup() error {
	if s.opts.MaxWatchesPerPage < 0 {
		return errors.New("watch: negative page quota")
	}
	for name, root := range s.opts.Roots {
		if !alias.MatchString(name) {
			return fmt.Errorf("watch: invalid root alias %q", name)
		}
		if !filepath.IsAbs(root.Path) {
			return fmt.Errorf("watch: root %q: root must be absolute", name)
		}
		canonical, err := filepath.EvalSymlinks(root.Path)
		if err != nil {
			return fmt.Errorf("watch: root %q: %w", name, err)
		}
		root.Path = canonical
		info, err := validateRoot(root.Path)
		if err != nil {
			return fmt.Errorf("watch: root %q: %w", name, err)
		}
		s.opts.Roots[name] = root
		s.identities[name] = info
	}
	return nil
}
func validateRoot(path string) (os.FileInfo, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("root must be absolute")
	}
	path = filepath.Clean(path)
	for p := path; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || isReparse(info) {
			return nil, errors.New("root components must be ordinary directories")
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	f, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Stat(".")
}

type request struct {
	Root       string `json:"root"`
	Recursive  bool   `json:"recursive"`
	DebounceMS int    `json:"debounceMs"`
}
type part struct {
	Type    string           `json:"type"`
	Events  []mygo.FileEvent `json:"events,omitzero"`
	Code    string           `json:"code,omitzero"`
	Message string           `json:"message,omitzero"`
}

func failure(code string) error { return fmt.Errorf("watch:%s:filesystem watch %s", code, code) }
func codeFor(err error) string {
	switch {
	case errors.Is(err, mygo.ErrWatchOverflow):
		return "overflow"
	case errors.Is(err, mygo.ErrWatchLimit):
		return "limit"
	case errors.Is(err, platform.ErrUnsupported):
		return "unsupported"
	default:
		return "io"
	}
}

// Watch watches a root for the calling page, streaming "ready" once
// everything is watched, then the events, until the page closes parts or
// goes away. Errors are "watch:<code>:<message>", without paths.
func (s *service) Watch(ctx context.Context, req request, parts *mygo.Channel[part]) error {
	return s.run(ctx, req, parts.Send, parts.Close)
}
func (s *service) run(ctx context.Context, req request, send func(part) error, unblock func()) error {
	root, ok := s.opts.Roots[req.Root]
	if !ok || req.Recursive && !root.Recursive || s.opts.Allow != nil && !s.opts.Allow(ctx, req.Root) {
		return failure("denied")
	}
	if req.DebounceMS < 0 || req.DebounceMS > 60000 {
		return failure("limit")
	}
	if ctx.Err() != nil {
		return nil
	}
	page, _ := ctx.Value(callcontext.PageKey{}).(<-chan struct{})
	if page == nil {
		return failure("denied")
	}
	s.mu.Lock()
	if s.total >= 128 || s.pages[page] >= s.opts.MaxWatchesPerPage {
		s.mu.Unlock()
		return failure("limit")
	}
	s.total++
	s.pages[page]++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.total--
		s.pages[page]--
		if s.pages[page] == 0 {
			delete(s.pages, page)
		}
		s.mu.Unlock()
	}()
	info, err := validateRoot(root.Path)
	if err != nil || s.identities[req.Root] == nil || !os.SameFile(info, s.identities[req.Root]) {
		return failure("io")
	}
	debounce := time.Duration(req.DebounceMS) * time.Millisecond
	if debounce == 0 {
		debounce = -1
	}
	life, cancel := context.WithCancel(ctx)
	defer cancel()
	w, err := s.factory(life, root.Path, mygo.FileWatchOptions{Recursive: req.Recursive, Debounce: debounce, MaxDelay: max(500*time.Millisecond, debounce)})
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return failure(codeFor(err))
	}
	defer func() { w.Close(); <-w.Done() }()
	stop := context.AfterFunc(ctx, func() { w.Close(); unblock() })
	defer stop()
	monitorDone := make(chan struct{})
	defer close(monitorDone)
	go func() {
		select {
		case <-w.Done():
			if w.Err() != nil {
				unblock()
			}
		case <-monitorDone:
		}
	}()
	outcome := func(err error) error {
		if terminal := w.Err(); terminal != nil && !errors.Is(terminal, context.Canceled) && !errors.Is(terminal, context.DeadlineExceeded) {
			return failure(codeFor(terminal))
		}
		if ctx.Err() != nil || errors.Is(err, io.EOF) {
			return nil
		}
		return failure(codeFor(err))
	}
	if err = send(part{Type: "ready"}); err != nil {
		return outcome(err)
	}
	for {
		event, err := w.Next(life)
		if err != nil {
			result := outcome(err)
			if result != nil && ctx.Err() == nil {
				_ = send(part{Type: "error", Code: codeFor(err), Message: "filesystem watch stopped"})
			}
			return result
		}
		if !utf8.ValidString(event.Path) || !utf8.ValidString(event.OldPath) {
			return failure("path-encoding")
		}
		p := part{Type: "events", Events: []mygo.FileEvent{event}}
		encoded, err := json.Marshal(p)
		if err != nil || len(encoded) > 64<<10 {
			return failure("limit")
		}
		if err = send(p); err != nil {
			return outcome(err)
		}
	}
}
