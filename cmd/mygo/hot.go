package main

import (
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"maps"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/internal/hotlink"
	"github.com/egoist/mygo/internal/hotpatch"
)

// hotBuild is how mygo dev builds a program to patch.
type hotBuild struct {
	flags []string // for go build and go list
}

// newHotBuild returns the flags of a development build to patch as it runs, or
// nil, having said why, where -hot cannot work: such apps restart on every change.
func newHotBuild(c *Config) *hotBuild {
	if !hotlink.SupportedPlatform(runtime.GOOS, runtime.GOARCH) {
		warnf("Hot reload does not support %s/%s: every change restarts the app", runtime.GOOS, runtime.GOARCH)
		return nil
	}
	v := goEnv("GOVERSION")
	if !hotlink.SupportedGo(v) {
		warnf("Hot reload does not support %s yet: every change restarts the app", v)
		return nil
	}
	if os.Getenv("CGO_ENABLED") == "1" {
		warnf("Hot reload does not support CGO_ENABLED=1: every change restarts the app")
		return nil
	}
	flags := []string{"-tags=mygo_hot"}
	for _, m := range hotModules(c) {
		flags = append(flags, hotlink.BuildFlags(m)...)
	}
	return &hotBuild{flags: flags}
}

// hotModules returns the modules to patch: the app's, and those it replaces
// with a directory, which are the only ones edited.
func hotModules(c *Config) []string {
	cmd := goCommand(c.root, nil, "list", "-m", "-f", `{{if or .Main .Replace}}{{.Path}}{{end}}`, "all")
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, nil
	if err := cmd.Run(); err != nil {
		return nil
	}
	return strings.Fields(out.String())
}

// hotConnectGrace is how long a save waits for a reloader still getting ready.
const hotConnectGrace = 10 * time.Second

// hotReloader connects mygo dev with one running build.
type hotReloader struct {
	dir      string // holds the socket and compiled packages
	listener net.Listener
	exe      string
	inputs   *buildInputs
	stamp    map[string]string

	mu      sync.Mutex
	ready   chan struct{} // closed when the session is ready, or failed
	err     error
	session *hotlink.Session
	comp    *hotlink.Compiler
	byDir   map[string][]*hotlink.Package
	conn    net.Conn
	enc     *gob.Encoder
	dec     *gob.Decoder
	client  *hotpatch.Client
	patched bool
}

// newHotReloader listens for a build about to start.
func newHotReloader() (*hotReloader, error) {
	dir, err := os.MkdirTemp("", "mygo-hot")
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", filepath.Join(dir, "s"))
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return &hotReloader{dir: dir, listener: ln, ready: make(chan struct{})}, nil
}

func (h *hotReloader) addr() string { return h.listener.Addr().String() }

func (h *hotReloader) close() {
	h.listener.Close()
	h.mu.Lock()
	if h.conn != nil {
		h.conn.Close()
	}
	h.mu.Unlock()
	os.RemoveAll(h.dir)
}

// start readies the reloader for the build at exe, whose inputs looked like
// stamp as it began: it reads what the build was linked from, and takes the
// connection the app makes as it starts. Both take a moment that should not
// delay the app.
func (h *hotReloader) start(c *Config, b *hotBuild, exe string, inputs *buildInputs, stamp map[string]string) {
	h.exe, h.inputs, h.stamp = exe, inputs, stamp
	go func() {
		defer close(h.ready)
		h.err = h.connect(c, b)
	}()
}

func (h *hotReloader) connect(c *Config, b *hotBuild) error {
	env := append(os.Environ(), "CGO_ENABLED=0")
	pkgs, err := hotlink.List(c.root, env, b.flags, c.Main)
	if err != nil {
		return err
	}
	archives := map[string]string{}
	h.byDir = map[string][]*hotlink.Package{}
	for _, p := range pkgs {
		if p.Export == "" {
			continue
		}
		key := p.ImportPath
		if p.Name == "main" {
			key = "main"
		}
		archives[key] = p.Export
		if !p.Standard {
			h.byDir[p.Dir] = append(h.byDir[p.Dir], p)
		}
	}
	s, err := hotlink.NewSession(h.exe, runtime.GOOS, runtime.GOARCH, archives)
	if err != nil {
		return err
	}
	// A module of MyGo older than this CLI has no hot reload to connect.
	if !s.CanPatch() {
		return errors.New("this version of MyGo has no hot reload")
	}
	// What go list compiled must be what the running build was: a save during
	// the build is in the archives and not in the app.
	if h.stamp != nil && !maps.Equal(snapshot(h.inputs), h.stamp) {
		return errors.New("the code changed while it built")
	}
	version, err := hotlink.HostGoVersion(c.root, env)
	if err != nil {
		return err
	}
	if h.comp, err = hotlink.NewCompiler(c.root, env, runtime.GOOS, runtime.GOARCH, version, h.dir, pkgs); err != nil {
		return err
	}
	h.session = s

	conn, err := h.listener.Accept()
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.conn, h.enc, h.dec = conn, gob.NewEncoder(conn), gob.NewDecoder(conn)
	h.mu.Unlock()
	var hello hotpatch.Hello
	if err := h.dec.Decode(&hello); err != nil {
		return err
	}
	res, err := s.Attach(hello)
	if err != nil {
		return err
	}
	if err := h.enc.Encode(res); err != nil {
		return err
	}
	var reserved hotpatch.Reserved
	if err := h.dec.Decode(&reserved); err != nil {
		return err
	}
	if reserved.Err != "" {
		return errors.New(reserved.Err)
	}
	s.Reserved(reserved.At, reserved.Size)
	h.client = &hotpatch.Client{Enc: h.enc, Dec: h.dec}
	s.UseClient(h.client)
	return nil
}

// hotResult is how an attempt went.
type hotResult struct {
	functions []string
	took      time.Duration
}

// apply brings the app to the source after the given files changed. An error
// that is a hotlink.Refusal asks for a restart; one that is a
// hotlink.CompileError shows the compiler's complaint.
func (h *hotReloader) apply(ctx context.Context, changed []string) (*hotResult, error) {
	start := time.Now()
	select {
	case <-h.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(hotConnectGrace):
		// An app that never connected, as one stuck before main.
		return nil, &hotlink.Refusal{Reason: "the app did not connect"}
	}
	if h.err != nil {
		return nil, &hotlink.Refusal{Reason: h.err.Error()}
	}
	pkgs := map[*hotlink.Package]bool{}
	for _, f := range changed {
		if filepath.Ext(f) != ".go" {
			return nil, &hotlink.Refusal{Reason: filepath.Base(f) + " changed"}
		}
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		list := h.byDir[filepath.Dir(f)]
		if len(list) == 0 {
			return nil, &hotlink.Refusal{Reason: filepath.Base(f) + " is new to the build"}
		}
		for _, p := range list {
			if !slices.Contains(p.GoFiles, filepath.Base(f)) {
				return nil, &hotlink.Refusal{Reason: filepath.Base(f) + " is new to the build"}
			}
			pkgs[p] = true
		}
	}
	if len(pkgs) == 0 {
		return &hotResult{took: time.Since(start)}, nil
	}
	list := slices.Collect(maps.Keys(pkgs))
	compiled, err := h.comp.CompileChanged(list)
	if err != nil {
		return nil, err
	}
	patch, err := h.session.Link(compiled)
	if err != nil {
		return nil, err
	}
	if patch == nil {
		return &hotResult{took: time.Since(start)}, nil
	}
	reply, err := h.client.Do(hotpatch.Request{Patch: patch})
	if err != nil {
		return nil, &hotlink.Refusal{Reason: "the app is gone: " + err.Error()}
	}
	if reply.Err != "" {
		return nil, fmt.Errorf("the app could not take the change: %s", reply.Err)
	}
	h.patched = true
	return &hotResult{functions: patch.Functions, took: time.Since(start)}, nil
}
