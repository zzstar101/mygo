package fake

import "github.com/egoist/mygo/internal/platform"

// FileWatch records enrollments and permits deterministic native invalidations.
// All methods, field access and Notify calls belong to main.
type FileWatch struct {
	Targets    map[uint64]platform.FileWatchTarget
	Notify     func(platform.FileWatchNotice)
	AddError   error
	DelayAdd   bool
	DelayClose bool
	pending    []func(error)
	done       chan struct{}
	closed     bool
	backend    *Backend
}

func (b *Backend) NewFileWatch(_ func(func()) bool, notify func(platform.FileWatchNotice)) (platform.FileWatch, error) {
	if !b.IsMainThread() {
		panic("fake: file watch off main")
	}
	if b.FileWatchError != nil {
		return nil, b.FileWatchError
	}
	w := &FileWatch{Targets: make(map[uint64]platform.FileWatchTarget), done: make(chan struct{}), backend: b}
	w.Notify = func(n platform.FileWatchNotice) {
		w.check()
		if !w.closed {
			notify(n)
		}
	}
	b.FileWatches = append(b.FileWatches, w)
	return w, nil
}
func (w *FileWatch) check() {
	if !w.backend.IsMainThread() {
		panic("fake: file watch off main")
	}
}
func (w *FileWatch) Add(t platform.FileWatchTarget, done func(error)) {
	w.check()
	if w.AddError == nil {
		w.Targets[t.ID] = t
	}
	if w.DelayAdd {
		w.pending = append(w.pending, done)
	} else {
		done(w.AddError)
	}
}

// CompleteAdds releases deferred acknowledgments on main.
func (w *FileWatch) CompleteAdds(err error) {
	w.check()
	pending := w.pending
	w.pending = nil
	for _, fn := range pending {
		fn(err)
	}
}
func (w *FileWatch) Remove(id uint64) { w.check(); delete(w.Targets, id) }
func (w *FileWatch) Close() <-chan struct{} {
	w.check()
	if !w.closed {
		w.closed = true
		clear(w.Targets)
		w.CompleteAdds(platform.ErrUnsupported)
		if !w.DelayClose {
			close(w.done)
		}
	}
	return w.done
}

// CompleteClose may run on any goroutine, independently of main dispatch.
func (w *FileWatch) CompleteClose() { close(w.done) }
