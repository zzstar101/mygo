//go:build linux && (amd64 || arm64)

package linux

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"sync"
	"syscall"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/watchdriver"
)

type inotifyGeneration struct {
	ids     []uint64
	retired bool
}
type inotifyWatch struct {
	fd, epoll   int
	pipe        [2]int
	generations map[int][]*inotifyGeneration
	targets     map[uint64]int
	mu          sync.Mutex
	closed      bool
}

func (*Backend) NewFileWatch(post func(func()) bool, notify func(platform.FileWatchNotice)) (platform.FileWatch, error) {
	fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
	if err != nil {
		return nil, err
	}
	ep, err := syscall.EpollCreate1(syscall.EPOLL_CLOEXEC)
	if err != nil {
		syscall.Close(fd)
		return nil, err
	}
	d := &inotifyWatch{fd: fd, epoll: ep, targets: make(map[uint64]int), generations: make(map[int][]*inotifyGeneration)}
	if err = syscall.Pipe2(d.pipe[:], syscall.O_NONBLOCK|syscall.O_CLOEXEC); err != nil {
		syscall.Close(fd)
		syscall.Close(ep)
		return nil, err
	}
	for _, f := range []int{fd, d.pipe[0]} {
		if err = syscall.EpollCtl(ep, syscall.EPOLL_CTL_ADD, f, &syscall.EpollEvent{Events: syscall.EPOLLIN, Fd: int32(f)}); err != nil {
			d.Close()
			return nil, err
		}
	}
	return watchdriver.New(d, post, notify), nil
}
func (d *inotifyWatch) Add(t platform.FileWatchTarget) error {
	if !t.Directory {
		return nil
	}
	fd, err := syscall.Open(t.Path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), t.Path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(info, t.Info) {
		return errors.New("watch target identity changed")
	}
	mask := uint32(syscall.IN_CREATE | syscall.IN_MODIFY | syscall.IN_CLOSE_WRITE | syscall.IN_ATTRIB | syscall.IN_DELETE | syscall.IN_MOVED_FROM | syscall.IN_MOVED_TO | syscall.IN_DELETE_SELF | syscall.IN_MOVE_SELF | syscall.IN_UNMOUNT | syscall.IN_ONLYDIR | syscall.IN_DONT_FOLLOW)
	wd, err := syscall.InotifyAddWatch(d.fd, t.Path, mask)
	if err != nil {
		return err
	}
	info, err = os.Lstat(t.Path)
	if err != nil || !os.SameFile(info, t.Info) {
		syscall.InotifyRmWatch(d.fd, uint32(wd))
		if err != nil {
			return err
		}
		return errors.New("watch target identity changed")
	}
	gens := d.generations[wd]
	if len(gens) == 0 || gens[len(gens)-1].retired {
		gens = append(gens, &inotifyGeneration{})
	}
	gens[len(gens)-1].ids = append(gens[len(gens)-1].ids, t.ID)
	d.generations[wd] = gens
	d.targets[t.ID] = wd
	return nil
}
func (d *inotifyWatch) Remove(id uint64) {
	wd, ok := d.targets[id]
	if !ok {
		return
	}
	delete(d.targets, id)
	for _, g := range d.generations[wd] {
		for i, v := range g.ids {
			if v == id {
				g.ids = append(g.ids[:i], g.ids[i+1:]...)
				if len(g.ids) == 0 && !g.retired {
					g.retired = true
					syscall.InotifyRmWatch(d.fd, uint32(wd))
				}
				return
			}
		}
	}
}

type inotifyRecord struct {
	wd           int
	mask, cookie uint32
	name         string
}

func parseInotify(buf []byte) ([]inotifyRecord, error) {
	var records []inotifyRecord
	for len(buf) > 0 {
		if len(buf) < 16 {
			return nil, errors.New("truncated inotify record")
		}
		length := uint64(binary.LittleEndian.Uint32(buf[12:16]))
		if length > uint64(len(buf)-16) {
			return nil, errors.New("oversized inotify name")
		}
		name := ""
		if length > 0 {
			data := buf[16 : 16+int(length)]
			end := bytes.IndexByte(data, 0)
			if end < 0 {
				return nil, errors.New("unterminated inotify name")
			}
			name = string(data[:end])
		}
		records = append(records, inotifyRecord{int(int32(binary.LittleEndian.Uint32(buf))), binary.LittleEndian.Uint32(buf[4:8]), binary.LittleEndian.Uint32(buf[8:12]), name})
		buf = buf[16+int(length):]
	}
	return records, nil
}
func (d *inotifyWatch) Wait() ([]platform.FileWatchNotice, error) {
	var events [2]syscall.EpollEvent
	var n int
	var err error
	for {
		n, err = syscall.EpollWait(d.epoll, events[:], -1)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	var notices []platform.FileWatchNotice
	for _, e := range events[:n] {
		if int(e.Fd) == d.pipe[0] {
			var b [256]byte
			for {
				_, err := syscall.Read(d.pipe[0], b[:])
				if err == syscall.EINTR {
					continue
				}
				if err != nil {
					break
				}
			}
			continue
		}
		var buf [65536]byte
		for {
			size, err := syscall.Read(d.fd, buf[:])
			if err == syscall.EINTR {
				continue
			}
			if err == syscall.EAGAIN {
				break
			}
			if err != nil {
				return nil, err
			}
			if size == 0 {
				return nil, errors.New("inotify stream ended")
			}
			records, err := parseInotify(buf[:size])
			if err != nil {
				return nil, err
			}
			for _, r := range records {
				if r.mask&syscall.IN_Q_OVERFLOW != 0 {
					return nil, platform.ErrFileWatchOverflow
				}
				gens := d.generations[r.wd]
				if len(gens) == 0 {
					continue
				}
				g := gens[0]
				if r.mask&syscall.IN_IGNORED != 0 {
					if len(gens) == 1 {
						delete(d.generations, r.wd)
					} else {
						d.generations[r.wd] = gens[1:]
					}
					if !g.retired {
						return nil, errors.New("inotify watch revoked")
					}
					continue
				}
				if g.retired || len(g.ids) == 0 {
					continue
				}
				notice := platform.FileWatchNotice{Target: g.ids[len(g.ids)-1], Name: r.name, Cookie: uint64(r.cookie)}
				switch {
				case r.mask&syscall.IN_UNMOUNT != 0:
					notice.Err = errors.New("watched filesystem unmounted")
				case r.mask&syscall.IN_MOVED_FROM != 0:
					notice.Kind = platform.FileWatchMoveFrom
				case r.mask&syscall.IN_MOVED_TO != 0:
					notice.Kind = platform.FileWatchMoveTo
				case r.mask&syscall.IN_CREATE != 0:
					notice.Kind = platform.FileWatchCreate
				case r.mask&(syscall.IN_DELETE|syscall.IN_DELETE_SELF) != 0:
					notice.Kind = platform.FileWatchRemove
				case r.mask&(syscall.IN_MODIFY|syscall.IN_CLOSE_WRITE|syscall.IN_ATTRIB) != 0:
					notice.Kind = platform.FileWatchWrite
				}
				// A deleted directory's automatic IN_IGNORED is expected;
				// retain its removal hint instead of turning it into stream loss.
				if r.mask&syscall.IN_DELETE_SELF != 0 {
					g.retired = true
				}
				notices = append(notices, notice)
				if len(notices) > 4096 {
					return nil, platform.ErrFileWatchOverflow
				}
			}
		}
	}
	return notices, nil
}
func (d *inotifyWatch) Wake() {
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
func (d *inotifyWatch) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	syscall.Close(d.fd)
	syscall.Close(d.pipe[0])
	syscall.Close(d.pipe[1])
	syscall.Close(d.epoll)
}
