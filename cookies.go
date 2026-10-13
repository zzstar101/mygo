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

// CookieSameSite says whether a cookie goes along with requests from
// other sites. Cookies.Set takes an empty one as Lax; a cookie List
// returns has an empty one when the engine does not say.
type CookieSameSite string

const (
	// CookieSameSiteLax sends the cookie along when the user navigates
	// from another site, but not with other requests from it.
	CookieSameSiteLax CookieSameSite = "lax"
	// CookieSameSiteStrict sends the cookie only with the site's own
	// requests.
	CookieSameSiteStrict CookieSameSite = "strict"
	// CookieSameSiteNone sends the cookie with every request; the cookie
	// must be Secure.
	CookieSameSiteNone CookieSameSite = "none"
)

// Cookie is a cookie of the app's pages. Its name, domain and path
// identify it.
type Cookie struct {
	Name  string
	Value string
	// Domain is the host the cookie is sent to, such as "example.com", or
	// with a leading dot, ".example.com", the domain whose subdomains get
	// it too. It is in lowercase ASCII (punycode), without a port.
	Domain string
	// Path is the path the cookie is sent for; empty means "/".
	Path string
	// Expires is when the cookie expires, in whole seconds; zero for a
	// cookie that lasts until the app quits.
	Expires  time.Time
	Secure   bool
	HTTPOnly bool
	SameSite CookieSameSite
}

// CookiesModule reads and changes the cookies of the app's pages. Use the
// Cookies singleton.
type CookiesModule struct{}

// Cookies reads and changes the cookies the app's pages share, HTTPOnly
// ones included, e.g. to sign the user in with a token the backend got:
//
//	err := mygo.Cookies.Set(mygo.Cookie{Name: "session", Value: token,
//		Domain: "example.com", Secure: true, HTTPOnly: true})
//
// The methods are safe from any goroutine once the app is running; on the
// main thread the app keeps handling events while they wait. On Windows
// they need a window that shows a web page. Cookies may carry the user's
// credentials: bind a backend method that does what the page needs rather
// than giving the page the cookies.
var Cookies CookiesModule

// List returns the cookies, sorted by domain, path and name.
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

// Get returns the cookie of name, domain and path, and whether there is
// one. The domain is matched without regard to case and with its leading
// dot, if any: "example.com" and ".example.com" are different cookies. An
// empty path is "/". It fails when the engine keeps several such
// cookies, e.g. partitioned ones of the same name.
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

// Set adds the cookie, or replaces the one of the same name, domain and
// path. An empty path is "/" and an empty SameSite Lax; SameSite None
// needs Secure, as do the __Secure- and __Host- name prefixes. The value
// must be made of the characters a Set-Cookie header allows: encode
// others, e.g. with base64.RawURLEncoding. An Expires in the past deletes
// the cookie. The engine may still drop the cookie or expire it earlier,
// as it would one a server set.
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

// Delete deletes the cookie of name, domain and path, matched as Get
// matches them. Deleting a cookie that does not exist is not an error.
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

// Clear deletes every cookie, and nothing else the pages stored, unlike
// App.ClearBrowsingData. Pages may set new ones meanwhile.
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

// cookieCalls are the calls waiting for the backend, which may never
// complete them once the app quits: stopCookieCalls fails them. It is
// used on the main thread only.
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
