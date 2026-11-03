//go:build darwin

package main

import (
	"sync"
	"syscall"
)

const oEvtOnly = 0x8000

// kqueueNotifier wakes the watcher when a watched file or directory changes.
// kqueue watches open files: a file an editor replaced is a new one to open.
type kqueueNotifier struct {
	kq   int
	wake chan<- struct{}

	mu  sync.Mutex
	fds map[string]int
}

func newNotifier(wake chan<- struct{}) notifier {
	kq, err := syscall.Kqueue()
	if err != nil {
		return noNotifier{}
	}
	syscall.CloseOnExec(kq)
	n := &kqueueNotifier{kq: kq, wake: wake, fds: map[string]int{}}
	go n.run()
	return n
}

func (n *kqueueNotifier) run() {
	events := make([]syscall.Kevent_t, 16)
	for {
		c, err := syscall.Kevent(n.kq, nil, events, nil)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return
		}
		if c > 0 {
			wakeUp(n.wake)
		}
	}
}

func (n *kqueueNotifier) sync(paths, changed []string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, p := range changed {
		if fd, ok := n.fds[p]; ok {
			syscall.Close(fd)
			delete(n.fds, p)
		}
	}
	keep := make(map[string]bool, len(paths))
	for _, p := range paths {
		keep[p] = true
		if _, ok := n.fds[p]; ok {
			continue
		}
		fd, err := syscall.Open(p, oEvtOnly|syscall.O_CLOEXEC, 0)
		if err != nil {
			continue
		}
		ev := syscall.Kevent_t{
			Ident:  uint64(fd),
			Filter: syscall.EVFILT_VNODE,
			Flags:  syscall.EV_ADD | syscall.EV_CLEAR,
			Fflags: syscall.NOTE_WRITE | syscall.NOTE_DELETE | syscall.NOTE_RENAME | syscall.NOTE_EXTEND | syscall.NOTE_ATTRIB,
		}
		if _, err := syscall.Kevent(n.kq, []syscall.Kevent_t{ev}, nil, nil); err != nil {
			syscall.Close(fd)
			continue
		}
		n.fds[p] = fd
	}
	for p, fd := range n.fds {
		if !keep[p] {
			syscall.Close(fd)
			delete(n.fds, p)
		}
	}
}
