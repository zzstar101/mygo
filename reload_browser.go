//go:build js

package mygo

import "github.com/egoist/mygo/internal/platform"

// A browser build has no reload to hand state across: the page reloads by
// itself (see `mygo dev -web`). OnReload compiles, and its handlers are
// never called.

// reloadConn is absent in a browser build.
type reloadConn = interface{}

func startDevReload() {}

func saveDevReloadOnRelaunch() {}

func stopDevReload() {}

// restoreReloadBounds does nothing: a browser build has no window to put
// back where it was.
func restoreReloadBounds(p *platform.WindowOptions, st reloadWindowState) {}
