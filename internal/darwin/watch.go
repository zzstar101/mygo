//go:build darwin

package darwin

import (
	"errors"
	"os"
	"sync"
	"syscall"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/watchdriver"
)

type vnodeWatch struct {
	queue  int
	pipe   [2]int
	files  map[uint64]*os.File
	ids    map[int]uint64
	mu     sync.Mutex
	closed bool
}

func (*Backend) NewFileWatch(post func(func()) bool, notify func(platform.FileWatchNotice)) (platform.FileWatch, error) {
	q, err := syscall.Kqueue()
	if err != nil {
		return nil, err
	}
	syscall.CloseOnExec(q)
	d := &vnodeWatch{queue: q, files: make(map[uint64]*os.File), ids: make(map[int]uint64)}
	if err = syscall.Pipe(d.pipe[:]); err != nil {
		syscall.Close(q)
		return nil, err
	}
	for _, fd := range d.pipe {
		syscall.CloseOnExec(fd)
		if err = syscall.SetNonblock(fd, true); err != nil {
			d.Close()
			return nil, err
		}
	}
	change := syscall.Kevent_t{Ident: uint64(d.pipe[0]), Filter: syscall.EVFILT_READ, Flags: syscall.EV_ADD | syscall.EV_CLEAR}
	if _, err = syscall.Kevent(q, []syscall.Kevent_t{change}, nil, nil); err != nil {
		d.Close()
		return nil, err
	}
	return watchdriver.New(d, post, notify), nil
}
func (d *vnodeWatch) Add(t platform.FileWatchTarget) error {
	if !t.Directory && !t.Info.Mode().IsRegular() {
		return nil
	}
	fd, err := syscall.Open(t.Path, syscall.O_EVTONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), t.Path)
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	if !os.SameFile(info, t.Info) {
		f.Close()
		return errors.New("watch target identity changed")
	}
	change := syscall.Kevent_t{Ident: uint64(fd), Filter: syscall.EVFILT_VNODE, Flags: syscall.EV_ADD | syscall.EV_CLEAR | syscall.EV_RECEIPT, Fflags: syscall.NOTE_WRITE | syscall.NOTE_EXTEND | syscall.NOTE_ATTRIB | syscall.NOTE_LINK | syscall.NOTE_RENAME | syscall.NOTE_DELETE | syscall.NOTE_REVOKE}
	var receipt [1]syscall.Kevent_t
	for {
		_, err = syscall.Kevent(d.queue, []syscall.Kevent_t{change}, receipt[:], nil)
		if err != syscall.EINTR {
			break
		}
	}
	if err == nil && receipt[0].Flags&syscall.EV_ERROR != 0 && receipt[0].Data != 0 {
		err = syscall.Errno(receipt[0].Data)
	}
	if err != nil {
		f.Close()
		return err
	}
	d.files[t.ID] = f
	d.ids[fd] = t.ID
	return nil
}
func (d *vnodeWatch) Remove(id uint64) {
	if f := d.files[id]; f != nil {
		delete(d.ids, int(f.Fd()))
		delete(d.files, id)
		f.Close()
	}
}
func (d *vnodeWatch) Wait() ([]platform.FileWatchNotice, error) {
	var events [128]syscall.Kevent_t
	var n int
	var err error
	for {
		n, err = syscall.Kevent(d.queue, nil, events[:], nil)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	var notices []platform.FileWatchNotice
	for _, e := range events[:n] {
		if int(e.Ident) == d.pipe[0] {
			var buf [256]byte
			for {
				_, err := syscall.Read(d.pipe[0], buf[:])
				if err == syscall.EINTR {
					continue
				}
				if err != nil {
					break
				}
			}
			continue
		}
		if notice, ok := d.notice(e); ok {
			notices = append(notices, notice)
		}
	}
	return notices, nil
}

// notice translates one vnode record without requiring a GUI or a live queue.
func (d *vnodeWatch) notice(e syscall.Kevent_t) (platform.FileWatchNotice, bool) {
	id, ok := d.ids[int(e.Ident)]
	if !ok {
		return platform.FileWatchNotice{}, false
	}
	notice := platform.FileWatchNotice{Target: id}
	if e.Flags&syscall.EV_ERROR != 0 && e.Data != 0 {
		notice.Err = syscall.Errno(e.Data)
	}
	if e.Fflags&syscall.NOTE_REVOKE != 0 {
		notice.Err = errors.New("watch target revoked")
	}
	if e.Fflags&(syscall.NOTE_WRITE|syscall.NOTE_EXTEND|syscall.NOTE_ATTRIB|syscall.NOTE_LINK) != 0 {
		notice.Kind = platform.FileWatchWrite
	}
	return notice, true
}

func (d *vnodeWatch) Wake() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.closed {
		for {
			_, err := syscall.Write(d.pipe[1], []byte{1})
			if err != syscall.EINTR {
				break
			}
		}
	}
}
func (d *vnodeWatch) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	for _, f := range d.files {
		f.Close()
	}
	syscall.Close(d.pipe[0])
	syscall.Close(d.pipe[1])
	syscall.Close(d.queue)
}
