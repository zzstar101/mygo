//go:build linux && (amd64 || arm64)

package linux

import (
	"encoding/binary"
	"testing"
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
