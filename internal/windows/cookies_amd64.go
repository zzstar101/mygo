//go:build windows && amd64

package windows

import "math"

func putCookieExpires(cookie uintptr, seconds float64) uintptr {
	return comCall(cookie, cookiePutExpires, uintptr(math.Float64bits(seconds)))
}
