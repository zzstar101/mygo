//go:build unix

package hotlink

import (
	"os"
	"syscall"
)

// mapFile maps a file to read. It stays mapped for as long as mygo dev runs:
// sessions keep referring to it.
func mapFile(file string) ([]byte, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() == 0 {
		return nil, nil
	}
	return syscall.Mmap(int(f.Fd()), 0, int(st.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
}
