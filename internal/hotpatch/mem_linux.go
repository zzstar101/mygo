//go:build linux

package hotpatch

import (
	"fmt"
	"os"
	"runtime"
	"sync/atomic"
	"syscall"
)

func mprotect(at, n uint64, prot int) error {
	_, _, e := syscall.Syscall(syscall.SYS_MPROTECT, uintptr(at), uintptr(n), uintptr(prot))
	if e != 0 {
		return e
	}
	return nil
}

// reserve has nothing to do: memory within the image may become executable.
func reserve(at, limit uint64) (uint64, uint64, error) {
	if ps := uint64(os.Getpagesize()); ps > layoutPage {
		return 0, 0, fmt.Errorf("pages of %d bytes", ps)
	}
	return at, limit - at, nil
}

func protectCode(at, n uint64) error {
	if n == 0 {
		return nil
	}
	if err := mprotect(at, roundPage(n), syscall.PROT_READ|syscall.PROT_EXEC); err != nil {
		return err
	}
	clearCache(at, n)
	return nil
}

func protectRead(at, n uint64) error { return mprotect(at, n, syscall.PROT_READ) }

// writeJump replaces the start of a function, whose page stays executable for
// the other threads running around it. The word is aligned and written at once.
func writeJump(from, to uint64) error {
	target := (*uint64)(at(from))
	word, ok := jumpWord(runtime.GOARCH, *target, from, to)
	if !ok {
		return fmt.Errorf("a jump from %#x to %#x reaches too far", from, to)
	}
	ps := uint64(os.Getpagesize())
	page := from &^ (ps - 1)
	if err := mprotect(page, ps, syscall.PROT_READ|syscall.PROT_WRITE|syscall.PROT_EXEC); err != nil {
		return err
	}
	atomic.StoreUint64(target, word)
	if err := mprotect(page, ps, syscall.PROT_READ|syscall.PROT_EXEC); err != nil {
		return err
	}
	clearCache(from, 8)
	return nil
}
