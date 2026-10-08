//go:build windows && (amd64 || arm64)

package windows

import (
	"encoding/binary"
	"errors"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/watchdriver"
)

var (
	watchCreateEvent = systemDLL("kernel32.dll").NewProc("CreateEventW")
	watchGetResult   = systemDLL("kernel32.dll").NewProc("GetOverlappedResult")
)

type directoryWatch struct {
	sessions map[uint64]*directorySession
	retired  []*directorySession
	notices  chan platform.FileWatchNotice
	lost     chan struct{}
	wake     chan struct{}
}
type directorySession struct {
	file     *os.File
	handle   syscall.Handle
	overlap  syscall.Overlapped
	storage  [16384]uint32
	pin      runtime.Pinner
	mu       sync.Mutex
	stopping bool
	done     chan struct{}
	target   uint64
	owner    *directoryWatch
}

func (*Backend) NewFileWatch(post func(func()) bool, notify func(platform.FileWatchNotice)) (platform.FileWatch, error) {
	d := &directoryWatch{sessions: make(map[uint64]*directorySession), notices: make(chan platform.FileWatchNotice, 4096), lost: make(chan struct{}, 1), wake: make(chan struct{}, 1)}
	return watchdriver.New(d, post, notify), nil
}
func (d *directoryWatch) Add(t platform.FileWatchTarget) error {
	if !t.Directory {
		return nil
	}
	path, err := syscall.UTF16PtrFromString(t.Path)
	if err != nil {
		return err
	}
	h, err := syscall.CreateFile(path, syscall.FILE_LIST_DIRECTORY, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OVERLAPPED|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(h), t.Path)
	var native syscall.ByHandleFileInformation
	if err = syscall.GetFileInformationByHandle(h, &native); err != nil {
		f.Close()
		return err
	}
	if native.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		f.Close()
		return errors.New("watch target is a reparse point")
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	if !os.SameFile(info, t.Info) {
		f.Close()
		return errors.New("watch target identity changed")
	}
	event, _, e := watchCreateEvent.Call(0, 1, 0, 0)
	if event == 0 {
		f.Close()
		return e
	}
	s := &directorySession{file: f, handle: h, overlap: syscall.Overlapped{HEvent: syscall.Handle(event)}, done: make(chan struct{}), owner: d, target: t.ID}
	s.pin.Pin(&s.overlap)
	s.pin.Pin(&s.storage[0])
	if err = s.arm(); err != nil {
		s.pin.Unpin()
		syscall.CloseHandle(s.overlap.HEvent)
		f.Close()
		return err
	}
	d.sessions[t.ID] = s
	go s.run()
	return nil
}
func (s *directorySession) arm() error {
	// FILE_NOTIFY_CHANGE_FILE_NAME | DIR_NAME | ATTRIBUTES | SIZE | LAST_WRITE.
	err := syscall.ReadDirectoryChanges(s.handle, (*byte)(unsafe.Pointer(&s.storage[0])), 65536, false, 0x1f, nil, &s.overlap, 0)
	if err == syscall.ERROR_IO_PENDING {
		return nil
	}
	return err
}
func (s *directorySession) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.stopping {
		s.stopping = true
		_ = syscall.CancelIoEx(s.handle, &s.overlap)
	}
}
func (d *directoryWatch) Remove(id uint64) {
	if s := d.sessions[id]; s != nil {
		s.stop()
		delete(d.sessions, id)
		d.retired = append(d.retired, s)
	}
}
func (s *directorySession) run() {
	defer close(s.done)
	defer s.file.Close()
	defer syscall.CloseHandle(s.overlap.HEvent)
	defer s.pin.Unpin()
	// Prevent a later Remove/Close from cancelling a recycled HANDLE after
	// this worker has already failed and released its own directory handle.
	defer func() { s.mu.Lock(); s.stopping = true; s.mu.Unlock() }()
	for {
		var size uint32
		ok, _, err := watchGetResult.Call(uintptr(s.handle), uintptr(unsafe.Pointer(&s.overlap)), uintptr(unsafe.Pointer(&size)), 1)
		s.mu.Lock()
		if s.stopping {
			s.mu.Unlock()
			return
		}
		if ok == 0 {
			s.mu.Unlock()
			if err == syscall.Errno(1022) {
				err = platform.ErrFileWatchOverflow
			}
			s.owner.publish(platform.FileWatchNotice{Target: s.target, Err: err})
			return
		}
		if size == 0 {
			s.mu.Unlock()
			s.owner.publish(platform.FileWatchNotice{Target: s.target, Err: platform.ErrFileWatchOverflow})
			return
		}
		if size > 65536 {
			s.mu.Unlock()
			s.owner.publish(platform.FileWatchNotice{Target: s.target, Err: errors.New("oversized directory notification")})
			return
		}
		data := unsafe.Slice((*byte)(unsafe.Pointer(&s.storage[0])), int(size))
		notices, parseErr := parseDirectoryChanges(data, s.target)
		if parseErr == nil {
			parseErr = s.arm()
		}
		s.mu.Unlock()
		if parseErr != nil {
			s.owner.publish(platform.FileWatchNotice{Target: s.target, Err: parseErr})
			return
		}
		for _, n := range notices {
			s.owner.publish(n)
		}
	}
}
func (d *directoryWatch) publish(n platform.FileWatchNotice) {
	select {
	case d.notices <- n:
	default:
		select {
		case d.lost <- struct{}{}:
		default:
		}
	}
}
func (d *directoryWatch) Wait() ([]platform.FileWatchNotice, error) {
	for i := 0; i < len(d.retired); {
		select {
		case <-d.retired[i].done:
			d.retired = append(d.retired[:i], d.retired[i+1:]...)
		default:
			i++
		}
	}
	select {
	case <-d.wake:
		return nil, nil
	case <-d.lost:
		return nil, platform.ErrFileWatchOverflow
	case n := <-d.notices:
		return []platform.FileWatchNotice{n}, nil
	}
}
func (d *directoryWatch) Wake() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}
func (d *directoryWatch) Close() {
	for _, s := range d.sessions {
		s.stop()
	}
	for _, s := range d.retired {
		s.stop()
	}
	for _, s := range d.sessions {
		<-s.done
	}
	for _, s := range d.retired {
		<-s.done
	}
}

func parseDirectoryChanges(data []byte, target uint64) ([]platform.FileWatchNotice, error) {
	var result []platform.FileWatchNotice
	for {
		if len(data) < 12 {
			return nil, errors.New("truncated directory notification")
		}
		next := uint64(binary.LittleEndian.Uint32(data))
		action := binary.LittleEndian.Uint32(data[4:])
		size := uint64(binary.LittleEndian.Uint32(data[8:]))
		if size == 0 || size%2 != 0 || size > uint64(len(data)-12) {
			return nil, errors.New("invalid directory notification name length")
		}
		units := make([]uint16, int(size/2))
		for i := range units {
			units[i] = binary.LittleEndian.Uint16(data[12+2*i:])
		}
		for i := 0; i < len(units); i++ {
			u := units[i]
			if u == 0 {
				return nil, errors.New("NUL in directory notification")
			}
			if u >= 0xd800 && u <= 0xdbff {
				if i+1 >= len(units) || units[i+1] < 0xdc00 || units[i+1] > 0xdfff {
					return nil, errors.New("invalid UTF-16 directory notification")
				}
				i++
			} else if u >= 0xdc00 && u <= 0xdfff {
				return nil, errors.New("invalid UTF-16 directory notification")
			}
		}
		n := platform.FileWatchNotice{Target: target, Name: string(utf16.Decode(units))}
		switch action {
		case 1:
			n.Kind = platform.FileWatchCreate
		case 2:
			n.Kind = platform.FileWatchRemove
		case 3:
			n.Kind = platform.FileWatchWrite
		case 4:
			n.Kind = platform.FileWatchMoveFrom
		case 5:
			n.Kind = platform.FileWatchMoveTo
		default:
			return nil, errors.New("invalid directory notification action")
		}
		result = append(result, n)
		if next == 0 {
			return result, nil
		}
		if next%4 != 0 || next < 12+size || next > uint64(len(data)-12) {
			return nil, errors.New("invalid directory notification offset")
		}
		data = data[int(next):]
	}
}
