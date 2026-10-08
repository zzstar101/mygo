package mygo

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

// CookieSameSite controls when a cookie accompanies cross-site requests.
// Empty requests Lax on Set and means unknown or absent on reads.
type CookieSameSite string

const (
	CookieSameSiteLax    CookieSameSite = "lax"
	CookieSameSiteStrict CookieSameSite = "strict"
	CookieSameSiteNone   CookieSameSite = "none"
)

// Cookie is a snapshot of an HTTP cookie in the app's webview data store.
// It is not a net/http cookie jar entry and does not include partition keys.
type Cookie struct {
	Name  string
	Value string
	// Domain is a host or a leading-dot domain including its subdomains.
	// Use ASCII/punycode without a scheme, port, or trailing dot.
	Domain string
	// Path defaults to "/" when setting or identifying a cookie.
	Path string
	// Expires is zero for a session cookie, otherwise UTC whole seconds.
	// Persistence and lifetime caps remain controlled by the engine.
	Expires  time.Time
	Secure   bool
	HTTPOnly bool
	SameSite CookieSameSite
}

// CookiesModule manages the default cookie store shared by the app's pages.
// Methods are safe from any goroutine, including main-thread listeners, once
// the app is running; main-thread calls before App.Run panic. Windows needs an
// open web-page window (possibly initializing); native UI does not supply one.
// Cookies can contain credentials. Do not expose this module wholesale to pages.
type CookiesModule struct{}

// Cookies manages the application's default webview cookie store.
var Cookies CookiesModule

// List returns all native-visible cookies, including HTTPOnly, sorted by domain,
// path, then name. An empty store returns an empty slice. This snapshot is not
// a complete export of browser metadata or partitions.
func (CookiesModule) List() ([]Cookie, error) {
	needsApp("Cookies.List")
	result := cookieCall(func(done func(cookieResult)) {
		backend().App().ListCookies(func(values []platform.Cookie, err error) {
			out := make([]Cookie, 0, len(values))
			for _, c := range values {
				out = append(out, Cookie{c.Name, c.Value, strings.ToLower(c.Domain), c.Path, c.Expires, c.Secure, c.HTTPOnly, CookieSameSite(c.SameSite)})
			}
			slices.SortFunc(out, func(a, b Cookie) int {
				if n := strings.Compare(a.Domain, b.Domain); n != 0 {
					return n
				}
				if n := strings.Compare(a.Path, b.Path); n != 0 {
					return n
				}
				return strings.Compare(a.Name, b.Name)
			})
			done(cookieResult{out, err})
		})
	})
	return result.cookies, result.err
}

// Get returns an exact name/domain/path match. Domain is lowercased and empty
// path means "/"; a leading dot matters. Absence is not an error. Multiple
// indistinguishable native entries return an ambiguity error.
func (CookiesModule) Get(name, domain, path string) (Cookie, bool, error) {
	needsApp("Cookies.Get")
	key, err := normalizeCookieKey(name, domain, path)
	if err != nil {
		return Cookie{}, false, err
	}
	values, err := Cookies.List()
	if err != nil {
		return Cookie{}, false, err
	}
	var result Cookie
	found := false
	for _, c := range values {
		if c.Name == key.Name && c.Domain == key.Domain && c.Path == key.Path {
			if found {
				return Cookie{}, false, fmt.Errorf("mygo: cookies: ambiguous cookie key")
			}
			result = c
			found = true
		}
	}
	return result, found, nil
}

// Set adds or replaces an exact-key cookie. Empty path becomes "/", empty
// SameSite becomes Lax, and None requires Secure. A past nonzero Expires deletes
// the cookie. Nil error means completed or accepted, not a disk flush: browser
// policy may reject, shorten, or evict it.
func (CookiesModule) Set(c Cookie) error {
	needsApp("Cookies.Set")
	c, err := normalizeCookie(c)
	if err != nil {
		return err
	}
	if !c.Expires.IsZero() && !c.Expires.After(time.Now()) {
		return Cookies.Delete(c.Name, c.Domain, c.Path)
	}
	p := platform.Cookie{Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path, Expires: c.Expires, Secure: c.Secure, HTTPOnly: c.HTTPOnly, SameSite: string(c.SameSite)}
	return cookieCall(func(done func(cookieResult)) {
		backend().App().SetCookie(p, func(err error) { done(cookieResult{err: err}) })
	}).err
}

// Delete removes exactly this name/domain/path, if present. Domain is lowercased
// and empty path means "/". It is not URL deletion and never deletes siblings.
func (CookiesModule) Delete(name, domain, path string) error {
	needsApp("Cookies.Delete")
	key, err := normalizeCookieKey(name, domain, path)
	if err != nil {
		return err
	}
	return cookieCall(func(done func(cookieResult)) {
		backend().App().DeleteCookie(key, func(err error) { done(cookieResult{err: err}) })
	}).err
}

// Clear deletes a cookie snapshot, not localStorage, IndexedDB, caches, or other
// browsing data. It is not atomic: pages can create cookies concurrently. Every
// deletion is attempted and errors are joined (partial success is possible).
// App.ClearBrowsingData clears all kinds of browsing data instead.
func (CookiesModule) Clear() error {
	needsApp("Cookies.Clear")
	values, err := Cookies.List()
	if err != nil {
		return err
	}
	var errs []error
	for _, c := range values {
		if err := Cookies.Delete(c.Name, c.Domain, c.Path); err != nil {
			errs = append(errs, err)
			if errors.Is(err, errLoopStopped) {
				break
			}
		}
	}
	return errors.Join(errs...)
}

type cookieResult struct {
	cookies []Cookie
	err     error
}

// Accessed only on main. Native completions may arrive after shutdown.
var cookieCalls = struct {
	stopped bool
	next    uint64
	pending map[uint64]func(cookieResult)
}{pending: make(map[uint64]func(cookieResult))}

func cookieCall(start func(func(cookieResult))) cookieResult {
	ch := make(chan cookieResult, 1)
	if !postMain(func() {
		if cookieCalls.stopped {
			deliver(ch, cookieResult{err: errLoopStopped})
			return
		}
		cookieCalls.next++
		id := cookieCalls.next
		done := func(r cookieResult) {
			if _, ok := cookieCalls.pending[id]; !ok {
				return
			}
			delete(cookieCalls.pending, id)
			deliver(ch, r)
		}
		cookieCalls.pending[id] = done
		start(done)
	}) {
		return cookieResult{err: errLoopStopped}
	}
	return await(ch)
}

func stopCookieCalls() {
	cookieCalls.stopped = true
	for _, done := range cookieCalls.pending {
		done(cookieResult{err: errLoopStopped})
	}
}

func normalizeCookieKey(name, domain, path string) (platform.CookieKey, error) {
	bad := func(what string) (platform.CookieKey, error) {
		return platform.CookieKey{}, fmt.Errorf("mygo: cookies: invalid %s", what)
	}
	for _, c := range []byte(name) {
		if c < 32 || c == 127 {
			return bad("name")
		}
	}
	// Check for ASCII before lowercasing: strings.ToLower maps some non-ASCII
	// runes, such as the Kelvin sign, to ASCII letters.
	for _, c := range []byte(domain) {
		if c >= 0x80 {
			return bad("domain")
		}
	}
	domain = strings.ToLower(domain)
	host := strings.TrimPrefix(domain, ".")
	if host == "" || strings.HasSuffix(host, ".") {
		return bad("domain")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if host != domain || ip.Zone() != "" {
			return bad("IP domain")
		}
	} else {
		if len(host) > 253 {
			return bad("domain")
		}
		for _, label := range strings.Split(host, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return bad("domain")
			}
			for _, c := range []byte(label) {
				if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
					return bad("domain")
				}
			}
		}
	}
	if path == "" {
		path = "/"
	}
	if path[0] != '/' {
		return bad("path")
	}
	for _, c := range []byte(path) {
		if c < 32 || c > 126 || c == ';' {
			return bad("path")
		}
	}
	return platform.CookieKey{Name: name, Domain: domain, Path: path}, nil
}

func normalizeCookie(c Cookie) (Cookie, error) {
	bad := func(what string) (Cookie, error) { return Cookie{}, fmt.Errorf("mygo: cookies: invalid %s", what) }
	if c.Name == "" {
		return bad("name")
	}
	for _, b := range []byte(c.Name) {
		if b <= 32 || b >= 127 || strings.ContainsRune("()<>@,;:\\\"/[]?={}", rune(b)) {
			return bad("name")
		}
	}
	for _, b := range []byte(c.Value) {
		if !(b == 0x21 || b >= 0x23 && b <= 0x2b || b >= 0x2d && b <= 0x3a || b >= 0x3c && b <= 0x5b || b >= 0x5d && b <= 0x7e) {
			return bad("value")
		}
	}
	key, err := normalizeCookieKey(c.Name, c.Domain, c.Path)
	if err != nil {
		return Cookie{}, err
	}
	c.Domain = key.Domain
	c.Path = key.Path
	if c.SameSite == "" {
		c.SameSite = CookieSameSiteLax
	}
	switch c.SameSite {
	case CookieSameSiteLax, CookieSameSiteStrict:
	case CookieSameSiteNone:
		if !c.Secure {
			return bad("SameSite=None without Secure")
		}
	default:
		return bad("SameSite")
	}
	if !c.Expires.IsZero() {
		c.Expires = c.Expires.UTC().Truncate(time.Second)
		if c.Expires.Year() < 1601 || c.Expires.Year() > 9999 {
			return bad("expiration year")
		}
	}
	if strings.HasPrefix(c.Name, "__Secure-") || strings.HasPrefix(c.Name, "__Host-") {
		if !c.Secure {
			return bad("cookie prefix without Secure")
		}
	}
	if strings.HasPrefix(c.Name, "__Host-") && (strings.HasPrefix(c.Domain, ".") || c.Path != "/") {
		return bad("__Host- domain or path")
	}
	return c, nil
}
