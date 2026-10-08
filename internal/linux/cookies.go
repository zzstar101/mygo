//go:build linux && (amd64 || arm64)

package linux

import (
	"fmt"
	"github.com/egoist/mygo/internal/platform"
	"time"
	"unsafe"
)

var (
	soup3                                                                                bool
	cookieManager                                                                        func(ptr) ptr
	cookieList                                                                           func(ptr, ptr, ptr, ptr)
	cookieListFinish                                                                     func(ptr, ptr, *ptr) ptr
	cookieAdd, cookieDelete                                                              func(ptr, ptr, ptr, ptr, ptr)
	cookieAddFinish, cookieDeleteFinish                                                  func(ptr, ptr, *ptr) int32
	soupCookieNew                                                                        func(*byte, *byte, *byte, *byte, int32) ptr
	soupCookieFree                                                                       func(ptr)
	soupCookieName, soupCookieValue, soupCookieDomain, soupCookiePath, soupCookieExpires func(ptr) ptr
	soupCookieSecure, soupCookieHTTPOnly, soupCookieSameSite                             func(ptr) int32
	soupCookieSetSecure, soupCookieSetHTTPOnly, soupCookieSetSameSite                    func(ptr, int32)
	soupCookieSetExpires                                                                 func(ptr, ptr)
	cookieDateNew                                                                        func(int64) ptr
	cookieDateUnix                                                                       func(ptr) int64
	cookieDateFree                                                                       func(ptr)
	cookieListFree                                                                       func(ptr)
)

func bindCookies() {
	bind(libWebKit, &cookieManager, "webkit_web_context_get_cookie_manager")
	bind(libWebKit, &cookieList, "webkit_cookie_manager_get_all_cookies")
	bind(libWebKit, &cookieListFinish, "webkit_cookie_manager_get_all_cookies_finish")
	bind(libWebKit, &cookieAdd, "webkit_cookie_manager_add_cookie")
	bind(libWebKit, &cookieAddFinish, "webkit_cookie_manager_add_cookie_finish")
	bind(libWebKit, &cookieDelete, "webkit_cookie_manager_delete_cookie")
	bind(libWebKit, &cookieDeleteFinish, "webkit_cookie_manager_delete_cookie_finish")
	bind(libSoup, &soupCookieNew, "soup_cookie_new")
	bind(libSoup, &soupCookieFree, "soup_cookie_free")
	bind(libSoup, &soupCookieName, "soup_cookie_get_name")
	bind(libSoup, &soupCookieValue, "soup_cookie_get_value")
	bind(libSoup, &soupCookieDomain, "soup_cookie_get_domain")
	bind(libSoup, &soupCookiePath, "soup_cookie_get_path")
	bind(libSoup, &soupCookieExpires, "soup_cookie_get_expires")
	bind(libSoup, &soupCookieSecure, "soup_cookie_get_secure")
	bind(libSoup, &soupCookieHTTPOnly, "soup_cookie_get_http_only")
	bind(libSoup, &soupCookieSameSite, "soup_cookie_get_same_site_policy")
	bind(libSoup, &soupCookieSetSecure, "soup_cookie_set_secure")
	bind(libSoup, &soupCookieSetHTTPOnly, "soup_cookie_set_http_only")
	bind(libSoup, &soupCookieSetSameSite, "soup_cookie_set_same_site_policy")
	bind(libSoup, &soupCookieSetExpires, "soup_cookie_set_expires")
	bind(libGLib, &cookieListFree, "g_list_free")
	// GLib 2.80 gdatetime.h and libsoup 2.74 soup-date.h: Soup3 uses
	// refcounted GDateTime, Soup2 copied SoupDate; time_t is 64-bit on our
	// supported Linux amd64/arm64 targets. Never share their pointer types.
	if soup3 {
		bind(libGLib, &cookieDateNew, "g_date_time_new_from_unix_utc")
		bind(libGLib, &cookieDateUnix, "g_date_time_to_unix")
		bind(libGLib, &cookieDateFree, "g_date_time_unref")
	} else {
		bind(libSoup, &cookieDateNew, "soup_date_new_from_time_t")
		bind(libSoup, &cookieDateUnix, "soup_date_to_time_t")
		bind(libSoup, &cookieDateFree, "soup_date_free")
	}
}

func linuxCookieManager(operation string, available bool) (ptr, error) {
	if !available || cookieManager == nil || soupCookieNew == nil || soupCookieFree == nil {
		return 0, fmt.Errorf("mygo: cookies: %s requires WebKitGTK cookie APIs (List 2.42+, mutations 2.20+): %w", operation, platform.ErrUnsupported)
	}
	return cookieManager(webkitWebContextGetDefault()), nil
}

func (a appController) ListCookies(done func([]platform.Cookie, error)) {
	if err := webKit(); err != nil {
		done(nil, fmt.Errorf("mygo: cookies: List: %w", err))
		return
	}
	manager, err := linuxCookieManager("List", cookieList != nil && cookieListFinish != nil && cookieListFree != nil && soupCookieName != nil && soupCookieValue != nil && soupCookieDomain != nil && soupCookiePath != nil && soupCookieExpires != nil && soupCookieSecure != nil && soupCookieHTTPOnly != nil && cookieDateUnix != nil)
	if err != nil {
		done(nil, err)
		return
	}
	request := pending.add(func(source, result ptr) {
		var ge ptr
		list := cookieListFinish(source, result, &ge)
		values := []platform.Cookie{}
		// GList is three pointers, unlike GSList. WebKit transfers both the
		// list and every SoupCookie; clean them even on finish errors.
		for node := list; node != 0; {
			entry := *(**[3]ptr)(unsafe.Pointer(&node))
			c := entry[0]
			node = entry[1]
			out := platform.Cookie{Name: goStr(soupCookieName(c)), Value: goStr(soupCookieValue(c)), Domain: goStr(soupCookieDomain(c)), Path: goStr(soupCookiePath(c)), Secure: soupCookieSecure(c) != 0, HTTPOnly: soupCookieHTTPOnly(c) != 0}
			if d := soupCookieExpires(c); d != 0 {
				out.Expires = time.Unix(cookieDateUnix(d), 0).UTC()
			}
			if soupCookieSameSite != nil {
				switch soupCookieSameSite(c) {
				case 0:
					out.SameSite = "none"
				case 1:
					out.SameSite = "lax"
				case 2:
					out.SameSite = "strict"
				}
			}
			values = append(values, out)
			soupCookieFree(c)
		}
		cookieListFree(list)
		done(values, gErr(ge))
	})
	cookieList(manager, 0, cbAsyncReady, request)
}

func (a appController) SetCookie(c platform.Cookie, done func(error)) {
	if err := webKit(); err != nil {
		done(fmt.Errorf("mygo: cookies: Set: %w", err))
		return
	}
	manager, err := linuxCookieManager("Set", cookieAdd != nil && cookieAddFinish != nil && soupCookieSetSecure != nil && soupCookieSetHTTPOnly != nil && soupCookieSetSameSite != nil && soupCookieSetExpires != nil && cookieDateNew != nil && cookieDateFree != nil)
	if err != nil {
		done(err)
		return
	}
	native := soupCookieNew(cs(c.Name), cs(c.Value), cs(c.Domain), cs(c.Path), -1)
	if native == 0 {
		done(fmt.Errorf("mygo: cookies: Set could not construct SoupCookie"))
		return
	}
	var secure, httpOnly int32
	if c.Secure {
		secure = 1
	}
	if c.HTTPOnly {
		httpOnly = 1
	}
	soupCookieSetSecure(native, secure)
	soupCookieSetHTTPOnly(native, httpOnly)
	policy := int32(1)
	if c.SameSite == "none" {
		policy = 0
	} else if c.SameSite == "strict" {
		policy = 2
	}
	soupCookieSetSameSite(native, policy)
	if !c.Expires.IsZero() {
		date := cookieDateNew(c.Expires.Unix())
		if date == 0 {
			soupCookieFree(native)
			done(fmt.Errorf("mygo: cookies: Set cannot represent expiration"))
			return
		}
		soupCookieSetExpires(native, date)
		cookieDateFree(date)
	}
	request := pending.add(func(source, result ptr) {
		var ge ptr
		ok := cookieAddFinish(source, result, &ge)
		soupCookieFree(native)
		done(cookieFinishError("Set", ok, ge))
	})
	cookieAdd(manager, native, 0, cbAsyncReady, request)
}

func (a appController) DeleteCookie(key platform.CookieKey, done func(error)) {
	if err := webKit(); err != nil {
		done(fmt.Errorf("mygo: cookies: Delete: %w", err))
		return
	}
	manager, err := linuxCookieManager("Delete", cookieDelete != nil && cookieDeleteFinish != nil)
	if err != nil {
		done(err)
		return
	}
	native := soupCookieNew(cs(key.Name), cs(""), cs(key.Domain), cs(key.Path), -1)
	if native == 0 {
		done(fmt.Errorf("mygo: cookies: Delete could not construct SoupCookie"))
		return
	}
	request := pending.add(func(source, result ptr) {
		var ge ptr
		ok := cookieDeleteFinish(source, result, &ge)
		soupCookieFree(native)
		done(cookieFinishError("Delete", ok, ge))
	})
	cookieDelete(manager, native, 0, cbAsyncReady, request)
}

func cookieFinishError(operation string, ok int32, ge ptr) error {
	if err := gErr(ge); err != nil {
		return fmt.Errorf("mygo: cookies: %s: %w", operation, err)
	}
	if ok == 0 {
		return fmt.Errorf("mygo: cookies: %s failed", operation)
	}
	return nil
}
