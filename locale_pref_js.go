//go:build js

package mygo

// A browser keeps no language of its own: the page owns the language, and
// there is no per-user directory to keep a file in. Builds for the browser
// leave out locale_pref.go and its file access.

// RememberLocale does nothing in a build for the browser.
func (a *Application) RememberLocale(on bool) {}

func (a *Application) saveLocalePreference(tag string) {}

func (a *Application) restoreLocalePreference() {}
