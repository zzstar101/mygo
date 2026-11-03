package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/hotlink"
)

const hotTestProgram = `package main

import (
	"bufio"
	"fmt"
	"os"

	"github.com/egoist/mygo/internal/hotpatch"
)

//go:noinline
func answer() string { return "before" }

func main() {
	go hotpatch.Serve(os.Getenv("MYGO_HOT"), func(f func()) { f() }, nil)
	for sc := bufio.NewScanner(os.Stdin); sc.Scan(); {
		fmt.Println(answer())
	}
}
`

// A save reaches a running program through what mygo dev does.
func TestHotReloader(t *testing.T) {
	if !hotlink.SupportedPlatform(runtime.GOOS, runtime.GOARCH) || !hotlink.SupportedGo(goEnv("GOVERSION")) {
		t.Skip("hot reload does not run here")
	}
	// Within the module, for its internal import.
	dir, err := os.MkdirTemp("testdata", "hot-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	file, err := filepath.Abs(filepath.Join(dir, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(hotTestProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	c := &Config{root: root, Main: "./" + filepath.ToSlash(dir)}
	build := &hotBuild{flags: hotlink.BuildFlags("github.com/egoist/mygo/cmd/mygo/testdata")}
	exe := filepath.Join(t.TempDir(), "program")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if out, err := exec.Command("go", append(append([]string{"build"}, build.flags...), "-o", exe, c.Main)...).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	h, err := newHotReloader()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.close)
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "MYGO_HOT="+h.addr())
	// Open until the test ends: the program reads it.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stdin.Close() })
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	h.start(c, build, exe, nil, nil)
	// The edit must come after what the program was built from was read, which
	// mygo dev's stamp checks.
	<-h.ready
	if h.err != nil {
		t.Fatal(h.err)
	}

	ctx := context.Background()
	if err := os.WriteFile(file, []byte(strings.Replace(hotTestProgram, `"before"`, `"after"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := h.apply(ctx, []string{file})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.functions) != 1 || res.functions[0] != "main.answer" {
		t.Errorf("replaced %q", res.functions)
	}
	if !h.patched {
		t.Error("a patch did not count")
	}

	// What is not code in a package of the program asks for a restart.
	var refusal *hotlink.Refusal
	if _, err := h.apply(ctx, []string{filepath.Join(filepath.Dir(file), "icon.png")}); !errors.As(err, &refusal) {
		t.Errorf("a resource: %v", err)
	}
	if _, err := h.apply(ctx, []string{filepath.Join(filepath.Dir(file), "new.go")}); !errors.As(err, &refusal) {
		t.Errorf("a new file: %v", err)
	}
	// Code that does not compile is shown, and changes nothing.
	if err := os.WriteFile(file, []byte(strings.Replace(hotTestProgram, `"before"`, `1`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	var compile *hotlink.CompileError
	if _, err := h.apply(ctx, []string{file}); !errors.As(err, &compile) {
		t.Errorf("a compile error: %v", err)
	}
}
