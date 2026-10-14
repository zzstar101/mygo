//go:build !js

package mygo

import (
	"bufio"
	"encoding/json"
	"errors"
	"log"
	"net"
	"os"
	"time"

	"github.com/egoist/mygo/internal/platform"
)

// reloadConn is the app's connection to mygo dev.
type reloadConn = net.Conn

// startDevReload connects to mygo dev, unless this build was not launched
// with state handover: no MYGO_DEV_SOCKET (a production build, another
// launcher, or --no-reload-state), or the browser, which has no reload to
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
	if h := App.reloadHandlers(); h.Save != nil {
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
