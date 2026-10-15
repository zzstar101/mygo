package mygo

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"
)

// ReloadHandlers are what an app hands from one build of `mygo dev` to the
// next, for App.OnReload.
//
// Save runs on the main thread before the running build quits, in the dev
// output when it fails: put what should survive the reload into s. What
// Save sets is the whole state the app hands over, so a key a build no
// longer sets does not outlive the code that set it. The build that starts
// next reads it in Restore, which runs on the main thread before the app's
// WhenReady callbacks create its windows.
//
// Either may be nil, and what MyGo keeps itself (the window states of the
// reload, and what package ui keeps of its elements) is handed over
// whether or not they are set.
type ReloadHandlers struct {
	Save    func(s *ReloadState) error
	Restore func(s *ReloadState) error
}

// ReloadState is what a reload hands over: the app's values, by set key,
// next to what MyGo keeps by itself.
type ReloadState struct {
	values map[string]json.RawMessage
}

// Set stores value under key. The value must encode to JSON; a value that
// does not is only reported in the dev output, as the reload goes on.
func (s *ReloadState) Set(key string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if s.values == nil {
		s.values = map[string]json.RawMessage{}
	}
	s.values[key] = data
	return nil
}

// Get reads the value of key into value, which must be a pointer. It reports
// whether the key was set, and why it could not be read when it was: a key
// the build before this one never set is not an error, a value of a shape
// this build does not know is one.
func (s *ReloadState) Get(key string, value any) (bool, error) {
	data, ok := s.values[key]
	if !ok {
		return false, nil
	}
	if err := json.Unmarshal(data, value); err != nil {
		return false, fmt.Errorf("mygo: reload: %s: %w", key, err)
	}
	return true, nil
}

// Keys lists the keys set so far, sorted.
func (s *ReloadState) Keys() []string {
	keys := make([]string, 0, len(s.values))
	for key := range s.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// OnReload registers what an app keeps across a reload of `mygo dev`: the
// window is replaced and the process starts again, and Save hands what
// should survive to Restore, which runs before the first window of the
// new build.
//
//	mygo.App.OnReload(mygo.ReloadHandlers{
//		Save:    func(s *mygo.ReloadState) error { return s.Set("draft", app.draft) },
//		Restore: func(s *mygo.ReloadState) error { _, err := s.Get("draft", &app.draft); return err },
//	})
//
// Nothing else changes: MyGo keeps the window's bounds and the state of
// native UI elements itself, and the window is shown once it draws the
// restored state, so a reload does not flash an empty window.
//
// Outside `mygo dev` the handlers are never called: nothing is listening
// for a reload, so the app neither saves nor restores anything. A build of
// `mygo build` is marked production, which keeps it from even dialing a
// dev channel, and the app it ships never hands its state to anything.
func (a *Application) OnReload(h ReloadHandlers) {
	a.mu.Lock()
	a.reload = h
	a.mu.Unlock()
}

func (a *Application) reloadHandlers() ReloadHandlers {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reload
}

// reloadPayload is what the dev channel carries from one build to the
// next.
type reloadPayload struct {
	// Windows are the app's windows, in creation order.
	Windows []reloadWindowState `json:"windows,omitempty"`
	// Values are the app's, by key.
	Values map[string]json.RawMessage `json:"values,omitempty"`
}

// reloadWindowState is what a reload keeps of a window: where it was, and
// what its content kept.
type reloadWindowState struct {
	// StateKey names the window, so that the build that starts next puts
	// this state on the window it belongs to, wherever it is created.
	StateKey   string          `json:"stateKey,omitempty"`
	X          int             `json:"x"`
	Y          int             `json:"y"`
	Width      int             `json:"width"`
	Height     int             `json:"height"`
	Maximized  bool            `json:"maximized,omitempty"`
	FullScreen bool            `json:"fullScreen,omitempty"`
	Visible    bool            `json:"visible,omitempty"`
	Focused    bool            `json:"focused,omitempty"`
	UI         json.RawMessage `json:"ui,omitempty"`
}

// reloadMessage is one message of the dev channel, one JSON object a line.
type reloadMessage struct {
	// Type is "hello", "state" or "prepare-reload".
	Type string `json:"t"`
	// PID identifies the build that says hello, so that the CLI knows the
	// connection of the build it is running.
	PID int `json:"pid,omitempty"`
	// State is a reloadPayload.
	State json.RawMessage `json:"state,omitempty"`
}

// devReload is the app's side of the dev channel: the connection to
// mygo dev, which asks the running build for its state before it is
// replaced, and hands it to the build that starts next.
var devReload struct {
	conn    reloadConn
	sendMu  sync.Mutex // serializes writes to conn
	mu      sync.Mutex
	pulled  *reloadPayload // what the build before this one saved
	claimed []bool         // which of pulled.Windows a window took
	doing   bool           // the windows of a reload are being created
	saved   bool           // this build handed its state over already
}

// devSocketEnv tells an app of `mygo dev` where the dev channel listens.
// Like MYGO_DEV, it is not meant for the processes the app starts.
const devSocketEnv = "MYGO_DEV_SOCKET"

// startReloadRestore calls the app's Restore with what the build before
// this one left, and marks the windows the app creates when it becomes
// ready as the reload's. Main thread only, called before the app's
// WhenReady callbacks.
//
// A build that mygo dev did not launch has nothing to restore and is not
// waiting for anything: its Restore never runs. The first build of a
// mygo dev run has a channel, and runs Restore with nothing set, so an app
// can tell by Keys that it is starting fresh.
func startReloadRestore() {
	if !devReloadable || devReload.conn == nil && devReload.pulled == nil {
		return
	}
	p := devReload.pulled
	devReload.doing = p != nil
	if h := App.reloadHandlers(); h.Restore != nil {
		var values map[string]json.RawMessage
		if p != nil {
			values = p.Values
		}
		if err := h.Restore(&ReloadState{values: values}); err != nil {
			log.Printf("mygo: reload: Restore: %v", err)
		}
	}
}

// finishReloadRestore drops what this reload kept once the app's windows
// are created: windows opened later are ordinary ones. Main thread only.
func finishReloadRestore() {
	devReload.doing = false
	devReload.pulled = nil
	devReload.claimed = nil
}

// reloadWindowFor is the state the reload kept for a window created with
// stateKey, if any is left for it. A flyout is not a window of the reload:
// it is not kept, and does not take the state of one. Main thread only.
//
// A window takes the state of the window its StateKey names, so a build
// that creates its windows in another order still puts each state on the
// right window. A window without a StateKey takes the next state that has
// none either, which is what a build that never names its windows has to
// fall back on. A window the reload has no state for starts fresh: it is
// not worth putting a state of some other window on it.
func reloadWindowFor(stateKey string, isWindow bool) (reloadWindowState, bool) {
	if !devReloadable || !devReload.doing || !isWindow {
		return reloadWindowState{}, false
	}
	p := devReload.pulled
	if p == nil {
		return reloadWindowState{}, false
	}
	if devReload.claimed == nil {
		devReload.claimed = make([]bool, len(p.Windows))
	}
	for i := range p.Windows {
		if devReload.claimed[i] || p.Windows[i].StateKey != stateKey {
			continue
		}
		devReload.claimed[i] = true
		return p.Windows[i], true
	}
	return reloadWindowState{}, false
}

// reloadedWindow applies to w, created with opts, what the reload kept of
// the window it replaced: what its content had, and that it is shown once
// it drew the restored frames, so that a reload does not flash the window
// it is replacing. Main thread only.
func reloadedWindow(w *Window, opts *WindowOptions, st reloadWindowState) {
	if len(st.UI) > 0 && w.conn != nil && w.conn.ReloadRestore != nil {
		w.conn.ReloadRestore(st.UI)
	}
	if opts.Hidden || !st.Visible {
		// The app keeps the window hidden, or the build before this one
		// had hidden it: it stays hidden, and is not shown again.
		opts.Hidden = true
		return
	}
	if w.content == nil && opts.URL == "" {
		// Nothing in the window presents a first frame, so nothing would
		// show it again.
		return
	}
	opts.Hidden = true
	focused, shown := st.Focused, false
	show := func() {
		if shown {
			return
		}
		shown = true
		w.Show()
		if focused {
			w.Focus()
		}
	}
	w.OnReadyToShow(show)
	if w.content == nil {
		// A page that fails to load is never ready to show: the window
		// shows the failure, as it would have without the reload, rather
		// than staying hidden with nothing to tell why.
		w.onDidFailLoad.add(func(*LoadError) { show() }, false)
	}
}
