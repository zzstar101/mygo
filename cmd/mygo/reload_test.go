package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// dialDev connects to the dev channel as an app of mygo dev would.
func dialDev(t *testing.T, s *devReloadServer) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("unix", s.path, time.Second)
	if err != nil {
		t.Fatalf("dialing the dev channel: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func sendDev(t *testing.T, conn net.Conn, msg devReloadMessage) {
	t.Helper()
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(append(data, '\n')); err != nil {
		t.Fatalf("sending %q: %v", msg.Type, err)
	}
}

// readDev reads one message, one byte at a time so that nothing a later
// message holds is dropped with a buffer.
func readDev(t *testing.T, conn net.Conn) devReloadMessage {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var line []byte
	one := make([]byte, 1)
	for {
		n, err := conn.Read(one)
		if err != nil {
			t.Fatalf("reading the dev channel: %v", err)
		}
		if n == 0 {
			continue
		}
		if one[0] == '\n' {
			break
		}
		line = append(line, one[0])
	}
	var msg devReloadMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		t.Fatalf("reading %q: %v", line, err)
	}
	return msg
}

// TestDevReloadHandsStateToTheNextBuild walks a reload of mygo dev: the
// running app is asked for its state, and the app that starts next is
// handed it as it says hello.
func TestDevReloadHandsStateToTheNextBuild(t *testing.T) {
	s, err := startDevReloadServer()
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()

	app := dialDev(t, s)
	sendDev(t, app, devReloadMessage{Type: "hello", PID: 1})
	if msg := readDev(t, app); msg.Type != "state" || len(msg.State) != 0 {
		t.Fatalf("the first app was handed %+v, and it has nothing to hand", msg)
	}

	// mygo dev rebuilds: it asks the app it is about to replace for its
	// state, and the app saves on the main thread.
	state := make(chan []byte, 1)
	go func() { state <- s.prepare() }()
	if msg := readDev(t, app); msg.Type != "prepare-reload" {
		t.Fatalf("the running app was asked with %+v", msg)
	}
	saved := json.RawMessage(`{"values":{"draft":"typed"}}`)
	sendDev(t, app, devReloadMessage{Type: "state", State: saved})
	if got := <-state; string(got) != string(saved) {
		t.Errorf("prepare returned %q, want %q", got, saved)
	}

	next := dialDev(t, s)
	sendDev(t, next, devReloadMessage{Type: "hello", PID: 2})
	if msg := readDev(t, next); msg.Type != "state" || string(msg.State) != string(saved) {
		t.Errorf("the next app was handed %+v, want %s", msg, saved)
	}
}

// TestDevReloadAsksTheAppThatIsRunning checks that the state is asked of
// the newest app, not of the one the build before it replaced.
func TestDevReloadAsksTheAppThatIsRunning(t *testing.T) {
	s, err := startDevReloadServer()
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()

	old := dialDev(t, s)
	sendDev(t, old, devReloadMessage{Type: "hello", PID: 1})
	readDev(t, old)
	// The app that starts next says hello, and the old one quits without
	// answering.
	next := dialDev(t, s)
	sendDev(t, next, devReloadMessage{Type: "hello", PID: 2})
	if msg := readDev(t, next); msg.Type != "state" {
		t.Fatalf("the next app was handed %+v", msg)
	}
	_ = old.Close()

	state := make(chan []byte, 1)
	go func() { state <- s.prepare() }()
	if msg := readDev(t, next); msg.Type != "prepare-reload" {
		t.Fatalf("the app that is running was asked with %+v", msg)
	}
	sendDev(t, next, devReloadMessage{Type: "state", State: json.RawMessage(`{"values":{"n":2}}`)})
	if got := <-state; string(got) != `{"values":{"n":2}}` {
		t.Errorf("prepare returned %q", got)
	}
}

// TestDevReloadOfAQuitAppKeepsNothing checks that a reload after the app
// quit, as it does when it crashes, waits for nothing.
func TestDevReloadOfAQuitAppKeepsNothing(t *testing.T) {
	s, err := startDevReloadServer()
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()

	if state := s.prepare(); state != nil {
		t.Errorf("a reload without an app returned %q", state)
	}
	app := dialDev(t, s)
	sendDev(t, app, devReloadMessage{Type: "hello", PID: 1})
	readDev(t, app)
	_ = app.Close()
	// The server notices the app is gone and stops waiting: a reload of an
	// app that quit keeps nothing, and waits no longer than reloadTimeout.
	start := time.Now()
	if state := s.prepare(); state != nil {
		t.Errorf("a reload of a quit app returned %q", state)
	}
	if waited := time.Since(start); waited > reloadTimeout+time.Second {
		t.Errorf("a reload of a quit app waited %v", waited)
	}
}

// TestDevBuildFailureKeepsTheRunningApp checks that a build that fails
// asks the app that is running for nothing: its state is not taken, and it
// runs on, as a reload that did not happen.
func TestDevBuildFailureKeepsTheRunningApp(t *testing.T) {
	root := t.TempDir()
	// The build cannot even stage what it makes: .mygo is a file, not a
	// directory.
	if err := os.WriteFile(filepath.Join(root, ".mygo"), []byte("not a directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	s, err := startDevReloadServer()
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()

	app := dialDev(t, s)
	sendDev(t, app, devReloadMessage{Type: "hello", PID: 1})
	readDev(t, app)

	session := &devSession{root: root, reload: s}
	if _, _, err := session.buildAndLaunch(context.Background(), &task{}, [32]byte{}); err == nil {
		t.Fatal("a build that cannot stage its output succeeded")
	}
	// The app that is running was asked for nothing, and is not stopped.
	_ = app.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if msg, err := readDevLine(bufio.NewReader(app)); err == nil {
		t.Errorf("a failed build asked the app that is running for %+v", msg)
	}
}

// TestDevReloadWithoutAnAnswerStartsFresh checks that mygo dev does not
// wait for an app that does not answer: past reloadTimeout the reload goes
// on, and the build that starts next starts fresh.
func TestDevReloadWithoutAnAnswerStartsFresh(t *testing.T) {
	s, err := startDevReloadServer()
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()

	app := dialDev(t, s)
	sendDev(t, app, devReloadMessage{Type: "hello", PID: 1})
	readDev(t, app)
	// The app is busy: it is asked for its state and says nothing back.
	start := time.Now()
	if state := s.prepare(); state != nil {
		t.Errorf("a reload of an app that did not answer returned %q", state)
	}
	if waited := time.Since(start); waited < reloadTimeout {
		t.Errorf("a reload stopped waiting after %v, before reloadTimeout", waited)
	}

	next := dialDev(t, s)
	sendDev(t, next, devReloadMessage{Type: "hello", PID: 2})
	if msg := readDev(t, next); msg.Type != "state" || len(msg.State) != 0 {
		t.Errorf("the next app was handed %+v, want nothing", msg)
	}
}

// TestDevReloadKeepsPushedState checks the app that starts itself again:
// nothing can ask it for its state, so it pushes it as it goes, and the
// build that starts next gets it without a wait.
func TestDevReloadKeepsPushedState(t *testing.T) {
	s, err := startDevReloadServer()
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()

	app := dialDev(t, s)
	sendDev(t, app, devReloadMessage{Type: "hello", PID: 1})
	readDev(t, app)
	pushed := json.RawMessage(`{"values":{"draft":"typed"}}`)
	sendDev(t, app, devReloadMessage{Type: "state", State: pushed})
	_ = app.Close()
	waitDevGone(t, s)

	s.awaitState(reloadPushTimeout)
	start := time.Now()
	if state := s.prepare(); string(state) != string(pushed) {
		t.Errorf("prepare returned %q, want %s", state, pushed)
	}
	if waited := time.Since(start); waited > reloadPushTimeout {
		t.Errorf("a pushed state was waited for %v", waited)
	}
}

// TestDevReloadDropsALateAnswer checks that an app which answers after a
// newer one said hello does not hand its state to the build after that: it
// is the app that runs now whose state matters.
func TestDevReloadDropsALateAnswer(t *testing.T) {
	s, err := startDevReloadServer()
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()

	old := dialDev(t, s)
	sendDev(t, old, devReloadMessage{Type: "hello", PID: 1})
	readDev(t, old)

	// The build that replaced it is running now.
	next := dialDev(t, s)
	sendDev(t, next, devReloadMessage{Type: "hello", PID: 2})
	readDev(t, next)

	// The app that is gone answers the question mygo dev gave up on.
	sendDev(t, old, devReloadMessage{Type: "state", State: json.RawMessage(`{"values":{"draft":"late"}}`)})
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		state := s.state
		s.mu.Unlock()
		if state != nil || time.Now().After(deadline) {
			if state != nil {
				t.Errorf("a late answer was kept: %s", state)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitDevGone waits for the server to notice that the app it was talking
// to is gone.
func waitDevGone(t *testing.T, s *devReloadServer) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		gone := s.conn == nil
		s.mu.Unlock()
		if gone {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the dev channel still holds the app that quit")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestDevReloadServerSocketIsPrivate checks where the dev channel listens:
// in the per-user runtime directory, readable by the user alone, on a path
// of its own, so that two sessions on the same project never take each
// other's app.
func TestDevReloadServerSocketIsPrivate(t *testing.T) {
	s, err := startDevReloadServer()
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()

	dir := os.TempDir()
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		dir = runtimeDir
	}
	if !strings.HasPrefix(s.path, filepath.Clean(dir)+string(filepath.Separator)) {
		t.Errorf("the dev channel listens at %q, not in %q", s.path, dir)
	}
	fi, err := os.Stat(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		// Windows has no permission bits to set.
		t.Errorf("the dev channel socket is %v", fi.Mode().Perm())
	}

	// A second session on the same project listens on a path of its own,
	// and the first one keeps its own app.
	other, err := startDevReloadServer()
	if err != nil {
		t.Fatal(err)
	}
	defer other.close()
	if other.path == s.path {
		t.Errorf("two sessions share the socket %q", s.path)
	}
	app := dialDev(t, s)
	sendDev(t, app, devReloadMessage{Type: "hello", PID: 1})
	if msg := readDev(t, app); msg.Type != "state" {
		t.Errorf("the first session's app was handed %+v", msg)
	}
	if otherConn := dialDev(t, other); otherConn == nil {
		t.Error("the second session does not listen")
	}
}

// TestDevLaunchWithoutReloadState checks that a session without a dev
// channel, as mygo dev --no-reload-state, tells the app of no channel: it
// neither saves nor restores, and every rebuild starts a fresh app.
func TestDevLaunchWithoutReloadState(t *testing.T) {
	// The environment of mygo dev itself may hold the socket of another
	// session: the app it starts is told of no channel all the same.
	t.Setenv(devReloadEnv, "/tmp/elsewhere.sock")
	s := &devSession{root: t.TempDir()}
	if got := s.reloadEnv(); len(got) != 1 || got[0] != devReloadEnv+"=" {
		t.Errorf("a session without a dev channel told the app %v", got)
	}
	s.reload = &devReloadServer{path: "/tmp/some.sock"}
	if got := s.reloadEnv(); len(got) != 1 || got[0] != devReloadEnv+"=/tmp/some.sock" {
		t.Errorf("the dev channel of the app's environment is %v", got)
	}
}

// readDevLine reads one message of the dev channel.
func readDevLine(r *bufio.Reader) (devReloadMessage, error) {
	var msg devReloadMessage
	line, err := r.ReadBytes('\n')
	if err != nil {
		return msg, err
	}
	return msg, json.Unmarshal(line, &msg)
}

// fakeReloadApp plays an app of mygo dev, in a process of its own: it
// connects to the dev channel, takes the state of the build it replaces
// (MYGO_FAKE_STATE names a file to write it to), and hands its own
// (MYGO_FAKE_SAVE) over when mygo dev asks for it.
func fakeReloadApp() error {
	path := os.Getenv(devReloadEnv)
	if path == "" {
		return errors.New("no dev channel in the environment")
	}
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	send := func(msg devReloadMessage) error {
		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}
		_, err = conn.Write(append(data, '\n'))
		return err
	}
	if err := send(devReloadMessage{Type: "hello", PID: os.Getpid()}); err != nil {
		return err
	}
	r := bufio.NewReader(conn)
	msg, err := readDevLine(r)
	if err != nil {
		return err
	}
	if msg.Type != "state" {
		return fmt.Errorf("mygo dev said %q, not the state of the build before this one", msg.Type)
	}
	if file := os.Getenv("MYGO_FAKE_STATE"); file != "" {
		if err := os.WriteFile(file, msg.State, 0o600); err != nil {
			return err
		}
	}
	for {
		msg, err := readDevLine(r)
		if err != nil {
			return nil // mygo dev is over
		}
		if msg.Type != "prepare-reload" {
			continue
		}
		state := json.RawMessage(os.Getenv("MYGO_FAKE_SAVE"))
		if len(state) == 0 {
			state = json.RawMessage(`{"values":{"count":1}}`)
		}
		return send(devReloadMessage{Type: "state", State: state})
	}
}

// TestDevReloadAcrossProcesses runs the handover as it happens: two builds
// of the app, one after the other, and what the first saved in the second.
func TestDevReloadAcrossProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix only")
	}
	s, err := startDevReloadServer()
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()

	dir := t.TempDir()
	saved := `{"values":{"draft":"typed"},"windows":[{"x":40,"y":50,"width":500,"height":400,"visible":true}]}`

	// The first build: it has nothing to take, and it is the app mygo dev
	// asks for its state.
	first := filepath.Join(dir, "first.json")
	app := startFakeApp(t, s, first, saved)
	if state := waitForFile(t, first); len(state) != 0 {
		t.Errorf("the first build was handed %q", state)
	}
	if got := s.prepare(); string(got) != saved {
		t.Errorf("mygo dev got %q from the app, want %s", got, saved)
	}
	if err := app.Wait(); err != nil {
		t.Errorf("the app did not quit after handing its state over: %v", err)
	}

	// The build that starts next: what the first one saved is its.
	second := filepath.Join(dir, "second.json")
	next := startFakeApp(t, s, second, saved)
	if state := waitForFile(t, second); string(state) != saved {
		t.Errorf("the next build was handed %q, want %s", state, saved)
	}
	_ = next.Process.Kill()
	_ = next.Wait()
}

// startFakeApp starts a build of the app as mygo dev would: the test binary
// playing the app, with the dev channel in its environment.
func startFakeApp(t *testing.T, s *devReloadServer, stateFile, save string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(),
		"MYGO_FAKE_APP=reload-app",
		"MYGO_DEV=1",
		devReloadEnv+"="+s.path,
		"MYGO_FAKE_STATE="+stateFile,
		"MYGO_FAKE_SAVE="+save,
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the app: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return cmd
}

// waitForFile waits for the app to write the state it was handed.
func waitForFile(t *testing.T, path string) []byte {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			return data
		}
		if time.Now().After(deadline) {
			t.Fatalf("the app did not write %s: %v", path, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
