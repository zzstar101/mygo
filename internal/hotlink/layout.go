package hotlink

import (
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/egoist/mygo/internal/hotpatch"
)

type section int

const (
	secText section = iota
	secRodata
	secData
	secNoptrData
	secBSS
	secNoptrBSS
	numSections
)

// placed is a symbol this patch carries.
type placed struct {
	t    target
	sec  section
	addr uint64
	size uint64
}

type varWrite struct{ t target }

type linker struct {
	s         *Session
	placed    map[string]*placed
	order     [numSections][]*placed
	replaced  map[string]bool // functions the edit changed
	changedRO map[string]bool // read-only data the edit changed

	queue []*placed

	heapItabs map[[2]uint64]uint64 // what the program said of itabs
	far       map[uint64]uint64    // addresses beyond the reach of code, and their slots
}

func (l *linker) align() uint64 {
	if l.s.arch == "amd64" {
		return 32
	}
	return 16
}

func kindSection(k uint8) (section, bool) {
	switch k {
	case kindText, kindTextFIPS:
		return secText, true
	case kindRodata, kindRodataFIPS:
		return secRodata, true
	case kindData, kindDataFIPS:
		return secData, true
	case kindNoptrData, kindNoptrDataFIPS:
		return secNoptrData, true
	case kindBSS:
		return secBSS, true
	case kindNoptrBSS:
		return secNoptrBSS, true
	}
	return 0, false
}

// carried reports whether a symbol goes with each patch that needs it, not
// shared with the program: what has no identity (strings and other content
// addressed data), what is its object's alone, and the pointer masks of stack
// objects, which the runtime finds only from the module of their function.
func carried(t target) bool {
	return strings.HasPrefix(t.key, "#") || t.abi == abiStatic || strings.HasPrefix(t.name, "runtime.gcbits.") || isNumbered(t.name)
}

// isNumbered reports whether a symbol's name is only a number the compiler
// gave it in its package, which another compile gives to another: a name says
// nothing of what it is.
func isNumbered(name string) bool {
	return strings.Contains(name, "..typeAssert.") || strings.Contains(name, "..interfaceSwitch.")
}

// hostAddr returns where the running program has a symbol.
func (l *linker) hostAddr(t target) (uint64, bool) {
	s := l.s
	if carried(t) {
		return 0, false
	}
	if a, ok := s.latest[t.key]; ok {
		return a, true
	}
	if t.abi == 0 {
		if a, ok := s.host.syms[t.name+".abi0"]; ok {
			return a + s.slide, true
		}
	}
	a, ok := s.host.syms[t.name]
	if !ok {
		return 0, false
	}
	return a + s.slide, true
}

// typeAddr returns where the program has a type descriptor.
func (l *linker) typeAddr(t target) (uint64, bool) {
	d := t.o.symData(t.i)
	if len(d) < typeHeader {
		return 0, false
	}
	str := l.nameOf(t)
	key := typeKey{binary.LittleEndian.Uint32(d[typeHash:]), str}
	if a, ok := l.s.host.types[key]; ok {
		return a + l.s.slide, true
	}
	if a, ok := l.s.typeInCode(key, t.key); ok {
		return a + l.s.slide, true
	}
	return 0, false
}

// nameOf returns the string of a type descriptor in an object.
func (l *linker) nameOf(t target) string {
	d := t.o.symData(t.i)
	for j := range t.o.nreloc(t.i) {
		r := t.o.reloc(t.i, j)
		if r.off == typeStr {
			n, err := l.s.resolve(t.o, r.sym)
			if err != nil {
				return ""
			}
			if n, ok := l.s.def(n); ok {
				return strings.TrimPrefix(symBytes(n), starIf(d[typeTFlag]))
			}
		}
	}
	return ""
}

// itabAddr returns where the program has an itab.
func (l *linker) itabAddr(t target) (uint64, bool) {
	var inter, typ uint64
	for j := range t.o.nreloc(t.i) {
		r := t.o.reloc(t.i, j)
		if r.off != 0 && r.off != 8 {
			continue
		}
		rt, err := l.s.resolve(t.o, r.sym)
		if err != nil {
			return 0, false
		}
		rt, ok := l.s.def(rt)
		if !ok {
			return 0, false
		}
		a, ok := l.typeAddr(rt)
		if !ok {
			return 0, false
		}
		if r.off == 0 {
			inter = a
		} else {
			typ = a
		}
	}
	if inter == 0 || typ == 0 {
		return 0, false
	}
	if a, ok := l.s.host.itabs[[2]uint64{inter - l.s.slide, typ - l.s.slide}]; ok {
		return a + l.s.slide, true
	}
	// One the running program made for a conversion may exist. Code compares
	// itabs by address, so the patch must use that one.
	if a, ok := l.heapItabs[[2]uint64{inter, typ}]; ok {
		return a, a != 0
	}
	var a uint64
	if l.s.QueryItab != nil {
		if got, err := l.s.QueryItab(inter, typ); err == nil {
			a = got
		}
	}
	l.heapItabs[[2]uint64{inter, typ}] = a
	if a != 0 {
		l.far[a] = 0 // too far for code to reach: code loads it from a slot
	}
	return a, a != 0
}

// lookup says where a symbol is: in the patch, or in the program. A symbol
// that is neither is one to carry, if strong.
func (l *linker) lookup(t target) (uint64, *placed, bool) {
	if p, ok := l.placed[t.key]; ok {
		return 0, p, true
	}
	if !l.replaced[t.key] && !l.changedRO[t.key] {
		if carried(t) {
			return 0, nil, false
		}
		// Types and itabs have no entry in symbol tables.
		if isTypeDesc(t.name) || strings.HasPrefix(t.name, "go:itab.") {
			def, ok := l.s.def(t)
			if !ok {
				return 0, nil, false
			}
			var a uint64
			if isTypeDesc(t.name) {
				a, ok = l.typeAddr(def)
			} else {
				a, ok = l.itabAddr(def)
			}
			return a, nil, ok
		}
		if a, ok := l.hostAddr(t); ok {
			return a, nil, true
		}
	}
	return 0, nil, false
}

// place adds a symbol to the patch, and queues what it refers to.
func (l *linker) place(t target) (*placed, error) {
	if p, ok := l.placed[t.key]; ok {
		return p, nil
	}
	def, ok := l.s.def(t)
	if !ok {
		return nil, refuse("%s is defined nowhere the running build knows", t.name)
	}
	kind := def.o.symKind(def.i)
	sec, ok := kindSection(kind)
	if !ok {
		return nil, refuse("%s has a kind of symbol hot reload cannot carry (%d)", def.name, kind)
	}
	if def.o.symFlag2(def.i)&symFlag2PkgInit != 0 {
		return nil, refuse("a package's initialization is needed (%s)", def.name)
	}
	p := &placed{t: def, sec: sec, size: uint64(def.o.symSize(def.i))}
	l.placed[t.key] = p
	if def.key != t.key {
		l.placed[def.key] = p
	}
	l.order[sec] = append(l.order[sec], p)
	l.queue = append(l.queue, p)
	return p, nil
}

// close carries everything the placed symbols need and the program lacks.
func (l *linker) close() error {
	for len(l.queue) > 0 {
		p := l.queue[len(l.queue)-1]
		l.queue = l.queue[:len(l.queue)-1]
		o, i := p.t.o, p.t.i
		for j := range o.nreloc(i) {
			r := o.reloc(i, j)
			if isMarker(r.typ) || r.sym.pkg == pkgIdxInvalid {
				continue
			}
			t, err := l.s.resolve(o, r.sym)
			if err != nil {
				return err
			}
			// The methods of an itab are weak for the linker's dead code
			// elimination, which knows which get called. This does not.
			if r.weak && r.typ != rMETHODOFF && o.symFlag2(i)&symFlag2Itab == 0 {
				continue
			}
			if r.typ == rMETHODOFF && !l.isOwnPackageMethod(t) {
				continue
			}
			if _, _, ok := l.lookup(t); ok {
				continue
			}
			if _, err := l.place(t); err != nil {
				if r.weak {
					continue
				}
				return err
			}
		}
		for _, a := range o.auxs(i) {
			switch a.typ {
			case auxFuncdata:
				t, err := l.s.resolve(o, a.sym)
				if err != nil {
					return err
				}
				if t.o == nil {
					continue
				}
				if _, err := l.place(t); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// isOwnPackageMethod reports whether a method of a new type is in a package
// that was recompiled, whose methods must come along. Others stay out unless
// something else needs them: a method the program's linker dropped is one no
// conversion in it reached.
func (l *linker) isOwnPackageMethod(t target) bool {
	if t.o == nil {
		return false
	}
	return l.s.current[t.o.pkg] == t.o
}

func (l *linker) link(fns []target, writes []varWrite) (*hotpatch.Patch, error) {
	for _, f := range fns {
		if _, err := l.place(f); err != nil {
			return nil, err
		}
	}
	for _, w := range writes {
		o, i := w.t.o, w.t.i
		for j := range o.nreloc(i) {
			r := o.reloc(i, j)
			if isMarker(r.typ) {
				continue
			}
			t, err := l.s.resolve(o, r.sym)
			if err != nil {
				return nil, err
			}
			if _, _, ok := l.lookup(t); !ok {
				if _, err := l.place(t); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := l.close(); err != nil {
		return nil, err
	}
	return l.lay(fns, writes)
}

func rnd(x, a uint64) uint64 { return (x + a - 1) &^ (a - 1) }

func (l *linker) symAlign(p *placed) uint64 {
	if p.sec == secText {
		return l.align()
	}
	a := uint64(p.t.o.symAlign(p.t.i))
	if a == 0 {
		a = 8
	}
	return a
}

// module is the layout of a patch in memory, with the buffers being filled.
type module struct {
	text, ro, data, noptr              []byte
	textAt, roAt, dataAt, noptrAt      uint64
	bssAt, ebss, noptrbssAt, enoptrbss uint64
	end                                uint64
}

const pageSize = 1 << 14 // the largest of the platforms supported

func (l *linker) lay(fns []target, writes []varWrite) (*hotpatch.Patch, error) {
	s := l.s
	base := s.next
	if base == 0 {
		return nil, refuse("no program is attached")
	}
	// Text first.
	at := base
	for _, p := range l.order[secText] {
		at = rnd(at, l.align())
		p.addr = at
		at += p.size
	}
	maxpc := at
	textEnd := at

	tb := l.newTables(maxpc)
	// Rodata, with the tables after it.
	roAt := rnd(textEnd+tb.thunkSpace(l), pageSize)
	ro := roAt
	for _, p := range l.order[secRodata] {
		ro = rnd(ro, l.symAlign(p))
		p.addr = ro
		ro += p.size
	}
	// Slots for what code cannot reach.
	ro = rnd(ro, 8)
	for a := range l.far {
		l.far[a] = ro
		ro += 8
	}
	roSymsEnd := ro

	// The tables hold what the writers below compute, sized here.

	tables, err := tb.build(l, base, maxpc, roAt, roSymsEnd)
	if err != nil {
		return nil, err
	}
	roEnd := roSymsEnd + uint64(len(tables))

	dataAt := rnd(roEnd, pageSize)
	d := dataAt
	for _, p := range l.order[secData] {
		d = rnd(d, l.symAlign(p))
		p.addr = d
		d += p.size
	}
	edata := rnd(d, 8)
	n := edata
	for _, p := range l.order[secNoptrData] {
		n = rnd(n, l.symAlign(p))
		p.addr = n
		n += p.size
	}
	enoptr := rnd(n, 8)

	// bss
	b := enoptr
	for _, p := range l.order[secBSS] {
		b = rnd(b, l.symAlign(p))
		p.addr = b
		b += p.size
	}
	ebss := rnd(b, 8)
	nb := ebss
	for _, p := range l.order[secNoptrBSS] {
		nb = rnd(nb, l.symAlign(p))
		p.addr = nb
		nb += p.size
	}
	enb := rnd(nb, 8)

	// The garbage collector's programs and the moduledata go in memory it
	// does not scan, after the rest.
	gcdata, gcbss, err := l.gcPrograms(dataAt, edata, enoptr, ebss)
	if err != nil {
		return nil, err
	}
	gcAt := enb
	gcbssAt := gcAt + uint64(len(gcdata))
	mdAt := rnd(gcbssAt+uint64(len(gcbss)), 8)
	end := rnd(mdAt+mdSize, pageSize)
	if end > s.limit {
		return nil, refuse("the program ran out of memory set aside for hot reload")
	}

	// Now every address is known: fill and relocate.
	text := make([]byte, roAt-base)
	fillTrap(text, s.arch)
	rodata := make([]byte, roEnd-roAt)
	data := make([]byte, mdAt+mdSize-dataAt)

	put := func(p *placed, buf []byte, start uint64) error {
		copy(buf[p.addr-start:], p.t.o.symData(p.t.i))
		return l.relocate(p, buf[p.addr-start:], base, roAt, tb)
	}
	for _, p := range l.order[secText] {
		if err := put(p, text, base); err != nil {
			return nil, err
		}
	}
	for _, p := range l.order[secRodata] {
		if err := put(p, rodata, roAt); err != nil {
			return nil, err
		}
	}
	for _, sec := range []section{secData, secNoptrData} {
		for _, p := range l.order[sec] {
			if err := put(p, data, dataAt); err != nil {
				return nil, err
			}
		}
	}
	for a, slot := range l.far {
		binary.LittleEndian.PutUint64(rodata[slot-roAt:], a)
	}
	// Thunks live after the last function, outside what the function table covers.
	if err := tb.writeThunks(l, text, base); err != nil {
		return nil, err
	}
	// The tables, whose funcdata offsets needed rodata placed.
	tb.finish(l, tables, roSymsEnd, base, roAt)
	copy(rodata[roSymsEnd-roAt:], tables)

	copy(data[gcAt-dataAt:], gcdata)
	copy(data[gcbssAt-dataAt:], gcbss)

	mod := moduleLayout{
		text: base, etext: rnd(textEnd+tb.thunkSpace(l), 8), minpc: base, maxpc: maxpc,
		noptrdata: edata, enoptrdata: enoptr,
		data: dataAt, edata: edata,
		bss: enoptr, ebss: ebss,
		noptrbss: ebss, enoptrbss: enb,
		end: enb, gcdata: gcAt, gcbss: gcbssAt,
		types: roAt, etypes: roEnd,
		rodata: roAt, gofunc: roAt, epclntab: roEnd,
	}
	tb.writeModule(data[mdAt-dataAt:], &mod, roSymsEnd, l.s.host.pcHeader)

	lastModule, ok1 := s.host.syms["runtime.lastmoduledatap"]
	modulesInit, ok2 := s.host.syms["runtime.modulesinit"]
	if !ok1 || !ok2 {
		return nil, refuse("the program's runtime does not have what hot reload needs")
	}
	patch := &hotpatch.Patch{
		Base: base, Text: text, Rodata: rodata, Data: data,
		RodataAt: roAt, DataAt: dataAt, End: end,
		Module: mdAt, NextOffset: mdNext,
		LastModule:  lastModule + s.slide,
		ModulesInit: modulesInit + s.slide,
	}

	// The runtime must find the itabs the patch brings, or a conversion makes
	// another, which code comparing them by address would not take for it.
	if patch.ItabAdd, ok1 = symbolAt(s, "runtime.itabAdd"); !ok1 {
		return nil, refuse("the program's runtime does not have what hot reload needs")
	}
	if patch.ItabLock, ok1 = symbolAt(s, "runtime.itabLock"); !ok1 {
		return nil, refuse("the program's runtime does not have what hot reload needs")
	}
	if patch.Lock, ok1 = symbolAt(s, "runtime.lock2"); !ok1 {
		return nil, refuse("the program's runtime does not have what hot reload needs")
	}
	if patch.Unlock, ok1 = symbolAt(s, "runtime.unlock2"); !ok1 {
		return nil, refuse("the program's runtime does not have what hot reload needs")
	}
	for _, p := range l.order[secRodata] {
		if p.t.o.symFlag2(p.t.i)&symFlag2Itab != 0 {
			patch.Itabs = append(patch.Itabs, p.addr)
		}
	}

	// Jumps: every copy of each replaced function goes to the new code.
	for _, f := range fns {
		p := l.placed[f.key]
		if a, ok := l.hostAddrOriginal(f); ok {
			patch.Jumps = append(patch.Jumps, hotpatch.Jump{From: a, To: p.addr})
		}
		for _, a := range s.copies[f.key] {
			patch.Jumps = append(patch.Jumps, hotpatch.Jump{From: a, To: p.addr})
		}
		patch.Functions = append(patch.Functions, f.name)
	}
	// Writes: variables the edit gave another initial value.
	for _, w := range writes {
		a, ok := l.hostAddr(w.t)
		if !ok {
			continue
		}
		buf := make([]byte, w.t.o.symSize(w.t.i))
		copy(buf, w.t.o.symData(w.t.i))
		tmp := &placed{t: w.t, sec: secData, addr: a, size: uint64(len(buf))}
		if err := l.relocate(tmp, buf, base, roAt, tb); err != nil {
			return nil, err
		}
		patch.Writes = append(patch.Writes, hotpatch.Write{At: a, Data: buf})
	}

	// Commit: what later patches see.
	s.next = end
	for key, p := range l.placed {
		if p.t.abi == abiStatic || strings.HasPrefix(key, "#") {
			continue
		}
		s.latest[key] = p.addr
		if p.sec == secText {
			s.copies[key] = append(s.copies[key], p.addr)
		}
	}
	return patch, nil
}

func symbolAt(s *Session, name string) (uint64, bool) {
	a, ok := s.host.syms[name]
	return a + s.slide, ok
}

// hostAddrOriginal returns the function in the image, not a patch's.
func (l *linker) hostAddrOriginal(t target) (uint64, bool) {
	s := l.s
	if t.abi == 0 {
		if a, ok := s.host.syms[t.name+".abi0"]; ok {
			return a + s.slide, true
		}
	}
	a, ok := s.host.syms[t.name]
	if !ok {
		return 0, false
	}
	return a + s.slide, true
}

func fillTrap(b []byte, arch string) {
	switch arch {
	case "amd64":
		for i := range b {
			b[i] = 0xcc
		}
	case "arm64":
		for i := 0; i+4 <= len(b); i += 4 {
			binary.LittleEndian.PutUint32(b[i:], 0xd4200000) // brk #0
		}
	}
}

// addrOf returns the address a relocation of a placed symbol's target has.
// It returns ok false for a weak reference to something absent.
func (l *linker) addrOf(t target) (addr uint64, text, ok bool) {
	a, p, found := l.lookup(t)
	if p != nil {
		return p.addr, p.sec == secText, true
	}
	if found {
		h := l.s.host
		return a, a >= h.text+l.s.slide && a < h.etext+l.s.slide, true
	}
	return 0, false, false
}

// relocate applies the relocations of p, whose content is in b.
func (l *linker) relocate(p *placed, b []byte, textBase, roBase uint64, tb *tables) error {
	o, i := p.t.o, p.t.i
	for j := range o.nreloc(i) {
		r := o.reloc(i, j)
		if isMarker(r.typ) {
			continue
		}
		var addr uint64
		var isText, ok bool
		if r.sym.pkg != pkgIdxInvalid {
			t, err := l.s.resolve(o, r.sym)
			if err != nil {
				return err
			}
			addr, isText, ok = l.addrOf(t)
			if !ok && !r.weak && r.typ != rMETHODOFF {
				return refuse("%s refers to %s, which nothing has", p.t.name, t.name)
			}
		} else {
			ok = true
		}
		field := b[r.off:]
		pc := p.addr + uint64(r.off)
		v := int64(addr) + r.add
		switch r.typ {
		case rADDR:
			if !ok {
				v = 0
			}
			if r.siz == 8 {
				binary.LittleEndian.PutUint64(field, uint64(v))
			} else if r.siz == 4 {
				binary.LittleEndian.PutUint32(field, uint32(v))
			} else {
				return fmt.Errorf("hotlink: an address of %d bytes", r.siz)
			}
		case rADDROFF, rMETHODOFF:
			var off int64
			switch {
			case !ok:
				off = -1
			case isText:
				if addr < textBase {
					addr = tb.thunk(addr)
				}
				off = int64(addr) + r.add - int64(textBase)
			default:
				off = v - int64(roBase)
			}
			if off != int64(int32(off)) && off != int64(uint32(off)) {
				return fmt.Errorf("hotlink: an offset out of range in %s", p.t.name)
			}
			binary.LittleEndian.PutUint32(field, uint32(off))
		case rCALLARM64:
			d := v - int64(pc)
			if d >= 1<<27 || d < -(1<<27) {
				return refuse("a call from %s reaches too far", p.t.name)
			}
			ins := binary.LittleEndian.Uint32(field)
			binary.LittleEndian.PutUint32(field, ins&0xfc000000|uint32(d>>2)&0x03ffffff)
		case rADDRARM64, rARM64PCRELLDST8, rARM64PCRELLDST16, rARM64PCRELLDST32, rARM64PCRELLDST64:
			if slot, far := l.far[addr]; far && ok {
				// adrp, add takes the address; adrp, ldr of the same size takes
				// it from a slot.
				if r.typ != rADDRARM64 || r.add != 0 {
					return refuse("%s refers to %s in a way hot reload cannot redirect", p.t.name, "an itab of the program")
				}
				t := int64(slot) - int64(pc&^0xfff)
				o0, o1 := binary.LittleEndian.Uint32(field), binary.LittleEndian.Uint32(field[4:])
				if o1&0xff800000 != 0x91000000 {
					return refuse("%s refers to an itab of the program with an unknown instruction", p.t.name)
				}
				o0 |= uint32((t>>12)&3)<<29 | uint32((t>>14)&0x7ffff)<<5
				o1 = 0xf9400000 | o1&0x3ff | uint32((t&0xfff)>>3)<<10
				binary.LittleEndian.PutUint32(field, o0)
				binary.LittleEndian.PutUint32(field[4:], o1)
				continue
			}
			t := v - int64(pc&^0xfff)
			if t >= 1<<32 || t < -(1<<32) {
				return refuse("a reference from %s reaches too far", p.t.name)
			}
			o0, o1 := binary.LittleEndian.Uint32(field), binary.LittleEndian.Uint32(field[4:])
			o0 |= uint32((t>>12)&3)<<29 | uint32((t>>14)&0x7ffff)<<5
			lo := uint32(t & 0xfff)
			switch r.typ {
			case rARM64PCRELLDST16:
				lo >>= 1
			case rARM64PCRELLDST32:
				lo >>= 2
			case rARM64PCRELLDST64:
				lo >>= 3
			}
			o1 |= lo << 10
			binary.LittleEndian.PutUint32(field, o0)
			binary.LittleEndian.PutUint32(field[4:], o1)
		case rCALL, rPCREL:
			if slot, far := l.far[addr]; far && ok {
				// lea takes the address; mov of the same size takes it from a slot.
				if r.typ != rPCREL || r.off < 2 || b[r.off-2] != 0x8d {
					return refuse("%s refers to an itab of the program with an unknown instruction", p.t.name)
				}
				b[r.off-2] = 0x8b
				binary.LittleEndian.PutUint32(field, uint32(int64(slot)+r.add-int64(pc)-int64(r.siz)))
				continue
			}
			d := v - int64(pc) - int64(r.siz)
			if d != int64(int32(d)) {
				return refuse("a reference from %s reaches too far", p.t.name)
			}
			binary.LittleEndian.PutUint32(field, uint32(d))
		case rCONST:
			binary.LittleEndian.PutUint64(field, uint64(r.add))
		default:
			return refuse("%s has a relocation hot reload cannot apply (%d)", p.t.name, r.typ)
		}
	}
	return nil
}
