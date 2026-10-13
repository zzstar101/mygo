//go:build darwin

package darwin

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

func TestVnodeWatchEventMapping(t *testing.T) {
	d := &vnodeWatch{ids: map[int]uint64{42: 7}}
	for _, tc := range []struct {
		name   string
		flags  uint16
		fflags uint32
		data   int64
		kind   platform.FileWatchKind
	}{
		{name: "write", fflags: syscall.NOTE_WRITE, kind: platform.FileWatchWrite},
		{name: "extend", fflags: syscall.NOTE_EXTEND, kind: platform.FileWatchWrite},
		{name: "attributes", fflags: syscall.NOTE_ATTRIB, kind: platform.FileWatchWrite},
		{name: "link", fflags: syscall.NOTE_LINK, kind: platform.FileWatchWrite},
		{name: "rename", fflags: syscall.NOTE_RENAME},
		{name: "delete", fflags: syscall.NOTE_DELETE},
		{name: "combined", fflags: syscall.NOTE_WRITE | syscall.NOTE_RENAME, kind: platform.FileWatchWrite},
		{name: "error", flags: syscall.EV_ERROR, data: int64(syscall.EACCES), kind: platform.FileWatchLost},
		{name: "successful receipt", flags: syscall.EV_ERROR},
		{name: "revoked", fflags: syscall.NOTE_REVOKE, kind: platform.FileWatchLost},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, ok := d.notice(syscall.Kevent_t{Ident: 42, Flags: tc.flags, Fflags: tc.fflags, Data: tc.data})
			if !ok || n.Target != 7 || n.Kind != tc.kind || n.Err != nil {
				t.Fatalf("notice = %+v, %v", n, ok)
			}
		})
	}
	if _, ok := d.notice(syscall.Kevent_t{Ident: 99, Fflags: syscall.NOTE_WRITE}); ok {
		t.Fatal("accepted retired descriptor")
	}
}

func TestVnodeWatch(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	notices := make(chan platform.FileWatchNotice, 16)
	w, err := (&Backend{}).NewFileWatch(func(n platform.FileWatchNotice) { notices <- n })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	add := func(id uint64, path string) {
		t.Helper()
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.Add(platform.FileWatchTarget{ID: id, Path: path, Directory: info.IsDir(), Info: info}); err != nil {
			t.Fatal(err)
		}
	}
	add(1, dir)
	add(2, file)
	info, _ := os.Lstat(dir)
	if err := w.Add(platform.FileWatchTarget{ID: 3, Path: dir, Directory: true, Info: info, Subtree: true}); err != platform.ErrUnsupported {
		t.Fatalf("subtree: %v", err)
	}
	next := func(target uint64, kind platform.FileWatchKind) {
		t.Helper()
		timeout := time.After(5 * time.Second)
		for {
			select {
			case n := <-notices:
				if n.Target == target && n.Kind == kind {
					return
				}
			case <-timeout:
				t.Fatalf("no notice %v for target %d", kind, target)
			}
		}
	}
	if err := os.WriteFile(file, []byte("written"), 0600); err != nil {
		t.Fatal(err)
	}
	next(2, platform.FileWatchWrite)
	if err := os.WriteFile(filepath.Join(dir, "new"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	next(1, platform.FileWatchWrite)
	w.Remove(2)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	next(1, platform.FileWatchWrite)
}
