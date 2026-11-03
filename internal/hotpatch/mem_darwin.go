//go:build darwin

package hotpatch

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
)

var mach struct {
	once                    sync.Once
	err                     error
	task                    uint32
	remap, allocate, icache uintptr
}

func machInit() error {
	mach.once.Do(func() {
		lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_GLOBAL)
		if err != nil {
			mach.err = err
			return
		}
		sym := func(n string) uintptr {
			p, err := purego.Dlsym(lib, n)
			if err != nil {
				mach.err = err
			}
			return p
		}
		mach.remap, mach.allocate, mach.icache = sym("mach_vm_remap"), sym("mach_vm_allocate"), sym("sys_icache_invalidate")
		if task := sym("mach_task_self_"); task != 0 {
			mach.task = *(*uint32)(at(uint64(task)))
		}
	})
	return mach.err
}

// reserve makes the part of region between at and limit memory of its own: a
// Mach-O data segment can never become executable, while memory allocated over
// it can.
func reserve(at, limit uint64) (uint64, uint64, error) {
	if err := machInit(); err != nil {
		return 0, 0, err
	}
	const (
		fixed     = 0
		overwrite = 0x4000 // VM_FLAGS_OVERWRITE
	)
	got := at
	r, _, _ := purego.SyscallN(mach.allocate, uintptr(mach.task), uintptr(unsafe.Pointer(&got)), uintptr(limit-at), fixed|overwrite)
	if r != 0 {
		return 0, 0, fmt.Errorf("mach_vm_allocate: %d", r)
	}
	return got, limit - at, nil
}

func protectCode(at, n uint64) error {
	if n == 0 {
		return nil
	}
	n = roundPage(n)
	if _, _, e := syscall.Syscall(syscall.SYS_MPROTECT, uintptr(at), uintptr(n), syscall.PROT_READ|syscall.PROT_EXEC); e != 0 {
		return e
	}
	purego.SyscallN(mach.icache, uintptr(at), uintptr(n))
	return nil
}

func protectRead(at, n uint64) error {
	if _, _, e := syscall.Syscall(syscall.SYS_MPROTECT, uintptr(at), uintptr(n), syscall.PROT_READ); e != 0 {
		return e
	}
	return nil
}

const pageSize = 16384

// writeJump replaces the start of a function. Code cannot be writable and
// running on macOS, and other threads are running around it, so it copies the
// page, changes the copy, makes that executable, and swaps it in with one call:
// no thread ever finds the page gone.
func writeJump(from, to uint64) error {
	word, ok := jumpWord(runtime.GOARCH, *(*uint64)(at(from)), from, to)
	if !ok {
		return fmt.Errorf("a jump from %#x to %#x reaches too far", from, to)
	}
	page := from &^ (pageSize - 1)
	mem, err := syscall.Mmap(-1, 0, pageSize, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		return err
	}
	defer syscall.Munmap(mem)
	copy(mem, bytesAt(page, pageSize))
	if from-page+8 > pageSize {
		return fmt.Errorf("a function starts too late in its page at %#x", from)
	}
	*(*uint64)(unsafe.Pointer(&mem[from-page])) = word
	if err := syscall.Mprotect(mem, syscall.PROT_READ|syscall.PROT_EXEC); err != nil {
		return err
	}
	purego.SyscallN(mach.icache, uintptr(unsafe.Pointer(&mem[0])), pageSize)
	dst := page
	var cur, most uint32
	const (
		overwrite   = 0x4000 // VM_FLAGS_OVERWRITE
		inheritCopy = 1      // VM_INHERIT_COPY
	)
	r, _, _ := purego.SyscallN(mach.remap, uintptr(mach.task), uintptr(unsafe.Pointer(&dst)), pageSize, 0, overwrite,
		uintptr(mach.task), uintptr(unsafe.Pointer(&mem[0])), 0,
		uintptr(unsafe.Pointer(&cur)), uintptr(unsafe.Pointer(&most)), inheritCopy)
	if r != 0 {
		return fmt.Errorf("mach_vm_remap: %d", r)
	}
	purego.SyscallN(mach.icache, uintptr(page), pageSize)
	return nil
}
