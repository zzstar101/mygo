package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

// reloadTimeout is how long mygo dev waits for the running app to hand its
// state over before it replaces it. The app saves on the main thread, so a
// busy app takes a moment; past the timeout the reload goes on without the
// state, as a reload always did.
const reloadTimeout = 2 * time.Second

// reloadPushTimeout is how long mygo dev waits for the state an app pushes
// as it starts itself again (App.Relaunch), which it cannot ask for: by
// the time it knows, the app is gone.
const reloadPushTimeout = 500 * time.Millisecond

// devReloadEnv tells an app of mygo dev where the dev channel listens.
// Like MYGO_DEV, it is not meant for the processes the app starts.
const devReloadEnv = "MYGO_DEV_SOCKET"

// devReloadMessage is one message of the dev channel: one JSON object a
// line.
type devReloadMessage struct {
	// Type is "hello", "prepare-reload" or "state".
	Type string `json:"t"`
	// PID identifies the app that says hello.
	PID int `json:"pid,omitempty"`
	// State is the state of the app that is being replaced, in the shape
	// of mygo's reloadPayload; empty when it keeps nothing.
	State json.RawMessage `json:"state,omitempty"`
}

// devReloadServer is mygo dev's end of the dev channel. It listens on a
// socket in the per-user runtime directory, asks the app it is about to
// replace for its state, and hands it to the app that starts next.
//
// The state only lives here, in memory, for the moment between the two
// builds: nothing is written to disk, so nothing can be stale, and an app
// started by hand is never handed a state.
type devReloadServer struct {
	path string
	ln   net.Listener

	mu    sync.Mutex
	send  sync.Mutex // serializes writes to conn
	conn  net.Conn   // the app that is running, once it said hello
	state []byte     // what the app being replaced saved
	// answered is closed when the running app sent its state, or its
	// connection ended; nil when nothing is waiting.
	answered chan struct{}
}

// startDevReloadServer listens for the app of mygo dev.
//
// The path of the socket belongs to this session alone, so that two
// sessions on the same project never take each other's app: an app is told
// the path of the session that started it, and reaches only that one.
func startDevReloadServer() (*devReloadServer, error) {
	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	file := "mygo-dev-" + strconv.Itoa(os.Getuid()) + "-" + hex.EncodeToString(nonce[:]) + ".sock"
	path := ""
	for _, dir := range []string{os.Getenv("XDG_RUNTIME_DIR"), os.TempDir(), "/tmp"} {
		if dir != "" && len(filepath.Join(dir, file)) < 100 {
			path = filepath.Join(dir, file)
			break
		}
	}
	if path == "" {
		// As the single instance socket: /tmp is the last resort.
		path = filepath.Join("/tmp", file)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	// The state of an app is the app's own: only the user runs mygo dev.
	_ = os.Chmod(path, 0o600)
	s := &devReloadServer{path: path, ln: ln}
	go s.accept()
	return s, nil
}

func (s *devReloadServer) accept() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.serve(conn)
	}
}

// serve reads the messages of one app, until it quits.
func (s *devReloadServer) serve(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			s.forget(conn)
			return
		}
		var msg devReloadMessage
		if json.Unmarshal(line, &msg) != nil {
			continue
		}
		switch msg.Type {
		case "hello":
			s.mu.Lock()
			s.conn = conn
			state := s.state
			s.state = nil
			s.mu.Unlock()
			// The state of the build this one replaces, if it kept any.
			s.write(conn, devReloadMessage{Type: "state", State: state})
		case "state":
			s.mu.Lock()
			if s.answered == nil && s.conn != conn {
				// An answer that came late, from an app a newer one
				// replaced: the state of the build that is running now is
				// what prepare asked of it.
				s.mu.Unlock()
				continue
			}
			s.state = append([]byte(nil), msg.State...)
			wait := s.answered
			s.answered = nil
			s.mu.Unlock()
			if wait != nil {
				close(wait)
			}
		}
	}
}

// forget drops the app that is gone, unless a newer one said hello in the
// meantime: the state of an app that is replaced is not the state of the
// one that replaced it, and an app that quits while mygo dev waits for the
// state of one that is still running ends nothing.
func (s *devReloadServer) forget(conn net.Conn) {
	s.mu.Lock()
	if s.conn != conn {
		s.mu.Unlock()
		return
	}
	s.conn = nil
	wait := s.answered
	s.answered = nil
	s.mu.Unlock()
	if wait != nil {
		close(wait)
	}
}

func (s *devReloadServer) write(conn net.Conn, msg devReloadMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	data = append(data, '\n')
	s.send.Lock()
	defer s.send.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(reloadTimeout))
	_, _ = conn.Write(data)
}

// prepare asks the running app for its state and waits for it, up to
// reloadTimeout. What it returns is what the app of the build that starts
// next should get; it stays here until that app says hello, or is nil when
// there is no app to ask or the app did not answer in time.
func (s *devReloadServer) prepare() []byte {
	s.mu.Lock()
	conn := s.conn
	if conn == nil {
		// No app of ours is running: the first build, an app that quit, or
		// one that pushed its state as it started itself again.
		state := s.state
		s.mu.Unlock()
		return state
	}
	wait := make(chan struct{})
	s.answered = wait
	// What the app is asked for is its state now, not what a state of an
	// earlier reload held, in case a build in between failed.
	s.state = nil
	s.mu.Unlock()

	s.write(conn, devReloadMessage{Type: "prepare-reload"})
	timer := time.NewTimer(reloadTimeout)
	defer timer.Stop()
	select {
	case <-wait:
	case <-timer.C:
	}

	s.mu.Lock()
	state := s.state
	s.answered = nil
	s.mu.Unlock()
	return state
}

// awaitState waits for the state an app pushed without being asked, as it
// starts itself again.
func (s *devReloadServer) awaitState(d time.Duration) {
	s.mu.Lock()
	if s.state != nil {
		s.mu.Unlock()
		return
	}
	wait := make(chan struct{})
	s.answered = wait
	s.mu.Unlock()

	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-wait:
	case <-timer.C:
	}

	s.mu.Lock()
	s.answered = nil
	s.mu.Unlock()
}

// close stops listening: mygo dev is over.
func (s *devReloadServer) close() {
	_ = s.ln.Close()
	s.mu.Lock()
	conn := s.conn
	s.conn = nil
	s.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	_ = os.Remove(s.path)
}
