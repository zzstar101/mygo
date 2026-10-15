//go:build !js

package mygo

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

func TestReloadState(t *testing.T) {
	var s ReloadState
	if err := s.Set("a", map[string]int{"n": 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("b", []string{"x"}); err != nil {
		t.Fatal(err)
	}
	var m map[string]int
	if ok, err := s.Get("a", &m); !ok || err != nil || m["n"] != 1 {
		t.Errorf("Get(a) = %v, %v", m, err)
	}
	var missing string
	if ok, err := s.Get("c", &missing); ok || err != nil {
		t.Errorf("Get of a key that was never set = %v, %v", ok, err)
	}
	var wrong int
	if ok, err := s.Get("b", &wrong); ok || err == nil {
		t.Errorf("Get into a value of another type = %v, %v", ok, err)
	}
	if err := s.Set("bad", func() {}); err == nil {
		t.Error("Set of a value that is not JSON did not fail")
	}
	if got := s.Keys(); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("Keys = %v", got)
	}
}

// TestReloadHandsStateToTheNextBuild walks the handover: the running build
// saves on the main thread and sends its payload over the dev channel, and
// the build that starts next restores it and creates its windows with the
// state of the window they replace.
func TestReloadHandsStateToTheNextBuild(t *testing.T) {
	app, dev := net.Pipe()
	defer dev.Close()
	t.Cleanup(func() {
		onMain(func() {
			stopDevReload()
			devReload.saved, devReload.pulled, devReload.doing, devReload.claimed = false, nil, false, nil
		})
		App.OnReload(ReloadHandlers{})
	})
	onMain(func() { devReload.conn, devReload.saved = app, false })

	draft := "old"
	App.OnReload(ReloadHandlers{
		Save:    func(s *ReloadState) error { return s.Set("draft", draft) },
		Restore: func(s *ReloadState) error { _, _ = s.Get("draft", &draft); return nil },
	})
	w, fw := testWindow(t, WindowOptions{})
	onMain(func() {
		fw.SetBounds(platform.Rect{X: 12, Y: 34, Width: 500, Height: 400})
		windows.focused = w
	})

	// mygo dev asks the running build for its state before it is replaced.
	type result struct {
		msg reloadMessage
		err error
	}
	read := make(chan result, 1)
	go func() {
		line, err := bufio.NewReader(dev).ReadBytes('\n')
		if err != nil {
			read <- result{err: err}
			return
		}
		var msg reloadMessage
		read <- result{msg, json.Unmarshal(line, &msg)}
	}()
	draft = "typed"
	onMain(saveDevReload)
	got := <-read
	if got.err != nil {
		t.Fatalf("reading the app's message: %v", got.err)
	}
	if got.msg.Type != "state" {
		t.Fatalf("the app sent %q", got.msg.Type)
	}
	var payload reloadPayload
	if err := json.Unmarshal(got.msg.State, &payload); err != nil {
		t.Fatal(err)
	}
	var savedDraft string
	if err := json.Unmarshal(payload.Values["draft"], &savedDraft); err != nil || savedDraft != "typed" {
		t.Errorf("the app handed draft=%q (%v)", savedDraft, err)
	}
	if len(payload.Windows) != 1 {
		t.Fatalf("the app handed %d windows", len(payload.Windows))
	}
	if st := payload.Windows[0]; st.X != 12 || st.Y != 34 || st.Width != 500 || st.Height != 400 || !st.Visible || !st.Focused {
		t.Errorf("the app handed the window as %+v", st)
	}

	// The build that starts next: the app restores, and the window it
	// creates while restoring takes the place and the content of the one
	// it replaced.
	draft = ""
	onMain(func() {
		devReload.pulled = &payload
		startReloadRestore()
	})
	if draft != "typed" {
		t.Errorf("the next build restored draft=%q", draft)
	}
	w2, next := testWindow(t, WindowOptions{URL: "about:blank"})
	onMain(finishReloadRestore)
	if o := next.Opts; o.X != 12 || o.Y != 34 || o.Width != 500 || o.Height != 400 {
		t.Errorf("the next build's window got %+v", o)
	}
	if next.IsVisible() {
		t.Error("the restored window was shown before it drew")
	}
	onMain(w2.readyToShow)
	if !next.IsVisible() {
		t.Error("the restored window is not shown once it is ready")
	}
	if got := onMainValue(func() *Window { return windows.focused }); got != w2 {
		t.Error("the window of the reload did not take the focus back")
	}
}

// TestReloadSaveIsTheWholeState checks that what a build saves is its whole
// state: a key the code no longer sets is gone, so the state of a build
// that changed does not live on in the one that replaces it.
func TestReloadSaveIsTheWholeState(t *testing.T) {
	t.Cleanup(func() {
		onMain(func() { devReload.pulled = nil })
		App.OnReload(ReloadHandlers{})
	})
	onMain(func() {
		devReload.pulled = &reloadPayload{Values: map[string]json.RawMessage{"draft": json.RawMessage(`"kept"`)}}
	})
	App.OnReload(ReloadHandlers{Save: func(s *ReloadState) error { return s.Set("count", 2) }})
	var payload reloadPayload
	onMain(func() { payload = captureDevReload() })
	if _, ok := payload.Values["draft"]; ok {
		t.Error("a key this build does not set again outlived the build that set it")
	}
	var count int
	if err := json.Unmarshal(payload.Values["count"], &count); err != nil || count != 2 {
		t.Errorf("count = %v (%v)", count, err)
	}
}

// TestReloadWindowStateWaitsForTheApp checks that no window takes the
// state of the build before this one once the app's own windows are
// created: a window the app opens later is an ordinary one.
func TestReloadWindowStateWaitsForTheApp(t *testing.T) {
	payload := reloadPayload{Windows: []reloadWindowState{{X: 5, Y: 6, Width: 500, Height: 400, Visible: true}}}
	t.Cleanup(func() {
		onMain(func() { devReload.pulled, devReload.doing, devReload.claimed = nil, false, nil })
	})
	onMain(func() {
		devReload.pulled = &payload
		startReloadRestore()
	})
	// The app creates no window for the reload here, so the reload is over:
	// a window opened later is an ordinary one.
	onMain(finishReloadRestore)
	_, fw := testWindow(t, WindowOptions{})
	if o := fw.Opts; o.X == 5 || o.Width == 500 {
		t.Errorf("a window created after the reload took its state: %+v", o)
	}
	if !fw.IsVisible() {
		t.Error("a window created after the reload starts hidden")
	}
}

// TestReloadRestoreBoundsKeepsWindowsOnScreen checks where a window the
// reload restores goes when the bounds it had no longer show on a display.
func TestReloadRestoreBoundsKeepsWindowsOnScreen(t *testing.T) {
	displays := onMainValue(func() []platform.Display { return backend().Screen().Displays() })
	if len(displays) == 0 {
		t.Skip("the backend reports no display")
	}
	work := displays[0].WorkArea
	var off, on platform.WindowOptions
	onMain(func() {
		restoreReloadBounds(&off, reloadWindowState{X: work.X + work.Width + 1000, Y: work.Y + work.Height + 1000, Width: 500, Height: 400})
		restoreReloadBounds(&on, reloadWindowState{X: work.X + 10, Y: work.Y + 10, Width: 500, Height: 400})
	})
	if !off.Center {
		t.Errorf("bounds off every display did not center the window: %+v", off)
	}
	if off.Width != 500 || off.Height != 400 {
		t.Errorf("the size was not kept: %+v", off)
	}
	if on.Center || on.X != work.X+10 || on.Y != work.Y+10 {
		t.Errorf("bounds on a display were not applied: %+v", on)
	}
}

// TestDevReloadConnectsToTheDevChannel walks what the app does when mygo dev
// launches it: it connects to the channel, takes the state of the build it
// replaces, and hands its own over when it is asked for it.
func TestDevReloadConnectsToTheDevChannel(t *testing.T) {
	// A Unix socket path is short: /tmp, not the long temporary directory
	// the tests get, and on Windows the system's own, which is short and
	// exists.
	dir := "/tmp"
	if runtime.GOOS == "windows" {
		dir = os.TempDir()
	}
	path := filepath.Join(dir, "mygo-reload-test-"+strconv.Itoa(os.Getpid())+".sock")
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path) })
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	t.Cleanup(func() {
		onMain(func() {
			stopDevReload()
			devReload.saved, devReload.pulled, devReload.doing, devReload.claimed = false, nil, false, nil
		})
		App.OnReload(ReloadHandlers{})
	})

	state := json.RawMessage(`{"values":{"draft":"typed"}}`)
	saved := make(chan reloadMessage, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		if msg, err := readDevMessage(r); err != nil || msg.Type != "hello" {
			return
		}
		// The state of the build this one replaces.
		data, _ := json.Marshal(reloadMessage{Type: "state", State: state})
		_, _ = conn.Write(append(data, '\n'))
		// mygo dev rebuilds: it asks the running build for its state.
		data, _ = json.Marshal(reloadMessage{Type: "prepare-reload"})
		_, _ = conn.Write(append(data, '\n'))
		msg, err := readDevMessage(bufio.NewReader(conn))
		if err != nil {
			return
		}
		saved <- msg
	}()

	draft := ""
	App.OnReload(ReloadHandlers{
		Save:    func(s *ReloadState) error { return s.Set("count", 7) },
		Restore: func(s *ReloadState) error { _, _ = s.Get("draft", &draft); return nil },
	})
	t.Setenv(devSocketEnv, path)
	onMain(startDevReload)
	if devReload.conn == nil {
		t.Fatal("the app did not connect to the dev channel")
	}
	if devReload.pulled == nil {
		t.Fatal("the app did not take the state of the build before it")
	}
	onMain(startReloadRestore)
	if draft != "typed" {
		t.Errorf("the app restored draft=%q", draft)
	}

	select {
	case msg := <-saved:
		if msg.Type != "state" {
			t.Fatalf("the app answered with %q", msg.Type)
		}
		var payload reloadPayload
		if err := json.Unmarshal(msg.State, &payload); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := json.Unmarshal(payload.Values["count"], &count); err != nil || count != 7 {
			t.Errorf("the app handed over count=%v (%v)", count, err)
		}
		if len(payload.Windows) != 0 {
			t.Errorf("the app handed over %d windows, and it has none", len(payload.Windows))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mygo dev asked, and the app never answered")
	}
}

// TestReloadRestoreRunsOnTheFirstBuild checks that a build mygo dev
// launched restores even with nothing to restore, so an app can tell by
// Keys that it is starting fresh, and that an app mygo dev did not launch
// restores nothing at all.
func TestReloadRestoreRunsOnTheFirstBuild(t *testing.T) {
	app, dev := net.Pipe()
	defer dev.Close()
	t.Cleanup(func() {
		onMain(func() {
			stopDevReload()
			devReload.saved, devReload.pulled, devReload.doing, devReload.claimed = false, nil, false, nil
		})
		App.OnReload(ReloadHandlers{})
	})
	ran := false
	var keys []string
	App.OnReload(ReloadHandlers{Restore: func(s *ReloadState) error {
		ran, keys = true, s.Keys()
		return nil
	}})
	onMain(func() {
		devReload.conn = app
		startReloadRestore()
	})
	if !ran {
		t.Error("Restore did not run in a build of mygo dev")
	}
	if len(keys) != 0 {
		t.Errorf("the first build of a run restored %v", keys)
	}
	took := onMainValue(func() bool { _, ok := reloadWindowFor("", true); return ok })
	if took {
		t.Error("the first build took the state of the window it replaced")
	}

	// An app that no dev channel launched, as a production build, has
	// nothing to restore and nothing to keep.
	onMain(func() {
		stopDevReload()
		devReload.pulled = nil
	})
	ran = false
	onMain(startReloadRestore)
	if ran {
		t.Error("Restore ran in an app mygo dev did not launch")
	}
}

// TestReloadWindowStateFollowsTheStateKey gives each window the state of
// the window its state key names, so a build that creates its windows in
// another order still puts each state where it belongs.
func TestReloadWindowStateFollowsTheStateKey(t *testing.T) {
	payload := reloadPayload{Windows: []reloadWindowState{
		{StateKey: "reload-main", X: 1, Y: 2, Width: 500, Height: 400, Visible: true, Focused: true},
		{StateKey: "reload-settings", X: 7, Y: 8, Width: 300, Height: 200, Visible: true},
	}}
	t.Cleanup(func() {
		onMain(func() { devReload.pulled, devReload.doing, devReload.claimed = nil, false, nil })
	})
	onMain(func() {
		devReload.pulled = &payload
		startReloadRestore()
	})
	// The build creates the windows in the other order than the one that
	// saved the state.
	_, settings := testWindow(t, WindowOptions{StateKey: "reload-settings", URL: "about:blank"})
	mainW, main := testWindow(t, WindowOptions{StateKey: "reload-main", URL: "about:blank"})
	if o := settings.Opts; o.X != 7 || o.Y != 8 || o.Width != 300 || o.Height != 200 {
		t.Errorf("the settings window got %+v", o)
	}
	if o := main.Opts; o.X != 1 || o.Y != 2 || o.Width != 500 || o.Height != 400 {
		t.Errorf("the main window got %+v", o)
	}
	onMain(mainW.readyToShow)
	if got := onMainValue(func() *Window { return windows.focused }); got != mainW {
		t.Error("the focus did not go back to the window that had it")
	}
	onMain(finishReloadRestore)
}

// TestReloadWindowStateOfANewWindowStartsFresh checks that a window the
// reload has no state for is an ordinary window, and that the windows
// whose keys do match still get theirs.
func TestReloadWindowStateOfANewWindowStartsFresh(t *testing.T) {
	payload := reloadPayload{Windows: []reloadWindowState{
		{StateKey: "reload-main", X: 1, Y: 2, Width: 500, Height: 400, Visible: true},
		{StateKey: "reload-settings", X: 7, Y: 8, Width: 300, Height: 200, Visible: true},
	}}
	t.Cleanup(func() {
		onMain(func() { devReload.pulled, devReload.doing, devReload.claimed = nil, false, nil })
	})
	onMain(func() {
		devReload.pulled = &payload
		startReloadRestore()
	})
	_, fresh := testWindow(t, WindowOptions{StateKey: "reload-about", URL: "about:blank"})
	if o := fresh.Opts; o.X == 1 || o.X == 7 || o.Width == 500 || o.Width == 300 {
		t.Errorf("a window the reload has no state for took one: %+v", o)
	}
	if !fresh.IsVisible() {
		t.Error("a window the reload has no state for starts hidden")
	}
	_, main := testWindow(t, WindowOptions{StateKey: "reload-main", URL: "about:blank"})
	if o := main.Opts; o.X != 1 || o.Y != 2 || o.Width != 500 || o.Height != 400 {
		t.Errorf("the window whose key matched got %+v", o)
	}
	onMain(finishReloadRestore)
}

// TestReloadWindowStateOfAWindowThatIsGone checks that the state of a
// window a build no longer creates does not land on another one.
func TestReloadWindowStateOfAWindowThatIsGone(t *testing.T) {
	payload := reloadPayload{Windows: []reloadWindowState{
		{StateKey: "reload-settings", X: 7, Y: 8, Width: 300, Height: 200, Visible: true},
		{StateKey: "reload-main", X: 1, Y: 2, Width: 500, Height: 400, Visible: true},
	}}
	t.Cleanup(func() {
		onMain(func() { devReload.pulled, devReload.doing, devReload.claimed = nil, false, nil })
	})
	onMain(func() {
		devReload.pulled = &payload
		startReloadRestore()
	})
	_, main := testWindow(t, WindowOptions{StateKey: "reload-main", URL: "about:blank"})
	if o := main.Opts; o.X != 1 || o.Y != 2 || o.Width != 500 || o.Height != 400 {
		t.Errorf("the window that is still there got %+v", o)
	}
	onMain(finishReloadRestore)
}

// TestReloadWindowStateOfDuplicateKeys checks that a state key that names
// more than one window matches them in the order the windows were
// created.
func TestReloadWindowStateOfDuplicateKeys(t *testing.T) {
	payload := reloadPayload{Windows: []reloadWindowState{
		{StateKey: "reload-pane", X: 1, Y: 1, Width: 300, Height: 200, Visible: true},
		{StateKey: "reload-pane", X: 2, Y: 2, Width: 400, Height: 300, Visible: true},
	}}
	t.Cleanup(func() {
		onMain(func() { devReload.pulled, devReload.doing, devReload.claimed = nil, false, nil })
	})
	onMain(func() {
		devReload.pulled = &payload
		startReloadRestore()
	})
	_, first := testWindow(t, WindowOptions{StateKey: "reload-pane", URL: "about:blank"})
	_, second := testWindow(t, WindowOptions{StateKey: "reload-pane", URL: "about:blank"})
	if o := first.Opts; o.X != 1 || o.Width != 300 {
		t.Errorf("the first window got %+v", o)
	}
	if o := second.Opts; o.X != 2 || o.Width != 400 {
		t.Errorf("the second window got %+v", o)
	}
	onMain(finishReloadRestore)
}

// TestReloadWindowStatesWithoutKeysFollowCreationOrder checks the fallback
// of an app that never names its windows: the states are taken in creation
// order, as they were saved.
func TestReloadWindowStatesWithoutKeysFollowCreationOrder(t *testing.T) {
	payload := reloadPayload{Windows: []reloadWindowState{
		{X: 1, Y: 2, Width: 500, Height: 400, Visible: true},
		{X: 7, Y: 8, Width: 300, Height: 200, Visible: true},
	}}
	t.Cleanup(func() {
		onMain(func() { devReload.pulled, devReload.doing, devReload.claimed = nil, false, nil })
	})
	onMain(func() {
		devReload.pulled = &payload
		startReloadRestore()
	})
	_, first := testWindow(t, WindowOptions{URL: "about:blank"})
	_, second := testWindow(t, WindowOptions{URL: "about:blank"})
	if o := first.Opts; o.X != 1 || o.Width != 500 {
		t.Errorf("the first window got %+v", o)
	}
	if o := second.Opts; o.X != 7 || o.Width != 300 {
		t.Errorf("the second window got %+v", o)
	}
	onMain(finishReloadRestore)
}

// TestReloadHiddenWindowStaysHidden checks that a window the build before
// this one had hidden is not shown by the reload, not even once it drew.
func TestReloadHiddenWindowStaysHidden(t *testing.T) {
	payload := reloadPayload{Windows: []reloadWindowState{
		{StateKey: "reload-hidden", Width: 400, Height: 300, Visible: false},
	}}
	t.Cleanup(func() {
		onMain(func() { devReload.pulled, devReload.doing, devReload.claimed = nil, false, nil })
	})
	onMain(func() {
		devReload.pulled = &payload
		startReloadRestore()
	})
	w, hidden := testWindow(t, WindowOptions{StateKey: "reload-hidden", URL: "about:blank"})
	if hidden.IsVisible() {
		t.Error("a window that was hidden was shown by the reload")
	}
	onMain(w.readyToShow)
	if hidden.IsVisible() {
		t.Error("a window that was hidden was shown once it was ready")
	}
	onMain(finishReloadRestore)
}

// TestReloadRelaunchSavesBeforeTheWindowsClose checks that App.Relaunch
// hands the state over while the windows are still there: the quit
// sequence closes them, and what they were cannot be read after that.
func TestReloadRelaunchSavesBeforeTheWindowsClose(t *testing.T) {
	app, dev := net.Pipe()
	defer dev.Close()
	t.Cleanup(func() {
		onMain(func() {
			stopDevReload()
			devReload.saved, devReload.pulled, devReload.doing, devReload.claimed = false, nil, false, nil
			App.quitting, App.relaunch = false, false
			cookieCalls.stopped = false
		})
		App.OnReload(ReloadHandlers{})
	})
	onMain(func() { devReload.conn, devReload.saved = app, false })
	App.OnReload(ReloadHandlers{Save: func(s *ReloadState) error { return s.Set("draft", "half typed") }})
	_, fw := testWindow(t, WindowOptions{StateKey: "reload-relaunch"})
	onMain(func() {
		fw.SetBounds(platform.Rect{X: 12, Y: 34, Width: 500, Height: 400})
		App.relaunch = true
	})

	read := make(chan reloadMessage, 1)
	go func() {
		defer close(read)
		line, err := bufio.NewReader(dev).ReadBytes('\n')
		if err != nil {
			return
		}
		var msg reloadMessage
		if json.Unmarshal(line, &msg) == nil {
			read <- msg
		}
	}()

	// The quit sequence of a relaunch, which closes the app's windows.
	if !onMainValue(func() bool { return App.prepareQuit() }) {
		t.Fatal("the quit sequence did not go through")
	}
	msg, ok := <-read
	if !ok {
		t.Fatal("the app relaunched without handing its state over")
	}
	if msg.Type != "state" {
		t.Fatalf("the app sent %q", msg.Type)
	}
	var payload reloadPayload
	if err := json.Unmarshal(msg.State, &payload); err != nil {
		t.Fatal(err)
	}
	var draft string
	if err := json.Unmarshal(payload.Values["draft"], &draft); err != nil || draft != "half typed" {
		t.Errorf("the app handed draft=%q (%v)", draft, err)
	}
	if len(payload.Windows) != 1 {
		t.Fatalf("the app handed %d windows: the save ran after they were closed", len(payload.Windows))
	}
	if st := payload.Windows[0]; st.X != 12 || st.Width != 500 {
		t.Errorf("the app handed the window as %+v", st)
	}
}

// TestReloadRelaunchCancelledSavesAgain checks that a relaunch a window
// cancels leaves the app answering mygo dev: the state it handed over as the
// quit began goes nowhere, and the next reload asks for it again.
func TestReloadRelaunchCancelledSavesAgain(t *testing.T) {
	app, dev := net.Pipe()
	defer dev.Close()
	t.Cleanup(func() {
		onMain(func() {
			stopDevReload()
			devReload.saved, devReload.pulled, devReload.doing, devReload.claimed = false, nil, false, nil
			App.quitting, App.relaunch = false, false
		})
		App.OnReload(ReloadHandlers{})
	})
	onMain(func() { devReload.conn, devReload.saved = app, false })
	count := 0
	App.OnReload(ReloadHandlers{Save: func(s *ReloadState) error { count++; return s.Set("count", count) }})
	w, _ := testWindow(t, WindowOptions{StateKey: "reload-relaunch-cancelled"})
	off := w.OnClose(func(e *CloseEvent) { e.PreventDefault() })
	defer off()

	read := make(chan reloadMessage, 2)
	go func() {
		defer close(read)
		r := bufio.NewReader(dev)
		for {
			line, err := r.ReadBytes('\n')
			if err != nil {
				return
			}
			var msg reloadMessage
			if json.Unmarshal(line, &msg) == nil {
				read <- msg
			}
		}
	}()
	next := func(what string) reloadMessage {
		t.Helper()
		select {
		case msg := <-read:
			return msg
		case <-time.After(5 * time.Second):
			t.Fatalf("the app did not hand its state over %s", what)
			return reloadMessage{}
		}
	}

	// The window cancels the relaunch, after the app handed its state over.
	App.Relaunch()
	next("as the relaunch began")
	if onMainValue(func() bool { return App.relaunch || App.quitting }) {
		t.Fatal("the relaunch went on")
	}
	// mygo dev rebuilds: the app that went on answers.
	onMain(saveDevReload)
	msg := next("when mygo dev asked after a cancelled relaunch")
	var payload reloadPayload
	if err := json.Unmarshal(msg.State, &payload); err != nil {
		t.Fatal(err)
	}
	if string(payload.Values["count"]) != "2" {
		t.Errorf("the app handed count=%s, want the state of a second Save", payload.Values["count"])
	}
}

// TestReloadRestoreFailureStartsFresh checks that a reload whose Restore
// fails still starts the app: the handover is best-effort, and a build
// that cannot take the state of the one before it starts with its own.
func TestReloadRestoreFailureStartsFresh(t *testing.T) {
	app, dev := net.Pipe()
	defer dev.Close()
	t.Cleanup(func() {
		onMain(func() {
			stopDevReload()
			devReload.saved, devReload.pulled, devReload.doing, devReload.claimed = false, nil, false, nil
		})
		App.OnReload(ReloadHandlers{})
	})
	onMain(func() { devReload.conn = app })
	App.OnReload(ReloadHandlers{Restore: func(s *ReloadState) error { return errors.New("cannot restore") }})
	onMain(startReloadRestore)
	_, fw := testWindow(t, WindowOptions{URL: "about:blank"})
	if !fw.IsVisible() {
		t.Error("the app did not start after its Restore failed")
	}
}

// TestDevReloadDoesNotConnectInProduction checks that a build of mygo
// build never dials the dev channel, whatever it is told to connect to.
func TestDevReloadDoesNotConnectInProduction(t *testing.T) {
	path := reloadTestSocketPath(t, "production")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	t.Cleanup(func() {
		onMain(func() {
			stopDevReload()
			devReload.pulled = nil
		})
	})
	old := production
	production = "1"
	t.Cleanup(func() { production = old })
	t.Setenv(devSocketEnv, path)
	onMain(startDevReload)
	if onMainValue(func() bool { return devReload.conn != nil }) {
		t.Error("a production build dialed the dev channel")
	}
}

// TestDevReloadWithoutAChannel checks that a build told of an empty
// MYGO_DEV_SOCKET, as mygo dev --no-reload-state starts it, dials nothing.
func TestDevReloadWithoutAChannel(t *testing.T) {
	t.Cleanup(func() {
		onMain(func() {
			stopDevReload()
			devReload.pulled = nil
		})
	})
	t.Setenv(devSocketEnv, "")
	onMain(startDevReload)
	if onMainValue(func() bool { return devReload.conn != nil }) {
		t.Error("a build without a dev channel dialed one")
	}
}

// reloadTestSocketPath is a socket path short enough for the tests: /tmp,
// not the long temporary directory they get, and on Windows the system's
// own directory, which is short and exists.
func reloadTestSocketPath(t *testing.T, name string) string {
	t.Helper()
	dir := "/tmp"
	if runtime.GOOS == "windows" {
		dir = os.TempDir()
	}
	path := filepath.Join(dir, "mygo-reload-test-"+name+"-"+strconv.Itoa(os.Getpid())+".sock")
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path) })
	return path
}
