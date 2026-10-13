//go:build !js

package mygo

import (
	"encoding/json/v2"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
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

// TestLocaleAfterNavigation checks that the pages a window loads after the
// locale changed read the new one at document start, and that the page is
// told the locale again as it loads, which its bridge tells no listener of
// when it had it already.
func TestLocaleAfterNavigation(t *testing.T) {
	resetLocale(t)
	_, fw := readyWindow(t, WindowOptions{Page: PageOptions{PreloadScript: "window.preloaded = 1"}})
	App.SetLocale("zh-Hans")
	received(t, fw, func(m map[string]any) bool { return localeEvent(m, "zh-Hans") })

	scripts := fw.UserScripts()
	if len(scripts) != 3 || !strings.Contains(scripts[0].Source, `"locale":"zh-Hans"`) {
		t.Fatalf("the bridge configuration of a later page has not the locale: %v", scripts)
	}
	if scripts[2].Source != "window.preloaded = 1" {
		t.Errorf("the preload script went: %v", scripts)
	}
	// The secret of the window's bridge stays.
	if !strings.Contains(scripts[0].Source, `"secret":"`+secret(fw)+`"`) {
		t.Errorf("the bridge configuration changed its secret")
	}
	seen := len(fw.Scripts())
	page(fw, `{"t":"dom-ready"}`)
	receivedAfter(t, fw, seen, func(m map[string]any) bool { return localeEvent(m, "zh-Hans") })
}

// TestLocaleChangesOnly checks that OnLocaleChanged, and the pages, hear of
// the locale the app shows when it changed only: not when the system tells
// of a change that left the language as it was, nor when SetLocale sets
// the language the system has.
func TestLocaleChangesOnly(t *testing.T) {
	resetLocale(t)
	_, fw := readyWindow(t, WindowOptions{})
	onMain(func() {}) // everything the tests before queued has run
	var heard []string
	off := App.OnLocaleChanged(func(tag string) { heard = append(heard, tag) })
	defer off()
	seen := len(fw.Scripts())

	fb.SetSystemLocale("en-US") // a change of region settings, say
	App.SetLocale("en-US")      // what the system has
	App.SetLocale("")
	onMain(func() {})
	if len(heard) != 0 {
		t.Errorf("OnLocaleChanged heard %q, want nothing", heard)
	}
	if got := localeEventsAfter(t, fw, seen); len(got) != 0 {
		t.Errorf("the page heard %q, want nothing", got)
	}

	fb.SetSystemLocale("de-DE")
	onMain(func() {})
	if len(heard) != 1 || heard[0] != "de-DE" {
		t.Errorf("OnLocaleChanged heard %q, want [de-DE]", heard)
	}
}

// TestSystemLocale checks that SystemLocale is the system's language
// whatever SetLocale set.
func TestSystemLocale(t *testing.T) {
	resetLocale(t)
	fb.SetSystemLocale("fr-FR")
	App.SetLocale("ja")
	if got := App.SystemLocale(); got != "fr-FR" {
		t.Errorf("SystemLocale() = %q, want fr-FR", got)
	}
	if got := App.Locale(); got != "ja" {
		t.Errorf("Locale() = %q, want ja", got)
	}
}

// TestSetLocaleCoalesces checks that the SetLocale calls made before the
// main thread gets to them become one: the app, and the pages, hear the last
// tag only, instead of a repaint for every step and a tag that is already
// older than the one the app shows.
func TestSetLocaleCoalesces(t *testing.T) {
	resetLocale(t)
	_, fw := readyWindow(t, WindowOptions{})
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

// TestMenuRolesFollowLocale checks that the items of a menu say the labels
// of their roles in the language of the locale the app shows, and keep a
// label the app gave them.
func TestMenuRolesFollowLocale(t *testing.T) {
	resetLocale(t)
	before := App.Menu()
	t.Cleanup(func() {
		menuTexts = i18n.New("en", builtinMenuTexts)
		App.SetMenu(before)
	})
	App.SetMenu(NewMenu([]*MenuItem{
		{Role: RoleEditMenu},
		{Label: "Tools", Submenu: []*MenuItem{{Label: "Grab", Role: RoleCopy}, {Role: RoleCopy}}},
	}))
	labels := func() (edit, undo, grab, copyText string) {
		m := fb.AppMenu()
		return m.Items[0].Label, m.Items[0].Submenu.Items[0].Label, m.Items[1].Submenu.Items[0].Label, m.Items[1].Submenu.Items[1].Label
	}
	if edit, undo, grab, copyText := labels(); edit != "Edit" || undo != "Undo" || grab != "Grab" || copyText != "Copy" {
		t.Errorf("the menu says %q, %q, %q and %q", edit, undo, grab, copyText)
	}
	App.SetLocale("zh-Hans")
	onMain(func() {})
	if edit, undo, grab, copyText := labels(); edit != "编辑" || undo != "撤销" || grab != "Grab" || copyText != "复制" {
		t.Errorf("in zh-Hans the menu says %q, %q, %q and %q", edit, undo, grab, copyText)
	}
	// A language the app adds, with English for what it leaves out.
	UseMenuTexts(map[string]MenuTexts{"de": {Edit: "Bearbeiten", Undo: "Widerrufen"}})
	App.SetLocale("de-DE")
	onMain(func() {})
	if edit, undo, _, copyText := labels(); edit != "Bearbeiten" || undo != "Widerrufen" || copyText != "Copy" {
		t.Errorf("in de-DE the menu says %q, %q and %q", edit, undo, copyText)
	}
	if got := roleTexts().QuitApp("Notes"); got != "Quit Notes" {
		t.Errorf("de-DE quits with %q", got)
	}
}
