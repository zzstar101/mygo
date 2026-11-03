//go:build js || mygo_noinspector

package mygo

import (
	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/surface"
)

// A browser build has no reload to hand state across: the page reloads by
// itself (see `mygo dev -web`). A production build (the tag
// mygo_noinspector, which leaves the developer tools out) is never run by
// mygo dev, and leaves the handover out so that it takes no room in the
// binary. OnReload compiles, and its handlers are never called.

// devReloadable tells whether this build can hand its state over at all.
const devReloadable = false

// reloadWindowState is empty: no window takes the state of another here.
type reloadWindowState struct{}

func setReloadHandlers(ReloadHandlers) {}

func startReloadRestore() {}

func finishReloadRestore() {}

func reloadWindowFor(string, bool) (reloadWindowState, bool) { return reloadWindowState{}, false }

func reloadedWindow(*Window, *WindowOptions, reloadWindowState) {}

func attachReload(*surface.Conn) {}

func resumeReload() {}

func reloadedURL(_ *Window, resolved string) string { return resolved }

func startDevReload() {}

func saveDevReloadOnRelaunch() {}

func stopDevReload() {}

// restoreReloadBounds does nothing: a browser build has no window to put
// back where it was.
func restoreReloadBounds(p *platform.WindowOptions, st reloadWindowState) {}
