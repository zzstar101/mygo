//go:build darwin

package darwin

import (
	"errors"
	"syscall"
	"testing"

	"github.com/egoist/mygo/internal/platform"
)

func TestVnodeWatchEventMapping(t *testing.T) {
	d := &vnodeWatch{ids: map[int]uint64{42: 7}}
	for _, tc := range []struct {
		name    string
		flags   uint16
		fflags  uint32
		data    int64
		kind    platform.FileWatchKind
		wantErr error
		revoked bool
	}{
		{name: "write", fflags: syscall.NOTE_WRITE, kind: platform.FileWatchWrite},
		{name: "extend", fflags: syscall.NOTE_EXTEND, kind: platform.FileWatchWrite},
		{name: "attributes", fflags: syscall.NOTE_ATTRIB, kind: platform.FileWatchWrite},
		{name: "link", fflags: syscall.NOTE_LINK, kind: platform.FileWatchWrite},
		{name: "rename", fflags: syscall.NOTE_RENAME},
		{name: "delete", fflags: syscall.NOTE_DELETE},
		{name: "combined", fflags: syscall.NOTE_WRITE | syscall.NOTE_RENAME, kind: platform.FileWatchWrite},
		{name: "error", flags: syscall.EV_ERROR, data: int64(syscall.EACCES), wantErr: syscall.EACCES},
		{name: "successful receipt", flags: syscall.EV_ERROR},
		{name: "revoked", fflags: syscall.NOTE_REVOKE, revoked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, ok := d.notice(syscall.Kevent_t{Ident: 42, Flags: tc.flags, Fflags: tc.fflags, Data: tc.data})
			if !ok || n.Target != 7 || n.Kind != tc.kind {
				t.Fatalf("notice = %+v, %v", n, ok)
			}
			if tc.revoked {
				if n.Err == nil || n.Err.Error() != "watch target revoked" {
					t.Fatalf("error = %v", n.Err)
				}
			} else if !errors.Is(n.Err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", n.Err, tc.wantErr)
			}
		})
	}
	if _, ok := d.notice(syscall.Kevent_t{Ident: 99, Fflags: syscall.NOTE_WRITE}); ok {
		t.Fatal("accepted retired descriptor")
	}
}
