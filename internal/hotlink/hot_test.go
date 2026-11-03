package hotlink

import (
	"bufio"
	"encoding/gob"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/internal/hotpatch"
)

// running is the host program, patched as it runs.
type running struct {
	t   *testing.T
	in  io.WriteCloser
	out *bufio.Reader
	enc *gob.Encoder
	dec *gob.Decoder

	client *hotpatch.Client
	sess   *Session
	comp   *Compiler
	pkgs   map[string]*Package
	dir    string // the copy of the host that edits change
}

const hostModule = "github.com/egoist/mygo/internal/hotlink/testdata"

func start(t *testing.T) *running {
	t.Helper()
	if !SupportedPlatform(runtime.GOOS, runtime.GOARCH) {
		t.Skipf("hot reload does not support %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	version, err := HostGoVersion(".", os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	if !SupportedGo(version) {
		t.Skipf("hot reload does not support %s", version)
	}
	tmp := t.TempDir()
	// The edits change a copy within the module: its imports of internal
	// packages must stay legal.
	work, err := os.MkdirTemp("testdata", "work-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(work) })
	suffix := "/" + filepath.Base(work)
	for _, name := range []string{"main.go", "lib/lib.go"} {
		b, err := os.ReadFile(filepath.Join("testdata", "host", name))
		if err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(work, name), rehome(string(b), suffix))
	}
	exe := filepath.Join(tmp, "host")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	flags := BuildFlags(hostModule)
	build := exec.Command("go", append(append([]string{"build"}, flags...), "-o", exe, "./"+work)...)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the host: %v\n%s", err, out)
	}
	pkgs, err := List(".", os.Environ(), flags, "./"+work)
	if err != nil {
		t.Fatal(err)
	}
	archives := map[string]string{}
	byName := map[string]*Package{}
	for _, p := range pkgs {
		if p.Export == "" {
			continue
		}
		key := p.ImportPath
		if p.Name == "main" {
			key = "main"
		}
		archives[key] = p.Export
		byName[key] = p
	}
	sess, err := NewSession(exe, runtime.GOOS, runtime.GOARCH, archives)
	if err != nil {
		t.Fatal(err)
	}
	sockDir, err := os.MkdirTemp("", "hot")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(sockDir) })
	sock := filepath.Join(sockDir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })

	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "MYGO_HOT="+sock)
	in, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close(); cmd.Process.Kill(); cmd.Wait() })

	conn, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	r := &running{t: t, in: in, out: bufio.NewReader(stdout), enc: gob.NewEncoder(conn), dec: gob.NewDecoder(conn),
		sess: sess, pkgs: byName, dir: work}
	if r.comp, err = NewCompiler(".", os.Environ(), runtime.GOOS, runtime.GOARCH, version, tmp, pkgs); err != nil {
		t.Fatal(err)
	}

	var hello hotpatch.Hello
	if err := r.dec.Decode(&hello); err != nil {
		t.Fatal(err)
	}
	res, err := sess.Attach(hello)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.enc.Encode(res); err != nil {
		t.Fatal(err)
	}
	var reserved hotpatch.Reserved
	if err := r.dec.Decode(&reserved); err != nil {
		t.Fatal(err)
	}
	if reserved.Err != "" {
		t.Fatal(reserved.Err)
	}
	sess.Reserved(reserved.At, reserved.Size)
	r.client = &hotpatch.Client{Enc: r.enc, Dec: r.dec}
	sess.UseClient(r.client)
	return r
}

// rehome points imports of the host's packages at the copy of it.
func rehome(src, suffix string) string {
	return strings.ReplaceAll(src, hostModule+"/host/", hostModule+suffix+"/")
}

func write(t *testing.T, file, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ask sends a command and returns the lines of its answer.
func (r *running) ask(cmd string, lines int) string {
	r.t.Helper()
	if _, err := io.WriteString(r.in, cmd+"\n"); err != nil {
		r.t.Fatal(err)
	}
	var out []string
	for range lines {
		s, err := r.out.ReadString('\n')
		if err != nil {
			r.t.Fatalf("%s: the host ended: %v, after %q", cmd, err, out)
		}
		out = append(out, strings.TrimSuffix(s, "\n"))
	}
	return strings.Join(out, "\n")
}

// patch compiles what changed and sends it to the program. It returns a
// refusal, and the number of functions replaced.
func (r *running) patch(keys ...string) (int, error) {
	r.t.Helper()
	var changed []*Package
	for _, k := range keys {
		changed = append(changed, r.pkgs[k])
	}
	compiled, err := r.comp.CompileChanged(changed)
	var refusal *Refusal
	if errors.As(err, &refusal) {
		return 0, err
	}
	if err != nil {
		r.t.Fatal(err)
	}
	p, err := r.sess.Link(compiled)
	if errors.As(err, &refusal) {
		return 0, err
	}
	if err != nil {
		r.t.Fatal(err)
	}
	if p == nil {
		return 0, nil
	}
	reply, err := r.client.Do(hotpatch.Request{Patch: p})
	if err != nil {
		r.t.Fatal(err)
	}
	if reply.Err != "" {
		r.t.Fatal(reply.Err)
	}
	if s, err := r.out.ReadString('\n'); err != nil || s != "applied\n" {
		r.t.Fatalf("after a patch: %q %v", s, err)
	}
	return len(p.Functions), nil
}

// edit changes the main package's source by replacing strings in what the file
// holds now, and patches the program.
func (r *running) edit(pairs ...string) (int, error) {
	r.t.Helper()
	file := filepath.Join(r.dir, "main.go")
	b, err := os.ReadFile(file)
	if err != nil {
		r.t.Fatal(err)
	}
	src := string(b)
	for i := 0; i < len(pairs); i += 2 {
		if !strings.Contains(src, pairs[i]) {
			r.t.Fatalf("the source has no %q", pairs[i])
		}
		src = strings.ReplaceAll(src, pairs[i], pairs[i+1])
	}
	write(r.t, file, src)
	return r.patch("main")
}

func (r *running) want(cmd string, want string) {
	r.t.Helper()
	if got := r.ask(cmd, strings.Count(want, "\n")+1); got != want {
		r.t.Errorf("%s: got %q, want %q", cmd, got, want)
	}
}

func read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The runtime treats new code as its own: its stack maps, tables, types and
// variables, through a collection, stack growth and a panic.
func TestPatchKeepsRuntimeWorking(t *testing.T) {
	r := start(t)
	r.want("message", "v1")
	r.ask("state", 1)
	src := rehome(read(t, "testdata/edits/v2.go.txt"), "/"+filepath.Base(r.dir))
	write(t, filepath.Join(r.dir, "main.go"), src)
	if _, err := r.patch("main"); err != nil {
		t.Fatal(err)
	}
	r.want("message", "v2 circle=12 (main.circle)")
	r.want("stress", strings.Join([]string{
		"alloc 88890",
		"deep 10000",
		"recovered assignment to entry in nil map at main.go:106",
		"self main.stress",
		"generic 1 2",
		"goroutines 4028",
		"errors.Is true",
		"same error true",
		"kept type true",
		"switch *point",
		"reflect circle 1 8 added",
		"dropped a b Xyy Thursday",
	}, "\n"))
	// What the program held stayed, and the variable whose initial value the
	// edit changed took it; the method called from code that did not change is
	// the new one.
	r.want("state", "5 20 42 103")
}

// A second edit replaces the first's code too, wherever it is called from.
func TestPatchAgain(t *testing.T) {
	r := start(t)
	r.edit(`return "no stress in v1"`, `return helper()`, `func main() {`, `func helper() string { return "helper 1" }

func main() {`)
	r.want("stress", "helper 1")
	for _, n := range []string{"2", "3", "4"} {
		prev := string(rune(n[0] - 1))
		if _, err := r.edit(`"helper `+prev+`"`, `"helper `+n+`"`); err != nil {
			t.Fatal(err)
		}
		r.want("stress", "helper "+n)
	}
}

// Nothing changes for what an edit did not touch.
func TestPatchOnlyWhatChanged(t *testing.T) {
	r := start(t)
	n, err := r.edit(`// point has a layout`, `// a comment moves the code below it, not more
// point has a layout`)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("a comment replaced %d functions", n)
	}
	n, err = r.edit(`return "v1"`, `return "v1 again"`)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("replaced %d functions, want 1", n)
	}
	r.want("message", "v1 again")
}

// A closure that exists keeps its code: what it captured is laid out as that
// code knows. What it calls is new.
func TestClosuresKeepTheirCode(t *testing.T) {
	r := start(t)
	r.want("held", "the old closure v1")
	if _, err := r.edit(`return "v1"`, `return "v2"`); err != nil {
		t.Fatal(err)
	}
	r.want("held", "the old closure v2")
}

// A package that imports another compiles again when what that exports changes.
func TestPatchDependency(t *testing.T) {
	r := start(t)
	r.want("lib", "name v1 lib v1")
	libFile := filepath.Join(r.dir, "lib", "lib.go")
	src := read(t, libFile)

	// The body of a function: one package.
	src = strings.ReplaceAll(src, `"name v1"`, `"name v2"`)
	write(t, libFile, src)
	if _, err := r.patch(libKey(r)); err != nil {
		t.Fatal(err)
	}
	r.want("lib", "name v2 lib v1")

	// A constant: what uses it holds its value, and compiles again.
	src = strings.ReplaceAll(src, `"lib v1"`, `"lib v2"`)
	write(t, libFile, src)
	if _, err := r.patch(libKey(r)); err == nil {
		r.want("lib", "name v2 lib v2")
	} else {
		// main.main is where the host uses it: a restart is the right answer.
		var refusal *Refusal
		if !errors.As(err, &refusal) {
			t.Fatal(err)
		}
	}
}

func libKey(r *running) string {
	for k := range r.pkgs {
		if strings.HasSuffix(k, "/lib") {
			return k
		}
	}
	r.t.Fatal("no lib")
	return ""
}

// What code alone cannot carry over asks for a restart, and the program goes on
// running what it ran.
func TestRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		from []string
		want string
	}{
		{"a struct changed", []string{`type point struct{ x, y int }`, `type point struct{ x, y, z int }`}, "layout of main.point"},
		{"a struct reordered", []string{`type point struct{ x, y int }`, `type point struct{ y, x int }`}, "layout of main.point"},
		{"main changed", []string{`calls++`, `calls += 2`}, "main changed"},
		{"an init was added", []string{`func main() {`, "func init() { calls = 1 }\n\nfunc main() {"}, "initialization"},
		{"an initializer needs code", []string{`calls    int `, `calls    = message2() `, `func main() {`, "func message2() int { return 1 }\n\nfunc main() {"}, "initialization"},
		{"a new import", []string{`"sync"`, "\"sync\"\n\t\"text/tabwriter\"", `func main() {`, "var _ = tabwriter.Debug\n\nfunc main() {"}, "new to the running build"},
		{"an exported method", []string{`func (p *point) sum()`, "func (p *point) Added() int { return 1 }\n\nfunc (p *point) sum()"}, "exported methods of"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := start(t)
			_, err := r.edit(tc.from...)
			var refusal *Refusal
			if !errors.As(err, &refusal) || !strings.Contains(refusal.Reason, tc.want) {
				t.Fatalf("got %v, want a refusal with %q", err, tc.want)
			}
			r.want("message", "v1")
		})
	}
}

// A helper method changes no value and no interface.
func TestUnexportedMethodsMayChange(t *testing.T) {
	r := start(t)
	if _, err := r.edit(
		`func (p *point) sum() int { return p.x + p.y }`,
		"func (p *point) twice() int { return 2 * (p.x + p.y) }\n\nfunc (p *point) sum() int { return p.twice() }",
	); err != nil {
		t.Fatal(err)
	}
	r.ask("state", 1)
	r.want("state", "2 10 42 6")
}

// Code that fails to compile changes nothing.
func TestCompileError(t *testing.T) {
	r := start(t)
	file := filepath.Join(r.dir, "main.go")
	src := read(t, file)
	write(t, file, strings.Replace(src, `return "v1"`, `return 1`, 1))
	_, err := r.comp.CompileChanged([]*Package{r.pkgs["main"]})
	var ce *CompileError
	if !errors.As(err, &ce) || !strings.Contains(ce.Output, "main.go") {
		t.Fatalf("got %v, want the compiler's complaint", err)
	}
	r.want("message", "v1")
}

// An interface the program never asked for reaches a method of a type it has.
func TestNewInterfaceOnExistingType(t *testing.T) {
	r := start(t)
	if _, err := r.edit(`return "no stress in v1"`, `var v any = &point{x: 3, y: 4}
	return fmt.Sprint(v.(interface{ Area() int }).Area())`); err != nil {
		t.Fatal(err)
	}
	r.want("stress", "12")
}

// A named type that only reaches an interface is the program's own, in new code.
func TestTypesAreThePrograms(t *testing.T) {
	r := start(t)
	if _, err := r.edit(`return "no stress in v1"`, `_, isValue := heldValue.(onlyValue)
	_, isPointer := heldPointer.(*onlyPointer)
	got, isNamed := heldNamed.(namedSlice)
	return fmt.Sprint(isValue, isPointer, isNamed, len(got), reflect.TypeOf(onlyValue{}) == reflect.TypeOf(heldValue))`,
		`"sync"`, "\"reflect\"\n\t\"sync\""); err != nil {
		t.Fatal(err)
	}
	r.want("stress", "true true true 2 true")
}

// An itab the running program made for a conversion is the one new code takes:
// it compares them by address.
func TestItabsAreThePrograms(t *testing.T) {
	r := start(t)
	r.want("convert", "made")
	if _, err := r.edit(`return "no stress in v1"`, `d, ok := heldDynamic.(*dynamic)
	var again dynamicIface = d
	return fmt.Sprint(ok, again == heldDynamic, d.value())`); err != nil {
		t.Fatal(err)
	}
	r.want("stress", "true true 7")
}
