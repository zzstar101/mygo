//go:build linux && (amd64 || arm64)

package linux

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

func TestWatchInotifyParser(t *testing.T) {
	record := make([]byte, 20)
	binary.LittleEndian.PutUint32(record, 5)
	binary.LittleEndian.PutUint32(record[8:], 77)
	binary.LittleEndian.PutUint32(record[12:], 4)
	copy(record[16:], "abc")
	got, err := parseInotify(record)
	if err != nil || len(got) != 1 || got[0].name != "abc" || got[0].cookie != 77 || got[0].wd != 5 {
		t.Fatalf("%+v %v", got, err)
	}
	for i := 1; i < len(record); i++ {
		if _, err := parseInotify(record[:i]); err == nil {
			t.Fatalf("accepted truncation %d", i)
		}
	}
	malformed := append([]byte(nil), record...)
	malformed[19] = 'x'
	if _, err := parseInotify(malformed); err == nil {
		t.Fatal("accepted unterminated name")
	}
	binary.LittleEndian.PutUint32(malformed[12:], 0xffffffff)
	if _, err := parseInotify(malformed); err == nil {
		t.Fatal("accepted oversized name")
	}
}

func TestInotifyWatch(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	notices := make(chan platform.FileWatchNotice, 16)
	w, err := (&Backend{}).NewFileWatch(func(n platform.FileWatchNotice) { notices <- n })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	add := func(id uint64, path string, subtree bool) error {
		t.Helper()
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		return w.Add(platform.FileWatchTarget{ID: id, Path: path, Directory: info.IsDir(), Info: info, Subtree: subtree})
	}
	if err := add(1, dir, true); err != platform.ErrUnsupported {
		t.Fatalf("subtree: %v", err)
	}
	for id, path := range map[uint64]string{1: dir, 2: sub} {
		if err := add(id, path, false); err != nil {
			t.Fatal(err)
		}
	}
	next := func(want platform.FileWatchNotice) {
		t.Helper()
		timeout := time.After(5 * time.Second)
		for {
			select {
			case n := <-notices:
				if n == want {
					return
				}
				if n.Err != nil || n.Kind == platform.FileWatchLost {
					t.Fatalf("notice %+v", n)
				}
			case <-timeout:
				t.Fatalf("no notice %+v", want)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(sub, "file"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	next(platform.FileWatchNotice{Target: 2, Kind: platform.FileWatchCreate, Name: "file"})
	if err := os.WriteFile(filepath.Join(sub, "file"), []byte("written"), 0600); err != nil {
		t.Fatal(err)
	}
	next(platform.FileWatchNotice{Target: 2, Kind: platform.FileWatchWrite, Name: "file"})
	// A directory deleted is a removal, not a lost watch.
	if err := os.RemoveAll(sub); err != nil {
		t.Fatal(err)
	}
	next(platform.FileWatchNotice{Target: 2, Kind: platform.FileWatchRemove})
	next(platform.FileWatchNotice{Target: 1, Kind: platform.FileWatchRemove, Name: "sub"})
	w.Remove(2)
	if err := os.Rename(filepath.Join(dir, "missing"), filepath.Join(dir, "x")); err == nil {
		t.Fatal("renamed nothing")
	}
	if err := os.WriteFile(filepath.Join(dir, "a"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "a"), filepath.Join(dir, "b")); err != nil {
		t.Fatal(err)
	}
	next(platform.FileWatchNotice{Target: 1, Kind: platform.FileWatchMoveFrom, Name: "a"})
	next(platform.FileWatchNotice{Target: 1, Kind: platform.FileWatchMoveTo, Name: "b"})
}
