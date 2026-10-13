//go:build !js

package mygo

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// rememberLocale keeps the remembered language of a test in a directory of
// its own, and restores the app's settings.
func rememberLocale(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	App.SetPath(PathUserData, dir)
	t.Cleanup(func() {
		onMain(func() { App.RememberLocale(false) })
		App.SetPath(PathUserData, "")
	})
	return filepath.Join(dir, localePreferenceFile)
}

// readLocaleFile returns the remembered tag, or fails the test.
func readLocaleFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", localePreferenceFile, err)
	}
	return strings.TrimSpace(string(data))
}

// writeLocaleFile writes a remembered tag, as a run before this one left it.
func writeLocaleFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRememberLocale(t *testing.T) {
	resetLocale(t)
	path := rememberLocale(t)
	onMain(func() { App.RememberLocale(true) })

	// SetLocale keeps the tag for the next run.
	App.SetLocale("zh-Hans")
	onMain(func() {}) // the save is queued on the main thread, and runs first
	if got := readLocaleFile(t, path); got != "zh-Hans" {
		t.Errorf("the remembered locale is %q, want %q", got, "zh-Hans")
	}

	// An empty tag follows the system again, and forgets the tag.
	App.SetLocale("")
	onMain(func() {})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("%s kept after SetLocale(\"\"): %v", localePreferenceFile, err)
	}
}

func TestLocaleIsNotKeptWithoutRemembering(t *testing.T) {
	resetLocale(t)
	path := rememberLocale(t)

	// Without RememberLocale, the app shows a language and keeps none.
	App.SetLocale("fr-FR")
	onMain(func() {})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("%s written although the app remembers no language: %v", localePreferenceFile, err)
	}

	onMain(func() { App.RememberLocale(true) })
	App.SetLocale("de-DE")
	onMain(func() {})
	if got := readLocaleFile(t, path); got != "de-DE" {
		t.Errorf("the remembered locale is %q, want %q", got, "de-DE")
	}

	// Turning it off stops the saving, and keeps what was saved.
	onMain(func() { App.RememberLocale(false) })
	App.SetLocale("it-IT")
	onMain(func() {})
	if got := readLocaleFile(t, path); got != "de-DE" {
		t.Errorf("the remembered locale is %q after RememberLocale(false), want %q", got, "de-DE")
	}
}

func TestRememberLocaleKeepsTheCurrentTag(t *testing.T) {
	resetLocale(t)
	path := rememberLocale(t)

	// The tag set before the app remembers it is saved right away.
	App.SetLocale("zh-Hant")
	onMain(func() { App.RememberLocale(true) })
	onMain(func() {}) // the save is queued on the main thread, and runs first
	if got := readLocaleFile(t, path); got != "zh-Hant" {
		t.Errorf("the remembered locale is %q, want %q", got, "zh-Hant")
	}
}

func TestRememberLocaleKeepsTheStoredTag(t *testing.T) {
	resetLocale(t)
	path := rememberLocale(t)

	// RememberLocale(true) before Run reads the tag, and must not forget
	// the one a run before it kept.
	writeLocaleFile(t, path, "fr-FR\n")
	onMain(func() { App.RememberLocale(true) })
	if got := readLocaleFile(t, path); got != "fr-FR" {
		t.Errorf("the remembered locale is %q, want %q", got, "fr-FR")
	}
}

func TestRestoreLocalePreference(t *testing.T) {
	resetLocale(t)
	path := rememberLocale(t)
	onMain(func() { App.RememberLocale(true) })
	writeLocaleFile(t, path, "zh-Hant\n")

	// The next run starts with the remembered language.
	onMain(func() { App.restoreLocalePreference() })
	if got := App.Locale(); got != "zh-Hant" {
		t.Errorf("App.Locale() = %q, want %q", got, "zh-Hant")
	}
	// A window created later tells its pages.
	_, fw := testWindow(t, WindowOptions{})
	if got := fw.Opts.UserScripts[0].Source; !strings.Contains(got, `"locale":"zh-Hant"`) {
		t.Errorf("the bridge configuration has no remembered locale: %s", got)
	}
	// The language of its own wins over the system's.
	fb.SetSystemLocale("de-DE")
	if got := App.Locale(); got != "zh-Hant" {
		t.Errorf("App.Locale() = %q after the system changed, want %q", got, "zh-Hant")
	}

	// An app that remembers no language ignores the file.
	App.SetLocale("")
	onMain(func() { App.RememberLocale(false) })
	fb.SetSystemLocale("en-US")
	writeLocaleFile(t, path, "fr-FR\n")
	onMain(func() { App.restoreLocalePreference() })
	if got := App.Locale(); got != "en-US" {
		t.Errorf("App.Locale() = %q without remembering, want %q", got, "en-US")
	}
}

func TestRestoreLocalePreferenceIgnoresDamage(t *testing.T) {
	resetLocale(t)
	path := rememberLocale(t)
	onMain(func() { App.RememberLocale(true) })

	for _, bad := range []string{"", "\n", "  \n", "zh Hans\n", strings.Repeat("x", 40)} {
		writeLocaleFile(t, path, bad)
		onMain(func() { App.restoreLocalePreference() })
		if got := App.Locale(); got != "en-US" {
			t.Errorf("App.Locale() = %q for %q, want %q", got, bad, "en-US")
		}
	}
}

// TestSetLocaleFromEveryGoroutine checks that the language kept for the next
// run is the one the app ends up showing: SetLocale is safe to call from any
// goroutine, and the tag another goroutine set in the meantime is the one
// that counts.
func TestSetLocaleFromEveryGoroutine(t *testing.T) {
	resetLocale(t)
	path := rememberLocale(t)
	onMain(func() { App.RememberLocale(true) })

	tags := []string{"fr", "de", "zh-Hans", "en-GB", "pt-BR", "ja", "es-ES", "it", "nl", "ru", "ko", "pl"}
	var wg sync.WaitGroup
	for _, tag := range tags {
		wg.Add(1)
		go func() {
			defer wg.Done()
			App.SetLocale(tag)
		}()
	}
	wg.Wait()
	// Everything SetLocale posted has run by now.
	onMain(func() {})

	locale := App.Locale()
	if locale == "" {
		t.Fatalf("the app shows no language of its own")
	}
	if got := readLocaleFile(t, path); got != locale {
		t.Errorf("the app shows %q and kept %q", locale, got)
	}
}
