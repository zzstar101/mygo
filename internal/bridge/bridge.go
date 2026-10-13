// Package bridge embeds the renderer runtime that MyGo injects into pages.
//
// bridge.js is built from ../../packages/bridge by `bun run build` and
// committed, so building a MyGo application never requires Bun.
package bridge

import (
	_ "embed"
	"encoding/json"

	"github.com/egoist/mygo/internal/platform"
)

//go:embed bridge.js
var source string

// Config is passed to the bridge as __MYGO_CONFIG__.
type Config struct {
	Platform string `json:"platform"`
	WindowID int    `json:"windowId"`
	Version  string `json:"version"`
	// Secret prefixes every message the bridge posts.
	Secret string `json:"secret"`
}

// Script returns the bridge source configured for one window.
func Script(cfg Config) string {
	b, _ := json.Marshal(cfg)
	return "(function(__MYGO_CONFIG__){\n" + source + "})(" + string(b) + ");"
}

// TitleBarEvent tells a page of a window with a hidden title bar the room
// the window controls take. The bridge keeps it in the --mygo-titlebar-*
// CSS variables.
const TitleBarEvent = "mygo:title-bar"

// TitleBar is the payload of TitleBarEvent, in CSS pixels.
type TitleBar struct {
	Height float64 `json:"height"`
	Left   float64 `json:"left"`
	Right  float64 `json:"right"`
}

// NewTitleBar converts the room of the window controls to the CSS pixels
// of a page shown at zoom.
func NewTitleBar(tb platform.TitleBar, zoom float64) TitleBar {
	if zoom <= 0 {
		zoom = 1
	}
	return TitleBar{Height: float64(tb.Height) / zoom, Left: float64(tb.Left) / zoom, Right: float64(tb.Right) / zoom}
}

// TitleBarScript delivers TitleBarEvent at document start, after the
// bridge, so that a page lays out around the window controls before it
// first paints.
func TitleBarScript(tb platform.TitleBar, zoom float64) string {
	p, _ := json.Marshal(NewTitleBar(tb, zoom))
	return `window.__mygo&&window.__mygo.receive({"t":"event","n":"` + TitleBarEvent + `","p":` + string(p) + `});`
}

// PreferencesEvent tells a page the accent color and the accessibility
// settings of the desktop. The bridge keeps the accent in the --mygo-accent
// CSS variable and the text scale in --mygo-text-scale.
const PreferencesEvent = "mygo:preferences"

// Preferences is a page's view of the desktop's settings.
type Preferences struct {
	// Accent is the color of the desktop's controls as "#rrggbb", or empty
	// where the desktop has none.
	Accent string `json:"accent"`
	// ReduceMotion reports that the desktop asks for less animation.
	ReduceMotion bool `json:"reduceMotion"`
	// HighContrast reports that the desktop uses a contrast theme.
	HighContrast bool `json:"highContrast"`
	// TextScale is the factor the desktop asks text to grow by; 1 is usual.
	TextScale float64 `json:"textScale"`
}

// PreferencesScript delivers PreferencesEvent at document start, after the
// bridge has registered the handler that turns it into CSS variables.
func PreferencesScript(p Preferences) string {
	b, _ := json.Marshal(p)
	return `window.__mygo&&window.__mygo.receive({"t":"event","n":"` + PreferencesEvent + `","p":` + string(b) + `});`
}
