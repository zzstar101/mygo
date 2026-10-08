//go:build windows && arm64

package windows

import (
	"math"
	"syscall"
	"unsafe"
)

func cookieExpiresThunkAddress() uintptr

func putCookieExpires(cookie uintptr, seconds float64) uintptr {
	vtable := *(*uintptr)(native(cookie))
	method := *(*uintptr)(unsafe.Add(native(vtable), cookiePutExpires*8))
	result, _, _ := syscall.SyscallN(cookieExpiresThunkAddress(), cookie, method, uintptr(math.Float64bits(seconds)))
	return result
}
