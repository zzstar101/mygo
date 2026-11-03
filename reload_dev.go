//go:build !js && !mygo_noinspector

package mygo

import (
	"bufio"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/egoist/mygo/internal/platform"
	"github.com/egoist/mygo/internal/surface"
)

// reloadConn is the app's connection to mygo dev.
type reloadConn = net.Conn

// devReloadable tells whether this build can hand its state over at all:
// a production build (reload_off.go) leaves it out.
const devReloadable = true

// startDevReload connects to mygo dev, unless this build was not launched
// with state handover: no MYGO_DEV_SOCKET (a production build, another
// launcher, or mygo dev without -hot), or the browser, which has no reload to
// keep state across (its page reloads by itself).
func startDevReload() {
	path := os.Getenv(devSocketEnv)
	os.Unsetenv(devSocketEnv)
	if path == "" || production == "1" || os.Getenv("MYGO_GENERATE") != "" {
		return
	}
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		// A dev channel that is not there only means no state to hand
		// over, as a build started by hand.
		return
	}
	devReload.conn = conn
	if err := sendDevMessage(reloadMessage{Type: "hello", PID: os.Getpid()}); err != nil {
		return
	}
	// The state of the build before this one, if any, is read before the
	// event loop runs: the app's Restore and the windows it creates get it.
	// The reader is the one the connection is read with from then on: what
	// it holds beyond the first message is not read again, so a second
	// reader would wait for messages already in the first one's buffer.
	r := bufio.NewReader(conn)
	_ = conn.SetReadDeadline(time.Now().Add(reloadStartTimeout))
	msg, err := readDevMessage(r)
	if err == nil && msg.Type == "state" {
		var p reloadPayload
		if json.Unmarshal(msg.State, &p) == nil {
			devReload.pulled = &p
		}
	}
	_ = conn.SetReadDeadline(time.Time{})
	go readDevReload(r)
}

// reloadStartTimeout is how long a build waits for the state of the build
// before it as it starts. The state is already in the CLI's memory when
// the build is started, so it arrives at once, and a build that waits is
// no worse off than one that does not.
var reloadStartTimeout = time.Second

// readDevReload reads the messages of mygo dev until the connection ends.
func readDevReload(r *bufio.Reader) {
	for {
		msg, err := readDevMessage(r)
		if err != nil {
			return
		}
		switch msg.Type {
		case "prepare-reload":
			// The running build is about to be replaced: save what the
			// next one should have, on the main thread, where the app's
			// state lives.
			postMain(saveDevReload)
		}
	}
}

func readDevMessage(r *bufio.Reader) (reloadMessage, error) {
	var msg reloadMessage
	line, err := r.ReadBytes('\n')
	if err != nil {
		return msg, err
	}
	err = json.Unmarshal(line, &msg)
	return msg, err
}

func sendDevMessage(msg reloadMessage) error {
	devReload.sendMu.Lock()
	defer devReload.sendMu.Unlock()
	if devReload.conn == nil {
		return errors.New("mygo: no dev channel")
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_ = devReload.conn.SetWriteDeadline(time.Now().Add(reloadStartTimeout))
	_, err = devReload.conn.Write(data)
	return err
}

// saveDevReload gathers what this build hands to the next one and sends
// it. Main thread only.
func saveDevReload() {
	if devReload.conn == nil || devReload.saved {
		return
	}
	payload := captureDevReload()
	devReload.saved = true
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("mygo: reload: %v", err)
		return
	}
	if err := sendDevMessage(reloadMessage{Type: "state", State: data}); err != nil {
		log.Printf("mygo: reload: %v", err)
	}
}

// captureDevReload is the payload of this build: its windows and what the
// app sets in Save. Main thread only.
func captureDevReload() reloadPayload {
	payload := reloadPayload{Windows: captureReloadWindows()}
	// What Save sets is the whole state of this build: a key it no longer
	// sets is gone, so what the build before this one kept of code that
	// changed does not live on.
	if h := reloadHandlers(); h.Save != nil {
		s := &ReloadState{}
		if err := h.Save(s); err != nil {
			log.Printf("mygo: reload: Save: %v", err)
		}
		payload.Values = s.values
	}
	return payload
}

// captureReloadWindows is the state of the app's windows, in creation
// order. Main thread only.
func captureReloadWindows() []reloadWindowState {
	list := windows.list
	var out []reloadWindowState
	for _, w := range list {
		// A flyout has no place of its own to keep: it is placed at the
		// window, or the tray icon, that opened it.
		if w.flyout != nil || w.native == nil {
			continue
		}
		st := reloadWindowState{StateKey: w.stateKey}
		if b := w.native.Bounds(); b.Width > 0 && b.Height > 0 {
			st.X, st.Y, st.Width, st.Height = b.X, b.Y, b.Width, b.Height
		}
		st.Maximized, st.FullScreen = w.native.IsMaximized(), w.native.IsFullScreen()
		st.Visible = w.native.IsVisible()
		st.Focused = windows.focused == w
		if w.content == nil {
			if u := w.native.URL(); u != "" && u != "about:blank" {
				st.URL = u
			}
		}
		if w.conn != nil && w.conn.ReloadSave != nil {
			if data := w.conn.ReloadSave(); len(data) > 0 {
				st.UI = data
			}
		}
		out = append(out, st)
	}
	return out
}

// saveDevReloadOnRelaunch hands the state over when the app starts itself
// again (App.Relaunch), which mygo dev turns into a new build: the app
// quits with the dev exit code, and the build that starts next gets what
// this one saved. It runs as the quit sequence begins, before the windows
// are closed, since they are part of what is handed over. Main thread
// only.
func saveDevReloadOnRelaunch() {
	if devReload.conn == nil || devReload.saved {
		return
	}
	saveDevReload()
}

// stopDevReload closes the dev channel, when the app quits without a
// replacement.
func stopDevReload() {
	if devReload.conn != nil {
		_ = devReload.conn.Close()
		devReload.conn = nil
	}
}

// restoreReloadBounds applies the bounds a reload kept for a window, if
// they show on a connected display, as a saved window state does.
func restoreReloadBounds(p *platform.WindowOptions, st reloadWindowState) {
	if st.Width <= 0 || st.Height <= 0 {
		return
	}
	r := Rectangle{st.X, st.Y, st.Width, st.Height}
	displays := backend().Screen().Displays()
	p.UseContentSize = false
	p.Maximized, p.FullScreen = st.Maximized, st.FullScreen
	if onDisplay(r, displays) {
		p.X, p.Y, p.Width, p.Height = r.X, r.Y, r.Width, r.Height
		p.Center = false
		return
	}
	p.Width, p.Height = r.Width, r.Height
	for _, d := range displays {
		if d.Primary {
			p.Width, p.Height = min(r.Width, d.WorkArea.Width), min(r.Height, d.WorkArea.Height)
		}
	}
	p.Center = true
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
	URL        string          `json:"url,omitempty"` // the page of a web view
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

	handlers ReloadHandlers // App.OnReload
	// urls are the pages of the windows of the reload, until they load one.
	urls map[*Window]string
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
	if h := reloadHandlers(); h.Restore != nil {
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
	// A window that loaded no page by now loads what it decides, later.
	devReload.urls = nil
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
	if st.URL != "" {
		if devReload.urls == nil {
			devReload.urls = map[*Window]string{}
		}
		devReload.urls[w] = st.URL
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

func setReloadHandlers(h ReloadHandlers) {
	devReload.mu.Lock()
	devReload.handlers = h
	devReload.mu.Unlock()
}

func reloadHandlers() ReloadHandlers {
	devReload.mu.Lock()
	defer devReload.mu.Unlock()
	return devReload.handlers
}

// attachReload tells a window's content whether a reload will ask for what it
// holds.
func attachReload(conn *surface.Conn) { conn.Reload = devReload.conn != nil }

// resumeReload lets the app hand its state over again, after a relaunch that a
// window canceled: what it handed as the quit began goes nowhere.
func resumeReload() { devReload.saved = false }

// reloadedURL returns the page to load for a window asked to load rawURL: the
// page it showed before the reload, if it is of the same site, so that a restart
// does not lose the place of a single page app. Asked once: what the app
// loads after that is what the app wants.
func reloadedURL(w *Window, resolved string) string {
	saved, ok := devReload.urls[w]
	if !ok {
		return resolved
	}
	delete(devReload.urls, w)
	a, err := url.Parse(saved)
	if err != nil {
		return resolved
	}
	b, err := url.Parse(resolved)
	if err != nil || a.Scheme != b.Scheme || a.Host != b.Host {
		return resolved
	}
	return saved
}
