// Package watchdriver owns the bounded worker/main-thread handoff shared by
// native file watchers. It contains no enumeration or public event policy.
package watchdriver

import (
	"context"
	"sync"

	"github.com/egoist/mygo/internal/platform"
)

// Driver owns kernel state on one worker. Wake is the only concurrent method.
// Wait must block without polling and return when Wake is called.
type Driver interface {
	Add(platform.FileWatchTarget) error
	Remove(uint64)
	Wait() ([]platform.FileWatchNotice, error)
	Wake()
	Close()
}
type command struct {
	target platform.FileWatchTarget
	remove uint64
}
type completion struct {
	id  uint64
	err error
}
type Watch struct {
	driver      Driver
	post        func(func()) bool
	notify      func(platform.FileWatchNotice)
	commands    chan command
	stop        chan struct{}
	rejected    chan struct{}
	rejectOnce  sync.Once
	done        chan struct{}
	pending     map[uint64]func(error) // main only
	closed      bool                   // main only
	mu          sync.Mutex
	notices     []platform.FileWatchNotice
	completions []completion
	scheduled   bool
	lost        bool
}

// New starts an owner worker. Call its returned methods only on main.
func New(d Driver, post func(func()) bool, notify func(platform.FileWatchNotice)) *Watch {
	w := &Watch{driver: d, post: post, notify: notify, commands: make(chan command, 4096), stop: make(chan struct{}), rejected: make(chan struct{}), done: make(chan struct{}), pending: make(map[uint64]func(error))}
	go w.run()
	return w
}
func (w *Watch) Add(t platform.FileWatchTarget, done func(error)) {
	if w.closed {
		done(context.Canceled)
		return
	}
	w.pending[t.ID] = done
	select {
	case w.commands <- command{target: t}:
		w.driver.Wake()
	default:
		delete(w.pending, t.ID)
		done(platform.ErrFileWatchOverflow)
	}
}
func (w *Watch) Remove(id uint64) {
	if w.closed {
		return
	}
	select {
	case w.commands <- command{remove: id}:
		w.driver.Wake()
	default:
		w.publish(nil, nil, platform.ErrFileWatchOverflow)
	}
}
func (w *Watch) Close() <-chan struct{} {
	if !w.closed {
		w.closed = true
		close(w.stop)
		w.driver.Wake()
		pending := w.pending
		w.pending = make(map[uint64]func(error))
		for _, fn := range pending {
			fn(context.Canceled)
		}
	}
	return w.done
}
func (w *Watch) run() {
	defer close(w.done)
	defer w.driver.Close()
	for {
		select {
		case <-w.rejected:
			return
		case <-w.stop:
			return
		default:
		}
		for draining := true; draining; {
			select {
			case <-w.rejected:
				return
			case <-w.stop:
				return
			case c := <-w.commands:
				if c.remove != 0 {
					w.driver.Remove(c.remove)
				} else {
					err := w.driver.Add(c.target)
					w.publish(nil, &completion{c.target.ID, err}, nil)
				}
			default:
				draining = false
			}
		}
		notices, err := w.driver.Wait()
		w.publish(notices, nil, err)
		if err != nil {
			select {
			case <-w.stop:
			case <-w.rejected:
			}
			return
		}
	}
}
func (w *Watch) publish(notices []platform.FileWatchNotice, c *completion, err error) {
	w.mu.Lock()
	if len(w.notices)+len(notices) > 4096 {
		w.lost = true
	} else {
		w.notices = append(w.notices, notices...)
	}
	if err != nil {
		if len(w.notices) < 4096 {
			w.notices = append(w.notices, platform.FileWatchNotice{Err: err})
		} else {
			w.lost = true
		}
	}
	if c != nil {
		w.completions = append(w.completions, *c)
	}
	schedule := !w.scheduled && (len(w.notices) > 0 || len(w.completions) > 0 || w.lost)
	if schedule {
		w.scheduled = true
	}
	w.mu.Unlock()
	if schedule {
		if !w.post(w.drain) {
			w.rejectOnce.Do(func() { close(w.rejected) })
			w.driver.Wake()
		}
	}
}
func (w *Watch) drain() {
	w.mu.Lock()
	notices, completions, lost := w.notices, w.completions, w.lost
	w.notices = nil
	w.completions = nil
	w.lost = false
	w.scheduled = false
	w.mu.Unlock()
	if w.closed {
		return
	}
	for _, c := range completions {
		if fn := w.pending[c.id]; fn != nil {
			delete(w.pending, c.id)
			fn(c.err)
		}
	}
	if lost {
		w.notify(platform.FileWatchNotice{Err: platform.ErrFileWatchOverflow})
		return
	}
	for _, n := range notices {
		if w.closed {
			return
		}
		w.notify(n)
	}
}
