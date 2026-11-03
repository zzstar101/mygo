//go:build windows

package main

import (
	"path/filepath"
	"sync"
	"syscall"
)

// directoryNotifier wakes the watcher when something in a watched directory
// changes, with a goroutine blocked in ReadDirectoryChangesW per directory.
type directoryNotifier struct {
	wake chan<- struct{}

	mu   sync.Mutex
	dirs map[string]bool
}

func newNotifier(wake chan<- struct{}) notifier {
	return &directoryNotifier{wake: wake, dirs: map[string]bool{}}
}

func (n *directoryNotifier) sync(paths, _ []string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, p := range paths {
		dir := filepath.Dir(p)
		if !n.dirs[dir] {
			n.dirs[dir] = true
			go n.watch(dir)
		}
	}
}

func (n *directoryNotifier) watch(dir string) {
	name, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return
	}
	const listDirectory = 1 // FILE_LIST_DIRECTORY
	h, err := syscall.CreateFile(name, listDirectory,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil,
		syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		// Polling finds it, once it exists.
		n.mu.Lock()
		delete(n.dirs, dir)
		n.mu.Unlock()
		return
	}
	defer syscall.CloseHandle(h)
	const filter = syscall.FILE_NOTIFY_CHANGE_FILE_NAME | syscall.FILE_NOTIFY_CHANGE_DIR_NAME | syscall.FILE_NOTIFY_CHANGE_ATTRIBUTES | syscall.FILE_NOTIFY_CHANGE_SIZE | syscall.FILE_NOTIFY_CHANGE_LAST_WRITE
	buf := make([]byte, 4096)
	for {
		var got uint32
		if err := syscall.ReadDirectoryChanges(h, &buf[0], uint32(len(buf)), false, filter, &got, nil, 0); err != nil {
			n.mu.Lock()
			delete(n.dirs, dir)
			n.mu.Unlock()
			return
		}
		wakeUp(n.wake)
	}
}
