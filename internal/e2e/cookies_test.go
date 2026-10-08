package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

func TestCookies(t *testing.T) {
	prefix := fmt.Sprintf("mygo%d", time.Now().UnixNano())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/seed" {
			http.SetCookie(w, &http.Cookie{Name: prefix + "server", Value: "private", Path: "/", HttpOnly: true})
		}
		if r.URL.Path == "/echo" {
			fmt.Fprint(w, r.Header.Get("Cookie"))
			return
		}
		fmt.Fprint(w, "<!doctype html><title>cookies</title><body>cookies</body>")
	}))
	defer server.Close()
	w := newWindow(t, mygo.WindowOptions{Title: "Cookies", Width: 300, Height: 200})
	if err := w.Page().LoadURL(server.URL + "/seed"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, w, "document.title === 'cookies'")
	t.Cleanup(func() {
		for _, suffix := range []string{"server", "plain", "hidden", "deep", "persistent"} {
			_ = mygo.Cookies.Delete(prefix+suffix, "127.0.0.1", "/")
			_ = mygo.Cookies.Delete(prefix+suffix, "127.0.0.1", "/deep/path")
		}
	})
	c, found, err := mygo.Cookies.Get(prefix+"server", "127.0.0.1", "/")
	if err != nil || !found || !c.HTTPOnly {
		t.Fatalf("server cookie: %+v %v %v", c, found, err)
	}
	for _, suffix := range []string{"plain", "hidden", "deep", "persistent"} {
		c := mygo.Cookie{Name: prefix + suffix, Value: "value", Domain: "127.0.0.1", HTTPOnly: suffix == "hidden"}
		if suffix == "deep" {
			c.Path = "/deep/path"
		}
		if suffix == "persistent" {
			c.Expires = time.Now().Add(time.Hour).UTC().Truncate(time.Second)
		}
		if err := mygo.Cookies.Set(c); err != nil {
			t.Fatal(err)
		}
		got, found, err := mygo.Cookies.Get(c.Name, c.Domain, c.Path)
		if err != nil || !found || got.Expires.IsZero() != c.Expires.IsZero() {
			t.Fatalf("roundtrip %s: %+v %v %v", suffix, got, found, err)
		}
	}
	visible, err := mygo.EvalAs[string](w.Page(), "document.cookie")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(visible, prefix+"plain=") || strings.Contains(visible, prefix+"hidden=") || strings.Contains(visible, prefix+"server=") || strings.Contains(visible, prefix+"deep=") {
		t.Fatalf("document.cookie visibility wrong: %q", visible)
	}
	header, err := mygo.EvalAs[string](w.Page(), "fetch('/echo').then(r=>r.text())")
	if err != nil || !strings.Contains(header, prefix+"hidden=") {
		t.Fatalf("HTTPOnly request missing: %q %v", header, err)
	}
	sibling := newWindow(t, mygo.WindowOptions{Title: "Cookie sibling", Width: 300, Height: 200})
	if err := sibling.Page().LoadURL(server.URL); err != nil {
		t.Fatal(err)
	}
	waitFor(t, sibling, "document.title === 'cookies'")
	visible, err = mygo.EvalAs[string](sibling.Page(), "document.cookie")
	if err != nil || !strings.Contains(visible, prefix+"plain=") {
		t.Fatalf("shared cookie: %q %v", visible, err)
	}
	w.Destroy()
	if err := mygo.Cookies.Delete(prefix+"plain", "127.0.0.1", ""); err != nil {
		t.Fatal(err)
	}
	if _, found, err := mygo.Cookies.Get(prefix+"deep", "127.0.0.1", "/deep/path"); err != nil || !found {
		t.Fatalf("deep sibling lost: %v %v", found, err)
	}
	if _, err := sibling.Page().Eval("localStorage.setItem('cookies-test','keep')"); err != nil {
		t.Fatal(err)
	}
	if err := mygo.Cookies.Clear(); err != nil {
		t.Fatal(err)
	}
	storage, err := mygo.EvalAs[string](sibling.Page(), "localStorage.getItem('cookies-test')")
	if err != nil || storage != "keep" {
		t.Fatalf("cookie Clear changed localStorage: %q %v", storage, err)
	}
	if err := mygo.App.ClearBrowsingData(); err != nil {
		t.Fatal(err)
	}
	sibling.Page().Reload()
	waitFor(t, sibling, "document.title === 'cookies' && localStorage.getItem('cookies-test') === null")
}

func TestCookieCallbackStress(t *testing.T) {
	w := newWindow(t, mygo.WindowOptions{Title: "Cookie callback stress", Width: 200, Height: 100})
	w.Page().LoadHTML("<title>cookie stress</title>", "")
	waitFor(t, w, "document.title === 'cookie stress'")
	for i := 0; i < 2100; i++ {
		if _, err := mygo.Cookies.List(); err != nil {
			t.Fatalf("List %d: %v", i, err)
		}
	}
}
