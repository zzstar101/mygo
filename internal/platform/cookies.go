package platform

import "time"

// Cookie is a normalized snapshot of a cookie in the default page store.
// SameSite is lax, strict, none, or empty (unknown on output only).
// A zero Expires denotes a session cookie.
type Cookie struct {
	Name, Value, Domain, Path string
	Expires                   time.Time
	Secure, HTTPOnly          bool
	SameSite                  string
}

// CookieKey identifies a cookie without request URL matching.
type CookieKey struct{ Name, Domain, Path string }
