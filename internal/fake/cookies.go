package fake

import (
	"github.com/egoist/mygo/internal/platform"
	"slices"
	"time"
)

func (a app) ListCookies(done func([]platform.Cookie, error)) {
	a.b.mu.Lock()
	a.b.CookieCalls = append(a.b.CookieCalls, "list")
	now := time.Now()
	if a.b.CookieNow != nil {
		now = a.b.CookieNow()
	}
	result := []platform.Cookie{}
	for _, c := range a.b.cookies {
		if c.Expires.IsZero() || c.Expires.After(now) {
			result = append(result, c)
		}
	}
	if a.b.CookieSnapshot != nil {
		result = slices.Clone(a.b.CookieSnapshot)
	}
	err := a.b.CookieListError
	a.b.mu.Unlock()
	a.b.completeCookie(func() { done(result, err) })
}

func (a app) SetCookie(c platform.Cookie, done func(error)) {
	a.b.mu.Lock()
	a.b.CookieCalls = append(a.b.CookieCalls, "set")
	err := a.b.CookieSetError
	if err == nil {
		if a.b.cookies == nil {
			a.b.cookies = make(map[platform.CookieKey]platform.Cookie)
		}
		a.b.cookies[platform.CookieKey{Name: c.Name, Domain: c.Domain, Path: c.Path}] = c
	}
	a.b.mu.Unlock()
	a.b.completeCookie(func() { done(err) })
}

func (a app) DeleteCookie(key platform.CookieKey, done func(error)) {
	a.b.mu.Lock()
	a.b.CookieCalls = append(a.b.CookieCalls, "delete")
	err := a.b.CookieDeleteError
	if err == nil {
		delete(a.b.cookies, key)
	}
	a.b.mu.Unlock()
	a.b.completeCookie(func() { done(err) })
}

func (b *Backend) completeCookie(done func()) {
	if b.CookieDeferred {
		b.CookiePending = append(b.CookiePending, done)
		return
	}
	done()
}

// ReleaseCookies completes deferred requests. Call on main via the fake loop.
// Returned callbacks can be replayed to test late/duplicate completion defenses.
func (b *Backend) ReleaseCookies() []func() {
	pending := b.CookiePending
	b.CookiePending = nil
	for _, done := range pending {
		done()
	}
	return pending
}
