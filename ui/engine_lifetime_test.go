package ui

import (
	"testing"

	"github.com/egoist/mygo/internal/surface"
)

// liveEngineCount is the number of engines that hear what reaches every
// window at once, as the texts UseTexts sets.
func liveEngineCount() int {
	enginesMu.Lock()
	defer enginesMu.Unlock()
	return len(liveEngines)
}

// TestWindowlessRenderingKeepsNoEngine checks that a view rendered without a
// window, as a test or a snapshot, leaves no engine behind: its engine, its
// host and what its view captured go when the view does, and only the
// windows shown hear what reaches every window at once.
func TestWindowlessRenderingKeepsNoEngine(t *testing.T) {
	before := liveEngineCount()
	view := func(c *context) {
		coreColumn(c).Padding(8).Children(func() {
			for range 200 {
				coreText(c, "a line of text the view holds")
			}
		})
	}
	if img := coreRender(view, 200, 400, 1); img == nil {
		t.Fatal("Render drew nothing")
	}
	tt := coreNewTester(view, 200, 400)
	tt.Frame()
	enginesMu.Lock()
	_, live := liveEngines[tt.rt]
	enginesMu.Unlock()
	if live {
		t.Error("the engine of a view rendered without a window hears what reaches every window")
	}
	if got := liveEngineCount(); got != before {
		t.Errorf("rendering without a window left %d engines behind", got-before)
	}
	tt.rt.close()
}

// TestAttachedContentWatches checks that the engine of a window shown hears
// what reaches every window at once, and stops hearing it when it closes.
func TestAttachedContentWatches(t *testing.T) {
	before := map[*engine]bool{}
	enginesMu.Lock()
	for rt := range liveEngines {
		before[rt] = true
	}
	enginesMu.Unlock()
	coreView(func(c *context) { coreText(c, "hello") }).AttachContent(&surface.Conn{Surface: &testSurface{}})
	var attached *engine
	enginesMu.Lock()
	for rt := range liveEngines {
		if !before[rt] {
			attached = rt
		}
	}
	enginesMu.Unlock()
	if attached == nil {
		t.Fatal("the engine of a window shown does not hear what reaches every window")
	}
	attached.close()
	if got := liveEngineCount(); got != len(before) {
		t.Errorf("closing the window left %d engines", got-len(before))
	}
}
