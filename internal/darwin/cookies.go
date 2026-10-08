//go:build darwin

package darwin

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ebitengine/purego/objc"
	"github.com/egoist/mygo/internal/platform"
)

// Prewarm purego's signature-keyed block trampolines during startup. Request
// blocks own closures, but do not allocate another native callback trampoline.
func warmCookieBlocks() {
	b := newBlock(func(objc.Block, id) {})
	b.Release()
	b = newBlock(func(objc.Block) {})
	b.Release()
}

func cookieStore() (id, error) {
	store := send(class("WKWebsiteDataStore"), "defaultDataStore")
	if !sendBool(store, "respondsToSelector:", uintptr(sel("httpCookieStore"))) || !sendBool(class("NSHTTPCookie"), "instancesRespondToSelector:", uintptr(sel("sameSitePolicy"))) {
		return 0, fmt.Errorf("mygo: cookies: requires macOS 10.15 or newer: %w", platform.ErrUnsupported)
	}
	return send(store, "httpCookieStore"), nil
}

func readCookie(c id) platform.Cookie {
	out := platform.Cookie{Name: goString(send(c, "name")), Value: goString(send(c, "value")), Domain: strings.ToLower(goString(send(c, "domain"))), Path: goString(send(c, "path")), Secure: sendBool(c, "isSecure"), HTTPOnly: sendBool(c, "isHTTPOnly")}
	out.SameSite = strings.ToLower(goString(send(c, "sameSitePolicy")))
	date := send(c, "expiresDate")
	if date != 0 && !sendBool(c, "isSessionOnly") {
		out.Expires = time.Unix(int64(msgFloat(date, sel("timeIntervalSince1970"))), 0).UTC()
	}
	return out
}

func (a appController) ListCookies(done func([]platform.Cookie, error)) {
	withPool(func() {
		store, err := cookieStore()
		if err != nil {
			done(nil, err)
			return
		}
		block := newBlock(func(_ objc.Block, array id) {
			var result []platform.Cookie
			withPool(func() {
				for _, c := range arrayItems(array) {
					result = append(result, readCookie(c))
				}
			})
			a.b.runOnMain(func() { done(result, nil) })
		})
		send(store, "getAllCookies:", uintptr(block))
		block.Release()
	})
}

func (a appController) SetCookie(c platform.Cookie, done func(error)) {
	withPool(func() {
		store, err := cookieStore()
		if err != nil {
			done(err)
			return
		}
		host := strings.TrimPrefix(c.Domain, ".")
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		scheme := "http"
		if c.Secure {
			scheme = "https"
		}
		header := c.Name + "=" + c.Value + "; Path=" + c.Path + "; SameSite=" + c.SameSite
		if strings.HasPrefix(c.Domain, ".") {
			header += "; Domain=" + c.Domain
		}
		if !c.Expires.IsZero() {
			header += "; Expires=" + c.Expires.Format(http.TimeFormat)
		}
		if c.Secure {
			header += "; Secure"
		}
		if c.HTTPOnly {
			header += "; HttpOnly"
		}
		headers := send(class("NSDictionary"), "dictionaryWithObject:forKey:", uintptr(nsString(header)), uintptr(nsString("Set-Cookie")))
		array := send(class("NSHTTPCookie"), "cookiesWithResponseHeaderFields:forURL:", uintptr(headers), uintptr(nsURL(scheme+"://"+host+"/")))
		if sendInt(array, "count") != 1 {
			done(fmt.Errorf("mygo: cookies: Set could not construct NSHTTPCookie"))
			return
		}
		native := send(array, "objectAtIndex:", 0)
		parsed := readCookie(native)
		if parsed.Name != c.Name || parsed.Domain != c.Domain || parsed.Path != c.Path || parsed.Value != c.Value || parsed.Secure != c.Secure || parsed.HTTPOnly != c.HTTPOnly || parsed.SameSite != c.SameSite {
			done(fmt.Errorf("mygo: cookies: Set parser cannot preserve requested identity or attributes"))
			return
		}
		block := newBlock(func(objc.Block) { a.b.runOnMain(func() { done(nil) }) })
		send(store, "setCookie:completionHandler:", uintptr(native), uintptr(block))
		block.Release()
	})
}

func (a appController) DeleteCookie(key platform.CookieKey, done func(error)) {
	withPool(func() {
		store, err := cookieStore()
		if err != nil {
			done(err)
			return
		}
		retain(store)
		block := newBlock(func(_ objc.Block, array id) {
			// The borrowed array must survive dispatch to main.
			retain(array)
			a.b.runOnMain(func() {
				withPool(func() {
					defer release(array)
					var match id
					for _, c := range arrayItems(array) {
						if goString(send(c, "name")) == key.Name && strings.ToLower(goString(send(c, "domain"))) == key.Domain && goString(send(c, "path")) == key.Path {
							if match != 0 {
								release(store)
								done(fmt.Errorf("mygo: cookies: ambiguous cookie key"))
								return
							}
							match = c
						}
					}
					if match == 0 {
						release(store)
						done(nil)
						return
					}
					retain(match)
					completion := newBlock(func(objc.Block) { a.b.runOnMain(func() { release(match); release(store); done(nil) }) })
					send(store, "deleteCookie:completionHandler:", uintptr(match), uintptr(completion))
					completion.Release()
				})
			})
		})
		send(store, "getAllCookies:", uintptr(block))
		block.Release()
	})
}
