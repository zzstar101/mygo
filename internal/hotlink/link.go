// Package hotlink links what a save compiled against a running program: it
// finds the functions the edit changed by comparing the compiler's objects,
// lays them out in memory the program set aside, resolves their references to
// the program's own functions, variables and types, and writes the tables the
// runtime needs to run them (the function table, stack maps, garbage collector
// masks), so that the program takes them as it takes a plugin.
//
// It refuses what code alone cannot carry over, as a struct that changed its
// layout while values of it are alive: mygo dev -hot restarts the app then.
//
// It reads what Go's linker reads, in the formats of the Go it supports
// (checkToolchain); any other Go restarts as well.
package hotlink

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/egoist/mygo/internal/hotpatch"
)

// Refusal says why a change needs the app restarted.
type Refusal struct{ Reason string }

func (r *Refusal) Error() string { return r.Reason }

func refuse(format string, args ...any) error {
	return &Refusal{fmt.Sprintf(format, args...)}
}

// Session links the patches of one running program.
type Session struct {
	host  *host
	goos  string
	arch  string
	slide uint64 // what the loader added to the image's addresses

	next, limit uint64 // the free part of the program's region

	running map[string][]*object // what the program was linked from, by import path

	// The compiled object of each package: runningObjects as of the last patch,
	// current as of what is being linked.
	runningObjects, current map[string]*object

	defs map[string]symLoc // every symbol defined, by key; built on the first need

	// QueryItab asks the running program for an itab of its own, or 0. Conversions
	// make itabs as they run, which nothing else knows of.
	QueryItab func(inter, typ uint64) (uint64, error)

	inCode map[string]uint64 // what typeInCode found, or 0 for nothing
	refs   map[*object]map[uint32][]reference

	latest map[string]uint64   // where patches put symbols, by key
	copies map[string][]uint64 // every copy of a function, by key, each of which must jump to the newest
}

type symLoc struct {
	o *object
	i uint32
}

// NewSession reads the host executable and the objects it was linked from.
func NewSession(exe, goos, goarch string, archives map[string]string) (*Session, error) {
	if !SupportedPlatform(goos, goarch) {
		return nil, refuse("hot reload does not support %s/%s", goos, goarch)
	}
	h, err := openHost(exe)
	if err != nil {
		return nil, err
	}
	s := &Session{
		host: h, goos: goos, arch: goarch,
		running: map[string][]*object{}, runningObjects: map[string]*object{}, current: map[string]*object{},
		latest: map[string]uint64{}, copies: map[string][]uint64{},
	}
	for pkg, file := range archives {
		objs, err := readArchive(pkg, file)
		if err != nil {
			return nil, err
		}
		s.running[pkg] = objs
		s.runningObjects[pkg] = objs[0]
		s.current[pkg] = objs[0]
	}
	return s, nil
}

// typeInCode finds a type descriptor the walk of descriptors could not reach:
// one that only code refers to, as a type converted to any and nothing else.
// The objects say which instructions refer to it, and the executable holds them
// with the address filled in. It returns a link-time address.
func (s *Session) typeInCode(key typeKey, sym string) (uint64, bool) {
	if a, ok := s.inCode[sym]; ok {
		return a, a != 0
	}
	if s.inCode == nil {
		s.inCode = map[string]uint64{}
	}
	var found uint64
	for _, objs := range s.running {
		for _, o := range objs {
			if found = s.typeInObject(o, key, sym); found != 0 {
				break
			}
		}
		if found != 0 {
			break
		}
	}
	s.inCode[sym] = found
	return found, found != 0
}

func (s *Session) typeInObject(o *object, key typeKey, sym string) uint64 {
	// Only an object that uses the type has it.
	var idx uint32
	has := false
	for _, i := range o.indexes() {
		if name := o.symName(i); isTypeDesc(name) && symKey(name, o.symABI(i)) == sym {
			idx, has = i, true
			break
		}
	}
	if !has {
		return 0
	}
	check := func(a uint64) bool {
		if a < s.host.typesAt || a >= s.host.etypes {
			return false
		}
		// It must be what was asked: an edit may have reused the name.
		d := s.host.read(a, typeHeader)
		return d != nil && binary.LittleEndian.Uint32(d[typeHash:]) == key.hash && s.host.typeString(s.host.typesAt, d) == key.str
	}
	if a := s.locate(o, idx, check, 2); a != 0 {
		s.host.walkTypes([]uint64{a})
		return a
	}
	return 0
}

// reference is a relocation of symbol from.
type reference struct {
	from uint32
	r    reloc
}

// references returns what refers to each symbol the object defines.
func (s *Session) references(o *object) map[uint32][]reference {
	if refs, ok := s.refs[o]; ok {
		return refs
	}
	refs := map[uint32][]reference{}
	for _, i := range o.indexes() {
		switch o.symKind(i) {
		case kindText, kindTextFIPS, kindData, kindDataFIPS, kindNoptrData, kindNoptrDataFIPS, kindRodata, kindRodataFIPS:
		default:
			continue
		}
		for j := range o.nreloc(i) {
			r := o.reloc(i, j)
			if r.sym.pkg == pkgIdxInvalid || isMarker(r.typ) {
				continue
			}
			t, err := s.resolveRunning(o, r.sym)
			if err == nil && t.o == o {
				refs[t.i] = append(refs[t.i], reference{i, r})
			}
		}
	}
	if s.refs == nil {
		s.refs = map[*object]map[uint32][]reference{}
	}
	s.refs[o] = refs
	return refs
}

// locate finds where the executable has a symbol the symbol table leaves out,
// by what refers to it there. Something may only refer to it through another
// such symbol, as code does to what a type assertion's data names; depth is how
// many of those to go through.
func (s *Session) locate(o *object, idx uint32, check func(uint64) bool, depth int) uint64 {
	for _, ref := range s.references(o)[idx] {
		var at uint64
		if o.symABI(ref.from) != abiStatic {
			at = s.host.syms[o.symName(ref.from)]
		}
		if at == 0 && depth > 0 {
			at = s.locate(o, ref.from, func(a uint64) bool {
				return s.host.read(a, int(o.symSize(ref.from))) != nil
			}, depth-1)
		}
		if at == 0 {
			continue
		}
		code := s.host.read(at, int(o.symSize(ref.from)))
		if code == nil {
			continue
		}
		if a, ok := decodeReference(s.arch, code, at, ref.r); ok && check(a) {
			return a
		}
	}
	return 0
}

// decodeReference reads back the address a relocation put in linked code.
func decodeReference(arch string, code []byte, at uint64, r reloc) (uint64, bool) {
	off := int(r.off)
	if off < 0 || off+int(r.siz) > len(code) {
		return 0, false
	}
	pc := at + uint64(off)
	le := binary.LittleEndian
	switch r.typ {
	case rADDR:
		if r.siz != 8 {
			return 0, false
		}
		return uint64(int64(le.Uint64(code[off:])) - r.add), true
	case rADDRARM64:
		if off+8 > len(code) {
			return 0, false
		}
		o0, o1 := le.Uint32(code[off:]), le.Uint32(code[off+4:])
		lo := int64((o0 >> 29) & 3)
		hi := int64(int32(o0<<8) >> 13) // the signed 19 bits at 5
		page := int64(pc&^0xfff) + (hi<<2|lo)<<12
		return uint64(page + int64((o1>>10)&0xfff) - r.add), true
	case rPCREL:
		if r.siz != 4 {
			return 0, false
		}
		d := int64(int32(le.Uint32(code[off:])))
		return uint64(int64(pc) + int64(r.siz) + d - r.add), true
	}
	return 0, false
}

// CanPatch reports whether the program can connect at all: a module of MyGo
// older than the CLI ignores the build tag.
func (s *Session) CanPatch() bool {
	_, ok := s.host.syms["github.com/egoist/mygo/internal/hotpatch.Probe"]
	return ok
}

// Attach takes what the running program said as it connected, and returns
// the memory in its image that it should set aside for patches.
func (s *Session) Attach(hello hotpatch.Hello) (hotpatch.Reserve, error) {
	if !SupportedGo(hello.Version) {
		return hotpatch.Reserve{}, refuse("hot reload does not support %s", hello.Version)
	}
	const pkg = "github.com/egoist/mygo/internal/hotpatch."
	probe, ok := s.host.syms[pkg+"Probe"]
	region, ok2 := s.host.syms[pkg+"region"]
	if !ok || !ok2 {
		return hotpatch.Reserve{}, refuse("the program has no hot reload")
	}
	s.slide = hello.Probe - probe
	return hotpatch.Reserve{
		At:    rnd(region+s.slide, pageSize),
		Limit: (region + s.slide + hotpatch.RegionSize) &^ (pageSize - 1),
	}, nil
}

// UseClient has the session ask the running program what only it knows.
func (s *Session) UseClient(c *hotpatch.Client) {
	table, ok := s.host.syms["runtime.itabTable"]
	if !ok {
		return
	}
	s.QueryItab = func(inter, typ uint64) (uint64, error) {
		reply, err := c.Do(hotpatch.Request{Itab: &hotpatch.ItabQuery{Inter: inter, Type: typ, Table: table + s.slide}})
		return reply.Itab, err
	}
}

// Reserved takes the memory the program set aside.
func (s *Session) Reserved(at, size uint64) {
	s.next, s.limit = at, at+size
}

// SupportedPlatform reports whether hot reload runs on a platform.
func SupportedPlatform(goos, goarch string) bool {
	return (goos == "darwin" || goos == "linux" || goos == "windows") && (goarch == "arm64" || goarch == "amd64")
}

// SupportedGo reports whether this package knows the formats of a Go release.
func SupportedGo(version string) bool {
	return strings.HasPrefix(version, "go1.27.") || version == "go1.27"
}

// target is a symbol a reference resolved to. o is nil for a symbol only
// named, whose definition, if needed, is found by key.
type target struct {
	o    *object
	i    uint32
	key  string
	name string
	abi  uint16
}

func symKey(name string, abi uint16) string {
	return string(rune('0'+abi)) + ":" + name
}

// resolve finds what a reference of o points to, among the objects compiled
// with it. Objects refer to the symbols of other packages by index, which a
// recompile of that package changes; mygo dev recompiles those that import it
// then (the fingerprints of objects enforce it), so the program's objects and
// the current ones are each consistent.
func (s *Session) resolve(o *object, r symRef) (target, error) {
	return s.resolveIn(s.current, o, r)
}

// resolveRunning is resolve for an object of the running program.
func (s *Session) resolveRunning(o *object, r symRef) (target, error) {
	return s.resolveIn(s.runningObjects, o, r)
}

func (s *Session) resolveIn(view map[string]*object, o *object, r symRef) (target, error) {
	var i uint32
	switch r.pkg {
	case pkgIdxInvalid:
		return target{}, nil
	case pkgIdxHashed64:
		i = r.idx + o.ndef
	case pkgIdxHashed:
		i = r.idx + o.ndef + o.nhashed64
	case pkgIdxNone:
		i = r.idx + o.ndef + o.nhashed64 + o.nhashed
	case pkgIdxBuiltin:
		b := builtins[r.idx]
		return target{key: symKey(b.name, uint16(b.abi)), name: b.name, abi: uint16(b.abi)}, nil
	case pkgIdxSelf:
		i = r.idx
	default:
		pkg := o.pkgName(r.pkg)
		dep := view[pkg]
		if dep == nil {
			return target{}, refuse("a change imports %s, which the running build does not have", pkg)
		}
		o, i = dep, r.idx
	}
	return s.target(o, i), nil
}

func (s *Session) target(o *object, i uint32) target {
	name, abi := o.symName(i), o.symABI(i)
	t := target{o: o, i: i, name: name, abi: abi}
	switch {
	case !hasIdentity(o, i):
		t.key = "#" + o.symHash(i)
	case abi == abiStatic:
		t.key = fmt.Sprintf("s%p:%d", o, i)
	default:
		t.key = symKey(name, abi)
		if i >= o.ndefined() {
			t.o = nil // only named here
		}
	}
	return t
}

// hasIdentity reports whether a symbol is what its name says, rather than any
// bytes alike. The compiler marks functions, itabs and types content addressed
// too, for the linker to merge copies, but one program has one of each of those.
func hasIdentity(o *object, i uint32) bool {
	if o.symHash(i) == "" || isText(o.symKind(i)) || o.symFlag2(i)&symFlag2Itab != 0 {
		return true
	}
	return isTypeDesc(o.symName(i))
}

func isText(kind uint8) bool { return kind == kindText || kind == kindTextFIPS }

// def finds where a symbol only named is defined.
func (s *Session) def(t target) (target, bool) {
	if t.o != nil {
		return t, true
	}
	if s.defs == nil {
		s.defs = map[string]symLoc{}
		for _, objs := range s.running {
			for _, o := range objs {
				s.indexObject(o)
			}
		}
	}
	l, ok := s.defs[t.key]
	if !ok {
		return t, false
	}
	return s.target(l.o, l.i), true
}

func (s *Session) indexObject(o *object) {
	for _, i := range o.indexes() {
		if abi := o.symABI(i); abi != abiStatic && hasIdentity(o, i) {
			key := symKey(o.symName(i), abi)
			// The newest definition wins; a function's body beats a stub.
			if l, ok := s.defs[key]; !ok || (o.symSize(i) > 0 && l.o.symSize(l.i) == 0) || l.o.pkg == o.pkg {
				s.defs[key] = symLoc{o, i}
			}
		}
	}
}

func (o *object) indexes() []uint32 {
	out := make([]uint32, 0, o.ndefined())
	for i := range o.ndefined() {
		out = append(out, i)
	}
	return out
}

var closure = regexp.MustCompile(`\.(func|gowrap|deferwrap)\d+(\.\d+)*(\.(func|gowrap|deferwrap)\d+(\.\d+)*)*$`)

// isClosure reports whether name is a function literal's or another
// function the compiler made of part of a function. What it captured lies in
// a layout only the code that made it knows, so a closure that exists keeps
// its code: the next call of the function around it makes the new one.
func isClosure(name string) bool { return closure.MatchString(name) }

// isTypeDesc reports whether a symbol is a type descriptor, not one of the
// strings and functions that have the same prefix.
func isTypeDesc(name string) bool {
	return strings.HasPrefix(name, "type:") && !strings.HasPrefix(name, "type:.")
}

// fingerprints computes what a symbol of a package is made of: its bytes,
// what it refers to, and what the code generator wrote for the runtime, but
// not line numbers, so a line added above a function does not replace it. A
// symbol the object alone owns (a literal's data, a closure) counts as content,
// by what it holds rather than by the number the compiler gave it.
type fingerprints struct {
	s       *Session
	o       *object
	resolve func(*object, symRef) (target, error)
	memo    map[uint32][32]byte
}

func newFingerprints(s *Session, o *object, resolve func(*object, symRef) (target, error)) *fingerprints {
	return &fingerprints{s, o, resolve, map[uint32][32]byte{}}
}

func (f *fingerprints) of(i uint32) [32]byte {
	if h, ok := f.memo[i]; ok {
		return h
	}
	f.memo[i] = [32]byte{} // a cycle counts for nothing
	o := f.o
	h := sha256.New()
	var buf [16]byte
	binary.LittleEndian.PutUint32(buf[:], o.symSize(i))
	buf[4], buf[5] = o.symKind(i), o.symFlag(i)
	h.Write(buf[:6])
	h.Write(o.symData(i))
	name := o.symName(i)
	for j := range o.nreloc(i) {
		r := o.reloc(i, j)
		if isMarker(r.typ) {
			continue
		}
		binary.LittleEndian.PutUint32(buf[:], uint32(r.off))
		buf[4] = r.siz
		binary.LittleEndian.PutUint16(buf[5:], r.typ)
		binary.LittleEndian.PutUint64(buf[7:], uint64(r.add))
		h.Write(buf[:15])
		t, err := f.resolve(o, r.sym)
		if err != nil {
			h.Write([]byte(err.Error()))
			continue
		}
		if t.o == o && t.i < o.ndefined() && f.owned(name, t) {
			sum := f.of(t.i)
			h.Write(sum[:])
		} else {
			h.Write([]byte(t.key))
		}
	}
	for _, a := range o.auxs(i) {
		switch a.typ {
		case auxPcsp, auxPcdata, auxPcinline, auxFuncdata:
			t, err := f.resolve(o, a.sym)
			if err != nil || t.o == nil {
				continue
			}
			h.Write([]byte{a.typ})
			if a.typ == auxFuncdata && t.o == o {
				sum := f.of(t.i)
				h.Write(sum[:])
			} else {
				h.Write(t.o.symData(t.i))
			}
		}
	}
	var sum [32]byte
	h.Sum(sum[:0])
	f.memo[i] = sum
	return sum
}

// owned reports whether t belongs to the symbol named owner: its closures,
// and the data only it refers to.
func (f *fingerprints) owned(owner string, t target) bool {
	if t.abi == abiStatic || isNumbered(t.name) {
		return true
	}
	return strings.HasPrefix(t.name, owner+".") || strings.HasPrefix(t.name, owner+"·")
}

// isMarker reports whether a relocation only tells the linker something.
func isMarker(typ uint16) bool {
	switch typ {
	case rUSEFIELD, rUSETYPE, rUSEIFACE, rUSEIFACEMETHOD, rUSENAMEDMETHOD, rKEEP, rINITORDER, rCALLIND, rDWARFSECREF, rADDRCUOFF:
		return true
	}
	return false
}

// The relocations of the architectures it supports (objabi.RelocType).
const (
	rADDR             = 1
	rADDRARM64        = 3
	rADDROFF          = 5
	rSIZE             = 6
	rCALL             = 7
	rCALLARM64        = 9
	rCALLIND          = 10
	rCONST            = 13
	rPCREL            = 14
	rUSEFIELD         = 21
	rUSETYPE          = 22
	rUSEIFACE         = 23
	rUSEIFACEMETHOD   = 24
	rUSENAMEDMETHOD   = 25
	rMETHODOFF        = 26
	rKEEP             = 27
	rGOTPCREL         = 29
	rDWARFSECREF      = 31
	rARM64PCRELLDST8  = 37
	rARM64PCRELLDST16 = 38
	rARM64PCRELLDST32 = 39
	rARM64PCRELLDST64 = 40
	rADDRCUOFF        = 98
	rINITORDER        = 102
)

// Change is what a recompiled package changed.
type change struct {
	funcs []uint32 // changed functions to replace (not closures)
	data  []uint32 // variables whose initial value changed
	ro    map[string]bool
}

// diff compares a package's new object with the one the program runs.
func (s *Session) diff(old, cur *object) (*change, error) {
	oldIdx := map[string]uint32{}
	for _, i := range old.indexes() {
		if hasIdentity(old, i) && old.symABI(i) != abiStatic {
			oldIdx[symKey(old.symName(i), old.symABI(i))] = i
		}
	}
	oldFP, newFP := newFingerprints(s, old, s.resolveRunning), newFingerprints(s, cur, s.resolve)
	c := &change{ro: map[string]bool{}}
	// Types first: code that changed may have changed because they did, and
	// their change is what to tell.
	for _, i := range cur.indexes() {
		if name := cur.symName(i); isTypeDesc(name) && cur.symABI(i) != abiStatic {
			if oi, had := oldIdx[symKey(name, cur.symABI(i))]; had {
				if err := s.checkType(name, old, oi, cur, i); err != nil {
					return nil, err
				}
			}
		}
	}
	for _, i := range cur.indexes() {
		name, kind := cur.symName(i), cur.symKind(i)
		// A symbol without a name is part of another: its metadata.
		if name == "" || !hasIdentity(cur, i) || cur.symABI(i) == abiStatic || isNumbered(name) {
			continue
		}
		key := symKey(name, cur.symABI(i))
		oi, had := oldIdx[key]
		if isTypeDesc(name) {
			continue
		}
		if had && oldFP.of(oi) == newFP.of(i) {
			continue
		}
		switch kind {
		case kindText, kindTextFIPS:
			if err := checkRunsOnce(name, cur, i); err != nil {
				return nil, err
			}
			switch {
			case had && isClosure(name):
				c.ro[key] = true // its new code, for the new code to refer to
			case had:
				c.funcs = append(c.funcs, i)
			}
		case kindRodata, kindRodataFIPS:
			if had {
				c.ro[key] = true
			}
		case kindData, kindDataFIPS, kindNoptrData, kindNoptrDataFIPS, kindBSS, kindNoptrBSS:
			if !had {
				continue
			}
			if old.symSize(oi) != cur.symSize(i) {
				return nil, refuse("%s changed size", name)
			}
			c.data = append(c.data, i)
		}
	}
	return c, nil
}

// initFunc matches what runs as a package initializes: its init functions,
// their closures, and maps it fills. A method named init has a receiver, and so
// parentheses, in its name.
var initFunc = regexp.MustCompile(`^[^()]*\.init(\.\d+)*(\.func\d+.*)?$|^[^()]*\.map\.init\.\d+$`)

// checkRunsOnce refuses a change to code that ran as the program started,
// which replacing would not run again.
func checkRunsOnce(name string, o *object, i uint32) error {
	switch {
	case name == "main.main" || strings.HasPrefix(name, "main.main."):
		return refuse("main changed")
	case o.symFlag2(i)&symFlag2PkgInit != 0 || initFunc.MatchString(name):
		return refuse("a package's initialization changed (%s)", name)
	}
	return nil
}

// checkType refuses a change to a type's layout: values of it are alive in
// the running program. Methods may come and go, but only unexported ones,
// since what a type's exported methods are reaches the app by reflection
// (mygo.Bind) and by interfaces it now implements.
func (s *Session) checkType(name string, old *object, oi uint32, cur *object, i uint32) error {
	a, b := old.symData(oi), cur.symData(i)
	if len(a) < typeHeader || len(b) < typeHeader {
		return nil
	}
	if binary.LittleEndian.Uint64(a[typeSize:]) != binary.LittleEndian.Uint64(b[typeSize:]) ||
		binary.LittleEndian.Uint64(a[typePtrBytes:]) != binary.LittleEndian.Uint64(b[typePtrBytes:]) ||
		a[typeKind] != b[typeKind] {
		return refuse("the layout of %s changed", strings.TrimPrefix(name, "type:"))
	}
	ax, err := s.typeShape(s.resolveRunning, old, oi)
	if err != nil {
		return err
	}
	bx, err := s.typeShape(s.resolve, cur, i)
	if err != nil {
		return err
	}
	if ax.layout != bx.layout {
		return refuse("the layout of %s changed", strings.TrimPrefix(name, "type:"))
	}
	if !slices.Equal(ax.exported, bx.exported) {
		return refuse("the exported methods of %s changed", strings.TrimPrefix(name, "type:"))
	}
	return nil
}

// typeShape is what a type descriptor says apart from where its methods are.
type typeShape struct {
	layout   string
	exported []string // the exported methods, with their signatures
}

func (s *Session) typeShape(resolve func(*object, symRef) (target, error), o *object, i uint32) (typeShape, error) {
	d := o.symData(i)
	k := d[typeKind] & 0x1f
	base, add, err := kindSizes(k, d)
	if err != nil {
		return typeShape{}, err
	}
	var mstart, mend int
	if d[typeTFlag]&tflagUncommon != 0 {
		u := d[base:]
		moff := int(binary.LittleEndian.Uint32(u[8:]))
		mcount := int(binary.LittleEndian.Uint16(u[4:]))
		mstart = base + moff
		mend = mstart + mcount*methodSize
	}
	_ = add

	// Relocations outside the method table, by target, in order.
	type rel struct {
		off int
		key string
	}
	var rels []rel
	methods := map[int][2]string{} // by the method's entry, its name and type
	for j := range o.nreloc(i) {
		r := o.reloc(i, j)
		if isMarker(r.typ) {
			continue
		}
		t, err := resolve(o, r.sym)
		if err != nil {
			return typeShape{}, err
		}
		off := int(r.off)
		if off >= mstart && off < mend {
			m := (off - mstart) / methodSize
			e := methods[m]
			switch (off - mstart) % methodSize {
			case 0:
				e[0] = symBytes(t)
			case 4:
				e[1] = t.key
			}
			methods[m] = e
			continue
		}
		rels = append(rels, rel{off, t.key})
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d %d %d %d\n", binary.LittleEndian.Uint64(d[typeSize:]), binary.LittleEndian.Uint64(d[typePtrBytes:]), k, len(d[:base]))
	for _, r := range rels {
		// What a reloc points to, in order, is the layout's types and names;
		// its place differs when the method table before it grew.
		if r.off >= mend && mend > mstart {
			fmt.Fprintf(&b, "+%s\n", r.key)
		} else {
			fmt.Fprintf(&b, "%d %s\n", r.off, r.key)
		}
	}
	if k == kStruct {
		// Field offsets are bytes, not relocations.
		n := int(binary.LittleEndian.Uint64(d[64:]))
		at := base
		if d[typeTFlag]&tflagUncommon != 0 {
			at += uncommonSize
		}
		for f := range n {
			if p := at + f*24 + 16; p+8 <= len(d) {
				fmt.Fprintf(&b, "f%d\n", binary.LittleEndian.Uint64(d[p:]))
			}
		}
	}
	shape := typeShape{layout: b.String()}
	for m := range len(methods) {
		e := methods[m]
		if e[0] != "" && isExportedName(e[0]) {
			shape.exported = append(shape.exported, e[0]+" "+e[1])
		}
	}
	sort.Strings(shape.exported)
	return shape, nil
}

// symBytes decodes a type name's data: a flag byte, its length and its text.
func symBytes(t target) string {
	if t.o == nil {
		return ""
	}
	d := t.o.symData(t.i)
	if len(d) < 2 {
		return ""
	}
	n, w := binary.Uvarint(d[1:])
	if w <= 0 || 1+w+int(n) > len(d) {
		return ""
	}
	return string(d[1+w : 1+w+int(n)])
}

func isExportedName(n string) bool { return n != "" && n[0] >= 'A' && n[0] <= 'Z' }

// Link makes the patch that brings the running program to the packages
// compiled, each given as its archive's objects. It only returns a Refusal for
// changes that need a restart. A nil patch means nothing changed.
func (s *Session) Link(list []Compiled) (*hotpatch.Patch, error) {
	compiled := map[string][]*object{}
	for _, c := range list {
		compiled[c.Key] = c.Objs
	}
	l := &linker{s: s, placed: map[string]*placed{}, replaced: map[string]bool{}, changedRO: map[string]bool{},
		heapItabs: map[[2]uint64]uint64{}, far: map[uint64]uint64{}}
	for pkg := range compiled {
		if s.running[pkg] == nil {
			return nil, refuse("%s is new to the running build", pkg)
		}
	}
	// The new objects first: references resolve through them.
	for pkg, objs := range compiled {
		s.current[pkg] = objs[0]
	}
	restore := func() {
		for pkg := range compiled {
			s.current[pkg] = s.runningObjects[pkg]
		}
	}
	var writes []varWrite
	var fns []target
	for pkg, objs := range compiled {
		c, err := s.diff(s.runningObjects[pkg], objs[0])
		if err != nil {
			restore()
			return nil, err
		}
		for _, i := range c.funcs {
			t := s.target(objs[0], i)
			fns = append(fns, t)
			l.replaced[t.key] = true
		}
		for _, i := range c.data {
			writes = append(writes, varWrite{s.target(objs[0], i)})
		}
		for k := range c.ro {
			l.changedRO[k] = true
		}
	}
	sort.Slice(fns, func(i, j int) bool { return fns[i].key < fns[j].key })

	if len(fns) == 0 && len(writes) == 0 {
		l.commitObjects(compiled)
		return nil, nil
	}
	p, err := l.link(fns, writes)
	if err != nil {
		restore()
		return nil, err
	}
	l.commitObjects(compiled)
	return p, nil
}

func (l *linker) commitObjects(compiled map[string][]*object) {
	s := l.s
	for pkg, objs := range compiled {
		s.running[pkg] = append([]*object{objs[0]}, s.running[pkg][1:]...)
		s.runningObjects[pkg] = objs[0]
		s.current[pkg] = objs[0]
	}
	if s.defs != nil {
		for _, objs := range compiled {
			s.indexObject(objs[0])
		}
	}
}
