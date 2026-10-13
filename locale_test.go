//go:build !js

package mygo

import (
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/fake"
)

// localeEvent reports whether m is the mygo:locale event carrying tag.
func localeEvent(m map[string]any, tag string) bool {
	return m["t"] == "event" && m["n"] == "mygo:locale" && m["p"] == tag
}

// resetLocale restores the locale the app shows and the locale of the
// system, which the tests below change.
func resetLocale(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		App.SetLocale("")
		fb.SetSystemLocale("en-US")
	})
	App.SetLocale("")
	fb.SetSystemLocale("en-US")
}

// waitLocale returns the locale OnLocaleChanged reports next.
func waitLocale(t *testing.T, changes <-chan string) string {
	t.Helper()
	select {
	case tag := <-changes:
		return tag
	case <-time.After(3 * time.Second):
		t.Fatal("OnLocaleChanged was not called")
		return ""
	}
}

func TestBridgeConfigurationLocale(t *testing.T) {
	resetLocale(t)
	_, fw := testWindow(t, WindowOptions{})
	if got := fw.Opts.UserScripts[0].Source; !strings.Contains(got, `"locale":"en-US"`) {
		t.Errorf("the bridge configuration has no locale: %s", got)
	}
}

func TestSetLocaleReachesPages(t *testing.T) {
	resetLocale(t)
	_, fw := readyWindow(t, WindowOptions{})
	changes := make(chan string, 4)
	off := App.OnLocaleChanged(func(tag string) { changes <- tag })
	defer off()

	App.SetLocale("zh-Hans")
	if got := App.Locale(); got != "zh-Hans" {
		t.Errorf("App.Locale() = %q, want %q", got, "zh-Hans")
	}
	// The page hears the new tag, and so does the app.
	received(t, fw, func(m map[string]any) bool { return localeEvent(m, "zh-Hans") })
	if got := waitLocale(t, changes); got != "zh-Hans" {
		t.Errorf("OnLocaleChanged = %q, want %q", got, "zh-Hans")
	}

	// An empty tag follows the system's locale again.
	App.SetLocale("")
	if got := App.Locale(); got != "en-US" {
		t.Errorf("App.Locale() = %q, want %q", got, "en-US")
	}
	received(t, fw, func(m map[string]any) bool { return localeEvent(m, "en-US") })
	if got := waitLocale(t, changes); got != "en-US" {
		t.Errorf("OnLocaleChanged = %q, want %q", got, "en-US")
	}
}

func TestSystemLocaleReachesPages(t *testing.T) {
	resetLocale(t)
	_, fw := readyWindow(t, WindowOptions{})
	changes := make(chan string, 4)
	off := App.OnLocaleChanged(func(tag string) { changes <- tag })
	defer off()

	// The app follows the system's locale while it shows none of its own.
	fb.SetSystemLocale("fr-FR")
	if got := App.Locale(); got != "fr-FR" {
		t.Errorf("App.Locale() = %q, want %q", got, "fr-FR")
	}
	received(t, fw, func(m map[string]any) bool { return localeEvent(m, "fr-FR") })
	if got := waitLocale(t, changes); got != "fr-FR" {
		t.Errorf("OnLocaleChanged = %q, want %q", got, "fr-FR")
	}

	// A locale of its own wins over the system's.
	App.SetLocale("zh-Hant")
	received(t, fw, func(m map[string]any) bool { return localeEvent(m, "zh-Hant") })
	waitLocale(t, changes)
	fb.SetSystemLocale("de-DE")
	if got := App.Locale(); got != "zh-Hant" {
		t.Errorf("App.Locale() = %q after the system changed, want %q", got, "zh-Hant")
	}
	if scripts := fw.Scripts(); hasLocaleEvent(scripts, "de-DE") {
		t.Errorf("the page heard de-DE although the app shows zh-Hant: %q", scripts)
	}
}

// hasLocaleEvent reports whether the page was told the mygo:locale event
// with tag.
func hasLocaleEvent(scripts []string, tag string) bool {
	for _, s := range scripts {
		const prefix = "__mygo.receive("
		if !strings.HasPrefix(s, prefix) {
			continue
		}
		if strings.Contains(s, `"n":"mygo:locale"`) && strings.Contains(s, `"p":"`+tag+`"`) {
			return true
		}
	}
	return false
}

func TestLocaleAfterNavigation(t *testing.T) {
	resetLocale(t)
	_, fw := readyWindow(t, WindowOptions{})
	App.SetLocale("zh-Hans")
	received(t, fw, func(m map[string]any) bool { return localeEvent(m, "zh-Hans") })

	// The script at document start carries the locale of the window's
	// creation; a page loaded later hears the current one.
	if got := fw.Opts.UserScripts[0].Source; strings.Contains(got, `"locale":"zh-Hans"`) {
		t.Fatal("the bridge configuration of a later page should keep the locale of the window's creation")
	}
	seen := len(fw.Scripts())
	page(fw, `{"t":"dom-ready"}`)
	receivedAfter(t, fw, seen, func(m map[string]any) bool { return localeEvent(m, "zh-Hans") })
}

// TestSetLocaleCoalesces checks that the SetLocale calls made before the
// main thread gets to them become one: the app, and the pages, hear the last
// tag only, instead of a repaint for every step and a tag that is already
// older than the one the app shows.
func TestSetLocaleCoalesces(t *testing.T) {
	resetLocale(t)
	path := rememberLocale(t)
	_, fw := readyWindow(t, WindowOptions{})
	onMain(func() { App.RememberLocale(true) })
	onMain(func() {}) // everything the tests before queued has run

	var heard []string
	off := App.OnLocaleChanged(func(tag string) { heard = append(heard, tag) })
	defer off()

	// Hold the main thread, so that both calls queue their work before any
	// of it runs.
	held, release := make(chan struct{}), make(chan struct{})
	go onMain(func() {
		close(held)
		<-release
	})
	<-held
	seen := len(fw.Scripts())
	App.SetLocale("zh-Hans")
	App.SetLocale("ja")
	close(release)
	onMain(func() {}) // everything they queued has run by now

	if got := App.Locale(); got != "ja" {
		t.Errorf("App.Locale() = %q, want %q", got, "ja")
	}
	if got := readLocaleFile(t, path); got != "ja" {
		t.Errorf("the app kept %q, want %q", got, "ja")
	}
	if len(heard) != 1 || heard[0] != "ja" {
		t.Errorf("OnLocaleChanged heard %q, want [ja]", heard)
	}
	if got := localeEventsAfter(t, fw, seen); len(got) != 1 || got[0] != "ja" {
		t.Errorf("the page heard %q, want [ja]", got)
	}
}

// localeEventsAfter returns the tags the mygo:locale event carried in a
// script after the first n.
func localeEventsAfter(t *testing.T, fw *fake.Window, n int) []string {
	t.Helper()
	var tags []string
	scripts := fw.Scripts()
	for _, s := range scripts[min(n, len(scripts)):] {
		const prefix = "__mygo.receive("
		if !strings.HasPrefix(s, prefix) {
			continue
		}
		var msgs []map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(s, prefix), ")")), &msgs); err != nil {
			t.Fatalf("bad script %q: %v", s, err)
		}
		for _, m := range msgs {
			if m["t"] == "event" && m["n"] == "mygo:locale" {
				if tag, ok := m["p"].(string); ok {
					tags = append(tags, tag)
				}
			}
		}
	}
	return tags
}

// receivedAfter is received for messages delivered in a script after the
// first n, which the tests use to tell a new delivery from an earlier one.
func receivedAfter(t *testing.T, fw *fake.Window, n int, pred func(m map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		scripts := fw.Scripts()
		if n > len(scripts) {
			n = len(scripts)
		}
		for _, s := range scripts[n:] {
			const prefix = "__mygo.receive("
			if !strings.HasPrefix(s, prefix) {
				continue
			}
			var msgs []map[string]any
			if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(s, prefix), ")")), &msgs); err != nil {
				t.Fatalf("bad script %q: %v", s, err)
			}
			for _, m := range msgs {
				if pred(m) {
					return m
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("message not received; scripts: %q", fw.Scripts())
	return nil
}
