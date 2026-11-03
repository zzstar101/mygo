//go:build windows

package hotpatch

import (
	"fmt"
	"runtime"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var (
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procVirtualProtect        = kernel32.NewProc("VirtualProtect")
	procFlushInstructionCache = kernel32.NewProc("FlushInstructionCache")
	procGetCurrentProcess     = kernel32.NewProc("GetCurrentProcess")
)

const (
	pageReadonly         = 0x02
	pageExecuteRead      = 0x20
	pageExecuteReadWrite = 0x40
)

func protect(addr, n uint64, prot uintptr) (uintptr, error) {
	var old uint32
	r, _, err := procVirtualProtect.Call(uintptr(addr), uintptr(n), prot, uintptr(unsafe.Pointer(&old)))
	if r == 0 {
		return 0, fmt.Errorf("VirtualProtect: %w", err)
	}
	return uintptr(old), nil
}

func flush(addr, n uint64) {
	self, _, _ := procGetCurrentProcess.Call()
	procFlushInstructionCache.Call(self, uintptr(addr), uintptr(n))
}

// reserve has nothing to do: memory within the image may become executable.
func reserve(at, limit uint64) (uint64, uint64, error) {
	return at, limit - at, nil
}

func protectCode(at, n uint64) error {
	if n == 0 {
		return nil
	}
	if _, err := protect(at, roundPage(n), pageExecuteRead); err != nil {
		return err
	}
	flush(at, n)
	return nil
}

func protectRead(at, n uint64) error {
	_, err := protect(at, n, pageReadonly)
	return err
}

// writeJump replaces the start of a function, whose page stays executable for
// the other threads running around it. The word is aligned and written at once.
func writeJump(from, to uint64) error {
	target := (*uint64)(at(from))
	word, ok := jumpWord(runtime.GOARCH, *target, from, to)
	if !ok {
		return fmt.Errorf("a jump from %#x to %#x reaches too far", from, to)
	}
	old, err := protect(from, 8, pageExecuteReadWrite)
	if err != nil {
		return err
	}
	atomic.StoreUint64(target, word)
	if _, err := protect(from, 8, old); err != nil {
		return err
	}
	flush(from, 8)
	return nil
}
