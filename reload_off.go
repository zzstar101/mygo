//go:build js || mygo_noinspector

package mygo

import "github.com/egoist/mygo/internal/platform"

// A browser build has no reload to hand state across: the page reloads by
// itself (see `mygo dev -web`). A production build (the tag
// mygo_noinspector, which leaves the developer tools out) is never run by
// mygo dev, and leaves the handover out so that it takes no room in the
// binary. OnReload compiles, and its handlers are never called.

// devReloadable tells whether this build can hand its state over at all.
const devReloadable = false

// reloadConn is absent in a browser build.
type reloadConn = interface{}

func startDevReload() {}

func saveDevReloadOnRelaunch() {}

func stopDevReload() {}

// restoreReloadBounds does nothing: a browser build has no window to put
// back where it was.
func restoreReloadBounds(p *platform.WindowOptions, st reloadWindowState) {}
