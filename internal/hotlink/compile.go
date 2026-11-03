package hotlink

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// Package is a package of the program, as go list reports it.
type Package struct {
	ImportPath    string
	Name          string
	Dir           string
	Export        string
	Standard      bool
	GoFiles       []string
	SFiles        []string
	CgoFiles      []string
	EmbedFiles    []string
	EmbedPatterns []string
	Imports       []string
	ImportMap     map[string]string
	Module        *struct {
		GoVersion string
		Main      bool
		Replace   *struct{}
	}
	ForTest string
	DepOnly bool
}

// BuildFlags are the go build flags of a program to patch: its own packages,
// those of the module in dir, without inlining.
func BuildFlags(module string) []string {
	return []string{"-gcflags=" + module + "/...=-l"}
}

// List returns the packages of main and all they import, with their compiled
// archives, built in dir with the flags given.
func List(dir string, env []string, flags []string, main string) ([]*Package, error) {
	args := append([]string{"list", "-deps", "-export", "-json"}, flags...)
	cmd := exec.Command("go", append(args, main)...)
	cmd.Dir, cmd.Env = dir, env
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list: %w\n%s", err, errOut.String())
	}
	var pkgs []*Package
	for d := json.NewDecoder(&out); ; {
		p := new(Package)
		if err := d.Decode(p); err == io.EOF {
			return pkgs, nil
		} else if err != nil {
			return nil, err
		}
		pkgs = append(pkgs, p)
	}
}

// Compiler compiles single packages as go build would, without go build's
// cost of working out what changed: the watcher knows. It compiles with -l, as
// the program's own packages must be (BuildFlags): a function inlined elsewhere
// could not be replaced without replacing, and recompiling, everywhere it went.
type Compiler struct {
	Dir     string // where to run
	Env     []string
	GOOS    string
	GOARCH  string
	Version string // of Go, as go1.27.3
	Tmp     string

	tool    string            // the compiler
	exports map[string]string // import path to archive
	pkgs    []*Package        // dependencies before what imports them
	seq     int
}

// NewCompiler returns a Compiler of a program's packages.
func NewCompiler(dir string, env []string, goos, goarch, version, tmp string, pkgs []*Package) (*Compiler, error) {
	c := &Compiler{Dir: dir, Env: env, GOOS: goos, GOARCH: goarch, Version: version, Tmp: tmp, pkgs: pkgs, exports: map[string]string{}}
	for _, p := range pkgs {
		if p.Export != "" {
			c.exports[p.ImportPath] = p.Export
		}
	}
	cmd := exec.Command("go", "env", "GOTOOLDIR")
	cmd.Dir, cmd.Env = dir, env
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	c.tool = filepath.Join(strings.TrimSpace(string(out)), "compile")
	if goos == "windows" {
		c.tool += ".exe"
	}
	return c, nil
}

// CompileError is the compiler rejecting the code, with what it printed.
type CompileError struct{ Output string }

func (e *CompileError) Error() string { return strings.TrimSpace(e.Output) }

// Compiled is a package compiled by CompileChanged.
type Compiled struct {
	Key  string // what objects call it: its import path, or main
	Objs []*object
}

// CompileChanged compiles the packages given, and those that import them where
// what they export changed, whose objects would no longer fit.
func (c *Compiler) CompileChanged(changed []*Package) ([]Compiled, error) {
	dirty := map[string]bool{}
	for _, p := range changed {
		dirty[p.ImportPath] = true
	}
	exports := map[string]string{}
	var out []Compiled
	for _, p := range c.pkgs { // in dependency order
		if p.Standard {
			continue
		}
		if !dirty[p.ImportPath] {
			for _, imp := range p.Imports {
				if _, ok := exports[imp]; ok {
					dirty[p.ImportPath] = true
				}
			}
		}
		if !dirty[p.ImportPath] {
			continue
		}
		if len(p.SFiles) > 0 || len(p.CgoFiles) > 0 {
			return nil, refuse("%s has assembly or cgo", p.ImportPath)
		}
		file, objs, err := c.compile(p, exports)
		if err != nil {
			return nil, err
		}
		if old := c.exports[p.ImportPath]; old != "" {
			if same, err := sameExports(old, file); err != nil {
				return nil, err
			} else if !same {
				exports[p.ImportPath] = file
			}
		}
		key := p.ImportPath
		if p.Name == "main" {
			key = "main"
		}
		out = append(out, Compiled{key, objs})
	}
	// What later edits compile against.
	for path, file := range exports {
		c.exports[path] = file
	}
	return out, nil
}

// sameExports reports whether two archives of a package look the same from
// outside: their export data, which holds the fingerprint of their indexes.
func sameExports(a, b string) (bool, error) {
	ax, err := exportData(a)
	if err != nil {
		return false, err
	}
	bx, err := exportData(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(ax, bx), nil
}

func exportData(file string) ([]byte, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	const magic = "!<arch>\n"
	if !bytes.HasPrefix(b, []byte(magic)) || len(b) < len(magic)+60 {
		return nil, fmt.Errorf("%s: not an archive", file)
	}
	p := b[len(magic):]
	size, err := strconv.Atoi(strings.TrimSpace(string(p[48:58])))
	if err != nil || 60+size > len(p) {
		return nil, fmt.Errorf("%s: a malformed archive", file)
	}
	return p[60 : 60+size], nil
}

// compile compiles p, against the archives overriding those of the program.
func (c *Compiler) compile(p *Package, overrides map[string]string) (string, []*object, error) {
	c.seq++
	out := filepath.Join(c.Tmp, fmt.Sprintf("%s-%d.a", strings.ReplaceAll(p.ImportPath, "/", "_"), c.seq))
	cfg := out + ".cfg"
	var b strings.Builder
	for from, to := range p.ImportMap {
		fmt.Fprintf(&b, "importmap %s=%s\n", from, to)
	}
	// Every package of the program: an edit may import one this package did
	// not. One that is new to the program needs its initialization to run, which
	// checkImports refuses.
	for imp, e := range c.exports {
		if o, ok := overrides[imp]; ok {
			e = o
		}
		fmt.Fprintf(&b, "packagefile %s=%s\n", imp, e)
	}
	if err := os.WriteFile(cfg, []byte(b.String()), 0o644); err != nil {
		return "", nil, err
	}
	defer os.Remove(cfg)

	if err := c.checkImports(p); err != nil {
		return "", nil, err
	}
	path := p.ImportPath
	if p.Name == "main" {
		path = "main"
	}
	args := []string{"-o", out, "-p", path, "-pack", "-importcfg", cfg, "-goversion", c.Version, "-dwarf=false", "-nolocalimports", "-c=4", "-l", "-complete"}
	if p.Module != nil && p.Module.GoVersion != "" {
		args = append(args, "-lang=go"+majorMinor(p.Module.GoVersion))
	}
	if pieByDefault(c.GOOS, c.GOARCH) {
		args = append(args, "-shared")
	}
	if len(p.EmbedFiles) > 0 {
		embed, err := c.writeEmbedConfig(p, out+".embed")
		if err != nil {
			return "", nil, err
		}
		defer os.Remove(embed)
		args = append(args, "-embedcfg", embed)
	}
	for _, f := range p.GoFiles {
		args = append(args, filepath.Join(p.Dir, f))
	}
	cmd := exec.Command(c.tool, args...)
	cmd.Dir, cmd.Env = c.Dir, c.Env
	var errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &errOut, &errOut
	if err := cmd.Run(); err != nil {
		return "", nil, &CompileError{errOut.String()}
	}
	objs, err := readArchive(p.ImportPath, out)
	if err != nil {
		return "", nil, err
	}
	return out, objs, nil
}

// writeEmbedConfig writes what the compiler reads for //go:embed, which go build
// otherwise writes (cmd/go/internal/load.resolveEmbed).
func (c *Compiler) writeEmbedConfig(p *Package, file string) (string, error) {
	cfg := struct {
		Patterns map[string][]string
		Files    map[string]string
	}{map[string][]string{}, map[string]string{}}
	for _, f := range p.EmbedFiles {
		cfg.Files[f] = filepath.Join(p.Dir, filepath.FromSlash(f))
	}
	for _, pattern := range p.EmbedPatterns {
		glob, _ := strings.CutPrefix(pattern, "all:")
		list := []string{}
		for _, f := range p.EmbedFiles {
			if embedMatches(glob, f) {
				list = append(list, f)
			}
		}
		cfg.Patterns[pattern] = list
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return file, os.WriteFile(file, data, 0o644)
}

// embedMatches reports whether an embedded file came from a pattern: the
// pattern names it, or a directory it is in.
func embedMatches(glob, file string) bool {
	for at := file; ; {
		if ok, _ := path.Match(glob, at); ok {
			return true
		}
		i := strings.LastIndexByte(at, '/')
		if i < 0 {
			return false
		}
		at = at[:i]
	}
}

// checkImports refuses a package that imports what the program does not have.
func (c *Compiler) checkImports(p *Package) error {
	fset := token.NewFileSet()
	for _, name := range p.GoFiles {
		f, err := parser.ParseFile(fset, filepath.Join(p.Dir, name), nil, parser.ImportsOnly)
		if err != nil {
			continue // the compiler says it better
		}
		for _, imp := range f.Imports {
			name, _ := strconv.Unquote(imp.Path.Value)
			if to, ok := p.ImportMap[name]; ok {
				name = to
			}
			if name != "unsafe" && name != "C" && c.exports[name] == "" {
				return refuse("%s imports %s, which is new to the running build", p.ImportPath, name)
			}
		}
	}
	return nil
}

func majorMinor(v string) string {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return v
}

// pieByDefault is internal/platform.DefaultPIE, for the platforms supported.
func pieByDefault(goos, goarch string) bool {
	switch goos {
	case "windows", "darwin":
		return true
	case "android", "ios":
		return true
	}
	return false
}

// HostGoVersion returns the Go version the go command in dir is.
func HostGoVersion(dir string, env []string) (string, error) {
	cmd := exec.Command("go", "env", "GOVERSION")
	cmd.Dir, cmd.Env = dir, env
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
