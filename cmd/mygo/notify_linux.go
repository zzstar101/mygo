//go:build linux

package main

import (
	"path/filepath"
	"sync"
	"syscall"
)

// inotifyNotifier wakes the watcher when something in a watched directory
// changes. inotify watches paths, so a replaced file needs nothing.
type inotifyNotifier struct {
	fd   int
	wake chan<- struct{}

	mu   sync.Mutex
	dirs map[string]bool
}

func newNotifier(wake chan<- struct{}) notifier {
	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC)
	if err != nil {
		return noNotifier{}
	}
	n := &inotifyNotifier{fd: fd, wake: wake, dirs: map[string]bool{}}
	go n.run()
	return n
}

func (n *inotifyNotifier) run() {
	buf := make([]byte, 4096)
	for {
		c, err := syscall.Read(n.fd, buf)
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

func (n *inotifyNotifier) sync(paths, _ []string) {
	const mask = syscall.IN_MODIFY | syscall.IN_CLOSE_WRITE | syscall.IN_MOVED_TO | syscall.IN_MOVED_FROM | syscall.IN_CREATE | syscall.IN_DELETE | syscall.IN_ATTRIB
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, p := range paths {
		dir := filepath.Dir(p)
		if !n.dirs[dir] {
			// A directory that does not exist yet is the polling's to find.
			if _, err := syscall.InotifyAddWatch(n.fd, dir, mask); err == nil {
				n.dirs[dir] = true
			}
		}
	}
}
