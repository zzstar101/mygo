//go:build windows && (amd64 || arm64)

package windows

import (
	"encoding/binary"
	"testing"
)

func TestWatchDirectoryParser(t *testing.T) {
	record := make([]byte, 16)
	binary.LittleEndian.PutUint32(record[4:], 1)
	binary.LittleEndian.PutUint32(record[8:], 2)
	binary.LittleEndian.PutUint16(record[12:], 'a')
	got, err := parseDirectoryChanges(record, 42)
	if err != nil || len(got) != 1 || got[0].Name != "a" || got[0].Target != 42 {
		t.Fatalf("%+v %v", got, err)
	}
	for _, mutate := range []func([]byte){
		func(b []byte) { binary.LittleEndian.PutUint32(b[8:], 3) },
		func(b []byte) { binary.LittleEndian.PutUint32(b[8:], 100) },
		func(b []byte) { binary.LittleEndian.PutUint32(b, 3) },
		func(b []byte) { binary.LittleEndian.PutUint16(b[12:], 0xd800) },
		func(b []byte) { binary.LittleEndian.PutUint16(b[12:], 0xdc00) },
		func(b []byte) { binary.LittleEndian.PutUint32(b[4:], 99) },
	} {
		b := append([]byte(nil), record...)
		mutate(b)
		if _, err := parseDirectoryChanges(b, 42); err == nil {
			t.Fatalf("accepted malformed %v", b)
		}
	}
	for i := 0; i < 14; i++ {
		if _, err := parseDirectoryChanges(record[:i], 42); err == nil {
			t.Fatalf("accepted length %d", i)
		}
	}
}
