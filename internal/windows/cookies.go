//go:build windows && (amd64 || arm64)

package windows

import (
	"fmt"
	"github.com/egoist/mygo/internal/platform"
	"math"
	"strings"
	"time"
	"unsafe"
)

// Mechanically checked against Microsoft's Microsoft.Web.WebView2 NuGet
// 1.0.4191.47, build/native/include/WebView2.h (C interface vtable order).
// https://api.nuget.org/v3-flatcontainer/microsoft.web.webview2/1.0.4191.47/microsoft.web.webview2.1.0.4191.47.nupkg
const (
	wv2GetCookieManager = 66
	cookieManagerCreate = 3
	cookieManagerGet    = 5
	cookieManagerAdd    = 6
	cookieManagerDelete = 7
	cookieListCount     = 3
	cookieListValue     = 4
	cookieGetName       = 3
	cookieGetValue      = 4
	cookieGetDomain     = 6
	cookieGetPath       = 7
	cookieGetExpires    = 8
	cookiePutExpires    = 9
	cookieGetHTTPOnly   = 10
	cookiePutHTTPOnly   = 11
	cookieGetSameSite   = 12
	cookiePutSameSite   = 13
	cookieGetSecure     = 14
	cookiePutSecure     = 15
	cookieGetSession    = 16
)

var iidICoreWebView2_2 = guid("9e8f0cf8-e670-4b5e-b2bc-73e061e3184c")

// All state is main-thread-only; closing a controller does not guarantee that
// WebView2 invokes outstanding completion handlers.
type cookieRequest struct {
	window   *window
	manager  uintptr
	finished bool
	done     func([]platform.Cookie, error)
}

var cookieRequests = map[*cookieRequest]struct{}{}

func (r *cookieRequest) finish(c []platform.Cookie, err error) {
	if r.finished {
		return
	}
	r.finished = true
	delete(cookieRequests, r)
	release(r.manager)
	r.manager = 0
	r.done(c, err)
}

func failCookieRequests(w *window, err error) {
	for r := range cookieRequests {
		if r.window == w {
			r.finish(nil, err)
		}
	}
}

func (a appController) withCookieManager(done func([]platform.Cookie, error), start func(*cookieRequest)) {
	var selected *window
	var handle uintptr
	for hwnd, w := range a.b.windows {
		if w.closed || w.surface != nil {
			continue
		}
		if selected == nil || w.ready && !selected.ready || w.ready == selected.ready && hwnd < handle {
			selected = w
			handle = hwnd
		}
	}
	if selected == nil {
		done(nil, fmt.Errorf("mygo: cookies need an open web-page window"))
		return
	}
	r := &cookieRequest{window: selected, done: done}
	cookieRequests[r] = struct{}{}
	selected.withWebViewOr(func() {
		if r.finished {
			return
		}
		view := queryInterface(selected.webview, &iidICoreWebView2_2)
		if view == 0 {
			r.finish(nil, fmt.Errorf("mygo: cookies: requires WebView2 ICoreWebView2_2 (SDK 1.0.705.50+): %w", platform.ErrUnsupported))
			return
		}
		hr := comCall(view, wv2GetCookieManager, uintptr(unsafe.Pointer(&r.manager)))
		release(view)
		if failed(hr) || r.manager == 0 {
			r.finish(nil, hresultError("cookies: get_CookieManager", hr))
			return
		}
		start(r)
	}, func(err error) { r.finish(nil, err) })
}

func (r *cookieRequest) enumerate(visit func(uintptr) error) {
	hr := withHandler(func(hr, list uintptr) {
		if r.finished {
			return
		}
		if failed(hr) || list == 0 {
			r.finish(nil, hresultError("cookies: GetCookies completion", hr))
			return
		}
		if err := visit(list); err != nil {
			r.finish(nil, err)
		}
	}, func(handler uintptr) uintptr {
		return comCall(r.manager, cookieManagerGet, uintptr(unsafe.Pointer(u16(""))), handler)
	})
	if failed(hr) {
		r.finish(nil, hresultError("cookies: GetCookies", hr))
	}
}

func cookieEach(list uintptr, visit func(uintptr) error) error {
	var count uint32
	if hr := comCall(list, cookieListCount, uintptr(unsafe.Pointer(&count))); failed(hr) {
		return hresultError("cookies: get_Count", hr)
	}
	for i := uint32(0); i < count; i++ {
		var c uintptr
		if hr := comCall(list, cookieListValue, uintptr(i), uintptr(unsafe.Pointer(&c))); failed(hr) || c == 0 {
			return hresultError("cookies: GetValueAtIndex", hr)
		}
		err := visit(c)
		release(c)
		if err != nil {
			return err
		}
	}
	return nil
}

func cookieString(c uintptr, slot int) (string, error) {
	var p uintptr
	hr := comCall(c, slot, uintptr(unsafe.Pointer(&p)))
	value := takeWstr(p)
	if failed(hr) {
		return "", hresultError("cookies: string property", hr)
	}
	return value, nil
}

func readWindowsCookie(c uintptr) (platform.Cookie, error) {
	var out platform.Cookie
	for _, field := range []struct {
		slot  int
		value *string
	}{{cookieGetName, &out.Name}, {cookieGetValue, &out.Value}, {cookieGetDomain, &out.Domain}, {cookieGetPath, &out.Path}} {
		v, err := cookieString(c, field.slot)
		if err != nil {
			return out, err
		}
		*field.value = v
	}
	out.Domain = strings.ToLower(out.Domain)
	var secure, httpOnly, session, policy int32
	for _, field := range []struct {
		slot  int
		value *int32
	}{{cookieGetSecure, &secure}, {cookieGetHTTPOnly, &httpOnly}, {cookieGetSession, &session}, {cookieGetSameSite, &policy}} {
		if hr := comCall(c, field.slot, uintptr(unsafe.Pointer(field.value))); failed(hr) {
			return out, hresultError("cookies: property", hr)
		}
	}
	out.Secure = secure != 0
	out.HTTPOnly = httpOnly != 0
	switch policy {
	case 0:
		out.SameSite = "none"
	case 1:
		out.SameSite = "lax"
	case 2:
		out.SameSite = "strict"
	}
	if session == 0 {
		var seconds float64
		if hr := comCall(c, cookieGetExpires, uintptr(unsafe.Pointer(&seconds))); failed(hr) {
			return out, hresultError("cookies: get_Expires", hr)
		}
		if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > 253402300799 {
			return out, fmt.Errorf("mygo: cookies: invalid native expiration")
		}
		out.Expires = time.Unix(int64(seconds), 0).UTC()
	}
	return out, nil
}

func (a appController) ListCookies(done func([]platform.Cookie, error)) {
	a.withCookieManager(done, func(r *cookieRequest) {
		r.enumerate(func(list uintptr) error {
			result := []platform.Cookie{}
			err := cookieEach(list, func(c uintptr) error {
				value, err := readWindowsCookie(c)
				if err == nil {
					result = append(result, value)
				}
				return err
			})
			r.finish(result, err)
			return nil
		})
	})
}

func (a appController) SetCookie(c platform.Cookie, done func(error)) {
	a.withCookieManager(func(_ []platform.Cookie, err error) { done(err) }, func(r *cookieRequest) {
		var nativeCookie uintptr
		hr := comCall(r.manager, cookieManagerCreate, uintptr(unsafe.Pointer(u16(c.Name))), uintptr(unsafe.Pointer(u16(c.Value))), uintptr(unsafe.Pointer(u16(c.Domain))), uintptr(unsafe.Pointer(u16(c.Path))), uintptr(unsafe.Pointer(&nativeCookie)))
		if failed(hr) || nativeCookie == 0 {
			r.finish(nil, hresultError("cookies: CreateCookie", hr))
			return
		}
		defer release(nativeCookie)
		seconds := -1.0
		if !c.Expires.IsZero() {
			seconds = float64(c.Expires.Unix())
		}
		if hr := putCookieExpires(nativeCookie, seconds); failed(hr) {
			r.finish(nil, hresultError("cookies: put_Expires", hr))
			return
		}
		var secure, httpOnly uintptr
		if c.Secure {
			secure = 1
		}
		if c.HTTPOnly {
			httpOnly = 1
		}
		policy := uintptr(1)
		if c.SameSite == "none" {
			policy = 0
		} else if c.SameSite == "strict" {
			policy = 2
		}
		for _, property := range []struct {
			slot  int
			value uintptr
		}{{cookiePutSecure, secure}, {cookiePutHTTPOnly, httpOnly}, {cookiePutSameSite, policy}} {
			if hr := comCall(nativeCookie, property.slot, property.value); failed(hr) {
				r.finish(nil, hresultError("cookies: property setter", hr))
				return
			}
		}
		if hr := comCall(r.manager, cookieManagerAdd, nativeCookie); failed(hr) {
			r.finish(nil, hresultError("cookies: AddOrUpdateCookie", hr))
			return
		}
		r.finish(nil, nil)
	})
}

func (a appController) DeleteCookie(key platform.CookieKey, done func(error)) {
	// Enumerating actual objects also supports nameless native cookies and
	// rejects indistinguishable partitioned entries. No wildcard APIs are used.
	a.withCookieManager(func(_ []platform.Cookie, err error) { done(err) }, func(r *cookieRequest) {
		r.enumerate(func(list uintptr) error {
			var match uintptr
			defer func() { release(match) }()
			err := cookieEach(list, func(c uintptr) error {
				name, err := cookieString(c, cookieGetName)
				if err != nil {
					return err
				}
				domain, err := cookieString(c, cookieGetDomain)
				if err != nil {
					return err
				}
				path, err := cookieString(c, cookieGetPath)
				if err != nil {
					return err
				}
				if name == key.Name && strings.ToLower(domain) == key.Domain && path == key.Path {
					if match != 0 {
						return fmt.Errorf("mygo: cookies: ambiguous cookie key")
					}
					addRef(c)
					match = c
				}
				return nil
			})
			if err != nil {
				return err
			}
			if match != 0 {
				if hr := comCall(r.manager, cookieManagerDelete, match); failed(hr) {
					return hresultError("cookies: DeleteCookie", hr)
				}
			}
			r.finish(nil, nil)
			return nil
		})
	})
}
