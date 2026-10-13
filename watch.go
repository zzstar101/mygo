package mygo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

// FileOp is the kind of a FileEvent.
type FileOp string

const (
	// FileCreate is a file or directory that appeared.
	FileCreate FileOp = "create"
	// FileWrite is a file or directory whose content or metadata may have
	// changed.
	FileWrite FileOp = "write"
	// FileRemove is a file or directory that went away.
	FileRemove FileOp = "remove"
	// FileRename is a file or directory that moved within the watched
	// directory.
	FileRename FileOp = "rename"
)

// FileEvent is a change in a watched directory. A watch compares what it
// sees with what it saw before, so events say what is different, not every
// step that made it so: a file written twice in a row is one write, and
// one created and removed in between is nothing. Treat them as hints to
// read the files again.
type FileEvent struct {
	Op FileOp `json:"op"`
	// Path is the path relative to the watched directory, separated by
	// slashes; "." is the directory itself.
	Path string `json:"path"`
	// OldPath is where a FileRename moved from.
	OldPath string `json:"oldPath,omitzero"`
	// IsDir reports whether it is a directory, as it was before a
	// FileRemove.
	IsDir bool `json:"isDir"`
}

// FileWatchOptions configures WatchFiles. The zero value watches a
// directory without the directories in it.
type FileWatchOptions struct {
	// Recursive watches the directories in the directory too.
	Recursive bool
	// Debounce is how long a watch waits for changes to stop before it
	// reports them: 50 ms by default. Negative reports them right away.
	Debounce time.Duration
	// MaxDelay is the longest changes wait while more keep coming: 500 ms
	// by default. It can't be shorter than Debounce.
	MaxDelay time.Duration
	// Buffer is how many events wait for Next before the watch stops with
	// ErrWatchOverflow: 1024 by default.
	Buffer int
	// MaxEntries is how many files and directories a watch covers before
	// it stops with ErrWatchLimit: 8192 by default.
	MaxEntries int
}

// ErrWatchOverflow is what a watch stops with when more events waited for
// Next than FileWatchOptions.Buffer. Read the files again and start a new
// watch.
var ErrWatchOverflow = errors.New("mygo: file watch overflow")

// ErrWatchLimit is what a watch stops with when its directory has more
// than FileWatchOptions.MaxEntries files and directories.
var ErrWatchLimit = errors.New("mygo: file watch entry limit exceeded")

// FileWatcher is a watch WatchFiles started. Its methods are safe from any
// goroutine.
type FileWatcher struct {
	mu      sync.Mutex
	events  []FileEvent
	waiters []*fileWaiter
	stopped bool
	err     error
	done    chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	// native is the backend's watch, which only the goroutine of the
	// watch uses; what it reports goes through raw, and overflow says raw
	// was full.
	native   platform.FileWatch
	raw      chan platform.FileWatchNotice
	overflow chan struct{}
	root     string
	opts     FileWatchOptions
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

// WatchFiles watches the directory at path, and with opts.Recursive the
// directories in it, but not those that symlinks or Windows junctions
// point to. It returns once everything is watched; what is already there
// is not reported. Canceling ctx stops the watch, as Close does.
//
// It can be called from any goroutine, and on the main thread once the app
// runs. The app stops its watches when it quits.
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
	w := &FileWatcher{ctx: life, cancel: cancel, root: root, opts: opts, done: make(chan struct{}), raw: make(chan platform.FileWatchNotice, 4096), overflow: make(chan struct{}, 1)}
	fileWatches.Lock()
	if fileWatches.stopped {
		fileWatches.Unlock()
		cancel()
		return nil, errLoopStopped
	}
	fileWatches.active[w] = true
	fileWatches.Unlock()
	if w.native, err = backend().NewFileWatch(w.notice); err != nil {
		fileWatches.Lock()
		delete(fileWatches.active, w)
		fileWatches.Unlock()
		cancel()
		return nil, fmt.Errorf("mygo: watch: %w", err)
	}
	ready := make(chan error, 1)
	go w.run(ready)
	if err := await(ready); err != nil {
		return nil, err
	}
	return w, nil
}

// notice is what the backend reports to, on its goroutine. When raw is
// full the watch looks at everything again instead.
func (w *FileWatcher) notice(n platform.FileWatchNotice) {
	select {
	case <-w.ctx.Done():
		return
	default:
	}
	if n.Err != nil && !errors.Is(n.Err, platform.ErrFileWatchOverflow) {
		// An error stops the watch, so it waits its turn.
		select {
		case w.raw <- n:
		case <-w.ctx.Done():
		}
		return
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

// Next waits for the next change. It returns io.EOF once the watch is
// closed, or the error that stopped it. Canceling ctx cancels the wait,
// not the watch. On the main thread it keeps processing events while it
// waits.
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

// Close stops the watch and drops the changes not yet returned by Next. It
// doesn't wait for the watch to release what it holds; Done does.
func (w *FileWatcher) Close() { w.stop(nil, true) }

// Done is closed once the watch has stopped and released its handles.
func (w *FileWatcher) Done() <-chan struct{} { return w.done }

// Err returns the error that stopped the watch, or nil if it is still
// running or was closed.
func (w *FileWatcher) Err() error { w.mu.Lock(); defer w.mu.Unlock(); return w.err }

func (w *FileWatcher) stop(err error, discard bool) {
	w.mu.Lock()
	if w.stopped {
		if discard {
			w.events = nil
		}
		w.mu.Unlock()
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
	for _, w := range all {
		w.Close()
	}
	for _, w := range all {
		<-w.Done()
	}
}

// run is the goroutine of the watch: it enrolls the tree, then waits for
// what the backend reports and, once nothing more came for opts.Debounce
// (or opts.MaxDelay passed), looks at it.
func (w *FileWatcher) run(ready chan error) {
	var terminal error
	scan := newFileWatchScan(w)
	defer func() {
		w.stop(terminal, errors.Is(terminal, ErrWatchOverflow) || errors.Is(terminal, ErrWatchLimit))
		scan.close()
		w.native.Close()
		fileWatches.Lock()
		delete(fileWatches.active, w)
		fileWatches.Unlock()
		close(w.done)
	}()
	events, err := scan.start()
	if err == nil {
		err = w.emit(events)
	}
	if err != nil {
		terminal = err
		deliver(ready, err)
		return
	}
	deliver(ready, nil)
	var timer *time.Timer
	var timerC <-chan time.Time
	var first time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	schedule := func() {
		if timerC == nil {
			first = time.Now()
		}
		delay := min(w.opts.Debounce, time.Until(first.Add(w.opts.MaxDelay)))
		if timer == nil {
			timer = time.NewTimer(delay)
		} else {
			timer.Reset(delay)
		}
		timerC = timer.C
	}
	for {
		select {
		case <-w.ctx.Done():
			terminal = w.ctx.Err()
			return
		case <-w.overflow:
			scan.all = true
			schedule()
		case n := <-w.raw:
			if err := scan.mark(n); err != nil {
				terminal = err
				return
			}
			if scan.pending() {
				schedule()
			}
		case <-timerC:
			timerC = nil
			events, err := scan.update()
			if err != nil {
				terminal = err
				info, statErr := os.Lstat(w.root)
				if errors.Is(statErr, os.ErrNotExist) || statErr == nil && !os.SameFile(info, scan.identity) {
					if err := w.emit([]FileEvent{{Op: FileRemove, Path: ".", IsDir: true}}); err != nil {
						terminal = err
					}
				}
				return
			}
			if err = w.emit(events); err != nil {
				terminal = err
				return
			}
		}
	}
}

var errWatchRootChanged = errors.New("watched root identity changed")
