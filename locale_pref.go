//go:build !js

package mygo

import (
	"os"
	"path/filepath"
	"strings"
)

// An app can remember the language a user picked, so that the next run
// starts with it instead of the one the system prefers: RememberLocale
// turns it on, SetLocale keeps the tag in locale in PathUserData, and Run
// reads it back before anything shows.

// localePreferenceFile is where RememberLocale keeps the tag.
const localePreferenceFile = "locale"

// RememberLocale tells MyGo to keep the language the app shows across runs.
// SetLocale then saves its tag in locale in PathUserData, SetLocale("")
// removes it and follows the user's preferred locale again, and Run starts
// with the saved tag when it is there. It is off by default, and safe from
// any goroutine; call it in main before Run, which reads the tag, or later
// to start saving.
//
// The tag SetLocale set before RememberLocale(true) is saved right away:
//
//	mygo.App.SetLocale("zh-Hans")
//	mygo.App.RememberLocale(true)
func (a *Application) RememberLocale(on bool) {
	a.mu.Lock()
	a.rememberLocale = on
	tag := a.locale
	a.mu.Unlock()
	// A tag of its own, set before the app remembers them, is worth keeping
	// now. An app following the system has nothing to keep, and leaves what
	// Run is about to read alone.
	if on && tag != "" {
		postMain(func() { a.saveLocalePreference(tag) })
	}
}

// saveLocalePreference keeps tag for the next run, or forgets it when the
// tag is empty. Main thread only, and it does nothing while the app does
// not remember its language.
func (a *Application) saveLocalePreference(tag string) {
	a.mu.Lock()
	remember := a.rememberLocale
	a.mu.Unlock()
	if !remember {
		return
	}
	path, err := a.localePreferencePath()
	if err != nil {
		return
	}
	if tag == "" {
		// Following the system again: a tag of its own is in the way.
		os.Remove(path)
		return
	}
	// A save that fails keeps the language the app shows, and the next
	// SetLocale writes the tag.
	if err := os.WriteFile(path+".tmp", []byte(tag+"\n"), 0o644); err != nil {
		return
	}
	os.Rename(path+".tmp", path)
}

// restoreLocalePreference makes the tag the app remembered the app's own,
// before anything showed. Main thread only, called by Run.
func (a *Application) restoreLocalePreference() {
	a.mu.Lock()
	remember, own := a.rememberLocale, a.locale != ""
	a.mu.Unlock()
	if !remember || own {
		return
	}
	tag := loadLocalePreference()
	if tag == "" {
		return
	}
	a.mu.Lock()
	if a.locale == "" {
		a.locale = tag
	}
	a.mu.Unlock()
}

// localePreferencePath is the file the remembered tag lives in.
func (a *Application) localePreferencePath() (string, error) {
	dir, err := a.Path(PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, localePreferenceFile), nil
}

// loadLocalePreference reads the remembered tag, or "" when the app
// remembers none, the file is damaged, or it holds no language tag.
func loadLocalePreference() string {
	path, err := App.localePreferencePath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	tag := strings.TrimSpace(string(data))
	if tag == "" || len(tag) > 35 || strings.ContainsAny(tag, " \t\r\n") {
		return "" // not a language tag: ignore it, and let SetLocale write a new one
	}
	return tag
}
