//go:build !js

package mygo

import (
	"testing"
	"time"
)

func TestCookiesRoundTrip(t *testing.T) {
	if err := Cookies.Clear(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = Cookies.Clear() })
	empty, err := Cookies.List()
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty List = %v, %v", empty, err)
	}
	expiry := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	for _, path := range []string{"", "/account"} {
		if err := Cookies.Set(Cookie{Name: "session", Value: "secret", Domain: "EXAMPLE.COM", Path: path, HTTPOnly: true, Expires: expiry}); err != nil {
			t.Fatal(err)
		}
	}
	got, found, err := Cookies.Get("session", "EXAMPLE.COM", "")
	if err != nil || !found || got.Domain != "example.com" || got.Path != "/" || got.SameSite != CookieSameSiteLax || !got.Expires.Equal(expiry) {
		t.Fatalf("Get = %+v, %v, %v", got, found, err)
	}
	got.Value = "changed"
	if err := Cookies.Set(got); err != nil {
		t.Fatal(err)
	}
	if err := Cookies.Delete("session", "example.com", ""); err != nil {
		t.Fatal(err)
	}
	if _, found, err := Cookies.Get("session", "example.com", ""); err != nil || found {
		t.Fatalf("deleted = %v, %v", found, err)
	}
	if _, found, err := Cookies.Get("session", "example.com", "/account"); err != nil || !found {
		t.Fatalf("sibling = %v, %v", found, err)
	}
	if err := Cookies.Delete("absent", "example.com", ""); err != nil {
		t.Fatal(err)
	}
}

func TestCookiesValidation(t *testing.T) {
	base := Cookie{Name: "key", Value: "secret", Domain: "example.com"}
	tests := []struct {
		name   string
		change func(*Cookie)
	}{
		{"empty name", func(c *Cookie) { c.Name = "" }}, {"name separator", func(c *Cookie) { c.Name = "a=b" }},
		{"value injection", func(c *Cookie) { c.Value = "secret\r\nX: y" }}, {"value space", func(c *Cookie) { c.Value = "a b" }},
		{"domain port", func(c *Cookie) { c.Domain = "example.com:80" }}, {"domain trailing dot", func(c *Cookie) { c.Domain = "example.com." }},
		{"domain double dot", func(c *Cookie) { c.Domain = "..example.com" }}, {"domain IP dot", func(c *Cookie) { c.Domain = ".127.0.0.1" }},
		{"domain Kelvin sign", func(c *Cookie) { c.Domain = "\u212a.example" }},
		{"path", func(c *Cookie) { c.Path = "relative" }}, {"path injection", func(c *Cookie) { c.Path = "/;Secure" }},
		{"enum", func(c *Cookie) { c.SameSite = "other" }}, {"insecure none", func(c *Cookie) { c.SameSite = CookieSameSiteNone }},
		{"secure prefix", func(c *Cookie) { c.Name = "__Secure-a" }}, {"host prefix", func(c *Cookie) { c.Name = "__Host-a"; c.Secure = true; c.Domain = ".example.com" }},
		{"expiration", func(c *Cookie) { c.Expires = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base
			tt.change(&c)
			if err := Cookies.Set(c); err == nil {
				t.Fatal("accepted invalid cookie")
			}
		})
	}
}

func TestCookiesMainThread(t *testing.T) {
	RunOnMain(func() {
		c := Cookie{Name: "main", Domain: "example.com"}
		if err := Cookies.Set(c); err != nil {
			t.Error(err)
		}
		if _, ok, err := Cookies.Get(c.Name, c.Domain, ""); err != nil || !ok {
			t.Errorf("Get: %v %v", ok, err)
		}
		if _, err := Cookies.List(); err != nil {
			t.Error(err)
		}
		if err := Cookies.Delete(c.Name, c.Domain, ""); err != nil {
			t.Error(err)
		}
		if err := Cookies.Clear(); err != nil {
			t.Error(err)
		}
	})
}

func TestCookiesPastExpiry(t *testing.T) {
	c := Cookie{Name: "past", Domain: "example.com"}
	if err := Cookies.Set(c); err != nil {
		t.Fatal(err)
	}
	c.Expires = time.Date(1960, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := Cookies.Set(c); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := Cookies.Get(c.Name, c.Domain, ""); err != nil || ok {
		t.Fatalf("Get: %v %v", ok, err)
	}
}
