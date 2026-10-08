package mygo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

// FileOp describes a coalesced change, not a count of native operations.
type FileOp string

const (
	// FileCreate means a path appeared in the watched directory.
	FileCreate FileOp = "create"
	// FileWrite means data or metadata at a path may have changed.
	FileWrite FileOp = "write"
	// FileRemove means a path disappeared from the watched directory.
	FileRemove FileOp = "remove"
	// FileRename means a rename within the watched directory was identified.
	FileRename FileOp = "rename"
)

// FileEvent describes a change relative to the watched directory. Paths use
// slash separators; "." identifies the root. Events are invalidation hints,
// not a complete or ordered filesystem journal.
type FileEvent struct {
	Op   FileOp `json:"op"`
	Path string `json:"path"`
	// OldPath is set only for FileRename; Path is its new name.
	OldPath string `json:"oldPath,omitzero"`
	// IsDir reports the kind observed before removal or after creation.
	IsDir bool `json:"isDir"`
}

// FileWatchOptions configures a watch. Zero values select useful defaults.
type FileWatchOptions struct {
	// Recursive watches descendants too, without following symlinks.
	Recursive bool
	// Debounce is the quiet period: 50 ms by default; negative disables it.
	Debounce time.Duration
	// MaxDelay bounds the debounce timer: 500 ms by default, at least Debounce.
	MaxDelay time.Duration
	// Buffer bounds queued public events: 1024 by default.
	Buffer int
	// MaxEntries bounds the snapshot and native targets: 8192 by default.
	MaxEntries int
}

// ErrWatchOverflow means changes were lost. Re-read state and start a new watch.
var ErrWatchOverflow = errors.New("mygo: file watch overflow")

// ErrWatchLimit means the configured entry limit was exceeded.
var ErrWatchLimit = errors.New("mygo: file watch entry limit exceeded")

// FileWatcher owns an event stream and its native resources. Methods are safe
// from any goroutine. Use one logical Next consumer. Obtain it from WatchFiles;
// its zero value is not usable.
type FileWatcher struct {
	mu            sync.Mutex
	events        []FileEvent
	waiters       []*fileWaiter
	stopped       bool
	err           error
	done          chan struct{}
	ctx           context.Context
	cancel        context.CancelFunc
	native        platform.FileWatch // main-thread only
	nativeClosing bool               // main-thread only
	nativeDone    chan (<-chan struct{})
	raw           chan platform.FileWatchNotice
	overflow      chan struct{}
	root          string
	opts          FileWatchOptions
}
type fileResult struct {
	event FileEvent
	err   error
}
type fileWaiter struct{ result chan fileResult }

var fileWatches = struct {
	sync.Mutex
	stopped bool
	active  map[*FileWatcher]bool
}{active: make(map[*FileWatcher]bool)}

// WatchFiles watches an existing directory without traversing descendant
// symlinks or reparse points. It requires a running app. It returns after
// enumeration and native registration; initial contents are not emitted.
// The lifetime context cancels the watch. On main, setup pumps native events.
func WatchFiles(ctx context.Context, path string, opts FileWatchOptions) (*FileWatcher, error) {
	needsApp("WatchFiles")
	if ctx == nil {
		return nil, errors.New("mygo: nil watch context")
	}
	if path == "" {
		return nil, errors.New("mygo: empty watch path")
	}
	if opts.Buffer < 0 || opts.MaxEntries < 0 || opts.MaxDelay < 0 {
		return nil, errors.New("mygo: negative watch limit")
	}
	if opts.Buffer == 0 {
		opts.Buffer = 1024
	}
	if opts.MaxEntries == 0 {
		opts.MaxEntries = 8192
	}
	if opts.Debounce == 0 {
		opts.Debounce = 50 * time.Millisecond
	}
	if opts.Debounce < 0 {
		opts.Debounce = 0
	}
	if opts.MaxDelay == 0 {
		opts.MaxDelay = 500 * time.Millisecond
	}
	if opts.MaxDelay < opts.Debounce {
		return nil, errors.New("mygo: MaxDelay must be at least Debounce")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	life, cancel := context.WithCancel(ctx)
	w := &FileWatcher{ctx: life, cancel: cancel, root: root, opts: opts, done: make(chan struct{}), nativeDone: make(chan (<-chan struct{}), 1), raw: make(chan platform.FileWatchNotice, 4096), overflow: make(chan struct{}, 1)}
	ready := make(chan error, 1)
	start := func() {
		fileWatches.Lock()
		if fileWatches.stopped {
			fileWatches.Unlock()
			cancel()
			deliver(ready, errLoopStopped)
			return
		}
		fileWatches.active[w] = true
		fileWatches.Unlock()
		n, e := backend().NewFileWatch(postMain, w.notice)
		if e != nil {
			fileWatches.Lock()
			delete(fileWatches.active, w)
			fileWatches.Unlock()
			cancel()
			deliver(ready, fmt.Errorf("mygo: watch: %w", e))
			return
		}
		w.native = n
		go w.run(ready)
	}
	if isMainThread() {
		start()
	} else if !postMain(start) {
		cancel()
		return nil, errLoopStopped
	}
	if err := await(ready); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *FileWatcher) notice(n platform.FileWatchNotice) {
	select {
	case <-w.ctx.Done():
		return
	default:
	}
	select {
	case w.raw <- n:
	default:
		select {
		case w.overflow <- struct{}{}:
		default:
		}
	}
}

// Next returns the next change, EOF after clean close, or the terminal error.
// Canceling ctx cancels this wait, not the watch. Main-thread waits pump events.
func (w *FileWatcher) Next(ctx context.Context) (FileEvent, error) {
	if ctx == nil {
		return FileEvent{}, errors.New("mygo: nil wait context")
	}
	if err := ctx.Err(); err != nil {
		return FileEvent{}, err
	}
	waiter := &fileWaiter{make(chan fileResult, 1)}
	w.mu.Lock()
	if len(w.events) > 0 {
		e := w.events[0]
		w.events[0] = FileEvent{}
		w.events = w.events[1:]
		w.mu.Unlock()
		return e, nil
	}
	if w.stopped {
		err := w.err
		if err == nil {
			err = io.EOF
		}
		w.mu.Unlock()
		return FileEvent{}, err
	}
	w.waiters = append(w.waiters, waiter)
	w.mu.Unlock()
	stop := context.AfterFunc(ctx, func() {
		w.mu.Lock()
		found := false
		for i, v := range w.waiters {
			if v == waiter {
				w.waiters = append(w.waiters[:i], w.waiters[i+1:]...)
				found = true
				break
			}
		}
		w.mu.Unlock()
		if found {
			deliver(waiter.result, fileResult{err: ctx.Err()})
		}
	})
	defer stop()
	r := await(waiter.result)
	return r.event, r.err
}

// Close immediately stops delivery and requests cleanup without waiting for
// worker I/O or UI dispatch. It is idempotent and discards queued events.
func (w *FileWatcher) Close() { w.stop(nil, true) }

// Done closes after all descriptors, handles and workers have been released.
// Never wait directly on Done on the UI thread.
func (w *FileWatcher) Done() <-chan struct{} { return w.done }

// Err returns the terminal failure, or nil while active or after explicit Close.
func (w *FileWatcher) Err() error { w.mu.Lock(); defer w.mu.Unlock(); return w.err }

func (w *FileWatcher) stop(err error, discard bool) {
	w.mu.Lock()
	if w.stopped {
		if discard {
			w.events = nil
		}
		w.mu.Unlock()
		if isMainThread() {
			w.closeNative()
		}
		return
	}
	w.stopped = true
	w.err = err
	if discard {
		w.events = nil
	}
	waiters := w.waiters
	w.waiters = nil
	w.mu.Unlock()
	if err == nil {
		err = io.EOF
	}
	for _, waiter := range waiters {
		deliver(waiter.result, fileResult{err: err})
	}
	w.cancel()
	if isMainThread() {
		w.closeNative()
	} else {
		postMain(w.closeNative)
	}
}

func (w *FileWatcher) closeNative() {
	if !w.nativeClosing {
		w.nativeClosing = true
		w.nativeDone <- w.native.Close()
	}
}

func (w *FileWatcher) emit(events []FileEvent) error {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return context.Canceled
	}
	if len(events)-min(len(events), len(w.waiters)) > w.opts.Buffer-len(w.events) {
		w.mu.Unlock()
		return ErrWatchOverflow
	}
	var deliveries []struct {
		ch chan fileResult
		e  FileEvent
	}
	for _, e := range events {
		if len(w.waiters) > 0 {
			v := w.waiters[0]
			w.waiters = w.waiters[1:]
			deliveries = append(deliveries, struct {
				ch chan fileResult
				e  FileEvent
			}{v.result, e})
		} else {
			w.events = append(w.events, e)
		}
	}
	w.mu.Unlock()
	for _, d := range deliveries {
		deliver(d.ch, fileResult{event: d.e})
	}
	return nil
}

func stopFileWatches() {
	fileWatches.Lock()
	fileWatches.stopped = true
	all := make([]*FileWatcher, 0, len(fileWatches.active))
	for w := range fileWatches.active {
		all = append(all, w)
	}
	fileWatches.Unlock()
	// Close executes on main before waiting. All worker waits select cancellation.
	for _, w := range all {
		w.Close()
	}
	for _, w := range all {
		<-w.Done()
	}
}

func (w *FileWatcher) run(ready chan error) {
	var terminal error
	defer func() {
		w.stop(terminal, errors.Is(terminal, ErrWatchOverflow) || errors.Is(terminal, ErrWatchLimit))
		<-(<-w.nativeDone)
		fileWatches.Lock()
		delete(fileWatches.active, w)
		fileWatches.Unlock()
		close(w.done)
	}()
	scan := newFileWatchScan(w)
	defer scan.close()
	before, err := scan.scan()
	if err != nil {
		terminal = err
		deliver(ready, err)
		return
	}
	// Initial contents are not events, but changes observed while enrolling are.
	if err := w.emit(fileWatchDiff(before, before, scan.races)); err != nil {
		terminal = err
		deliver(ready, err)
		return
	}
	deliver(ready, nil)
	writes := make(map[string]bool)
	var timer *time.Timer
	var timerC <-chan time.Time
	var first time.Time
	dirty := false
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		select {
		case <-w.ctx.Done():
			terminal = w.ctx.Err()
			return
		case <-w.overflow:
			terminal = ErrWatchOverflow
			return
		case n := <-w.raw:
			path, ok := scan.ids[n.Target]
			if !ok && n.Target != 0 {
				continue
			}
			if n.Err != nil {
				terminal = n.Err
				if errors.Is(n.Err, platform.ErrFileWatchOverflow) {
					terminal = ErrWatchOverflow
				}
				return
			}
			if path == ".." {
				if n.Name != filepath.Base(w.root) {
					continue
				}
				path = "."
				n.Name = ""
			}
			if n.Name != "" {
				if n.Name == "." || n.Name == ".." || strings.ContainsAny(n.Name, "/\x00") || strings.ContainsRune(n.Name, filepath.Separator) {
					terminal = errors.New("mygo: invalid native watch name")
					return
				}
				path = filepath.ToSlash(filepath.Join(path, n.Name))
			}
			if n.Kind == platform.FileWatchWrite {
				writes[path] = true
				if len(writes) > w.opts.MaxEntries {
					terminal = ErrWatchOverflow
					return
				}
			}
			now := time.Now()
			if !dirty {
				first = now
				dirty = true
			}
			delay := w.opts.Debounce
			if remaining := time.Until(first.Add(w.opts.MaxDelay)); remaining < delay {
				delay = remaining
			}
			if timer == nil {
				timer = time.NewTimer(delay)
			} else {
				timer.Reset(delay)
			}
			timerC = timer.C
		case <-timerC:
			timerC = nil
			dirty = false
			after, e := scan.scan()
			if e != nil {
				terminal = e
				rootInfo, rootErr := os.Lstat(w.root)
				if errors.Is(rootErr, os.ErrNotExist) || rootErr == nil && !os.SameFile(rootInfo, scan.identity) {
					if emitErr := w.emit([]FileEvent{{Op: FileRemove, Path: ".", IsDir: true}}); emitErr != nil {
						terminal = emitErr
					}
				}
				return
			}
			for path := range scan.races {
				writes[path] = true
			}
			events := fileWatchDiff(before, after, writes)
			before = after
			clear(writes)
			if e = w.emit(events); e != nil {
				terminal = e
				return
			}
		}
	}
}

var errWatchRootChanged = errors.New("watched root identity changed")
