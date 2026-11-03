package hotlink

import (
	"encoding/binary"
	"sort"
	"strings"
)

// tables builds what the runtime reads to run the patch's functions
// (cmd/link/internal/ld/pcln.go writes the same for a whole program).
type tables struct {
	maxpc uint64

	funcs []*placed // by address

	names    []byte
	nameOff  map[string]uint32
	cu       []uint32
	cuOff    map[*object]uint32
	files    []byte
	fileOff  map[string]uint32
	pctab    []byte
	pcOff    map[string]uint32
	funcInfo map[*placed]funcInfo

	thunks   map[uint64]uint64
	thunkIdx []uint64

	inl map[*placed][]byte // generated inline trees, in order of funcs

	blob []byte // the whole, once built

	offPctab, offNames, offCu, offFiles, offFtab, offFindfunc, offHeader int
	nfunc                                                                int
	ftabLen                                                              int

	funcOff []uint32

	pendFuncdata   []pendFD
	offInl, bodyAt int
	base           uint32
	pad8Base       uint64
}

func (l *linker) newTables(maxpc uint64) *tables {
	return &tables{
		maxpc:    maxpc,
		nameOff:  map[string]uint32{},
		cuOff:    map[*object]uint32{},
		fileOff:  map[string]uint32{},
		pcOff:    map[string]uint32{},
		funcInfo: map[*placed]funcInfo{},
		thunks:   map[uint64]uint64{},
		inl:      map[*placed][]byte{},
	}
}

// thunkSize is the space a jump to anywhere takes.
const thunkSize = 16

// thunks are for method tables and the like, whose offsets cannot point
// behind the module. Space is set aside for one per relocation that may need
// it, which is few: the methods of new types the program's functions serve.
func (tb *tables) thunkSpace(l *linker) uint64 {
	n := uint64(0)
	for _, p := range l.order[secRodata] {
		o, i := p.t.o, p.t.i
		for j := range o.nreloc(i) {
			switch o.reloc(i, j).typ {
			case rMETHODOFF, rADDROFF:
				n++
			}
		}
	}
	return n*thunkSize + 8
}

func (tb *tables) thunk(addr uint64) uint64 {
	if a, ok := tb.thunks[addr]; ok {
		return a
	}
	a := rnd(tb.maxpc, 16) + uint64(len(tb.thunkIdx))*thunkSize
	tb.thunks[addr] = a
	tb.thunkIdx = append(tb.thunkIdx, addr)
	return a
}

func (tb *tables) writeThunks(l *linker, text []byte, base uint64) error {
	for _, target := range tb.thunkIdx {
		at := tb.thunks[target]
		b := text[at-base:]
		if l.s.arch == "arm64" {
			// ldr x16, #8; br x16; the address
			binary.LittleEndian.PutUint32(b, 0x58000050)
			binary.LittleEndian.PutUint32(b[4:], 0xd61f0200)
			binary.LittleEndian.PutUint64(b[8:], target)
		} else {
			// jmp [rip+0]; the address
			b[0], b[1] = 0xff, 0x25
			binary.LittleEndian.PutUint32(b[2:], 0)
			binary.LittleEndian.PutUint64(b[6:], target)
		}
	}
	return nil
}

func (tb *tables) name(s string) uint32 {
	if off, ok := tb.nameOff[s]; ok {
		return off
	}
	off := uint32(len(tb.names))
	tb.names = append(append(tb.names, s...), 0)
	tb.nameOff[s] = off
	return off
}

func (tb *tables) cuOffset(o *object) uint32 {
	if off, ok := tb.cuOff[o]; ok {
		return off
	}
	off := uint32(len(tb.cu))
	for i := range o.nfile() {
		f := o.file(i)
		fo, ok := tb.fileOff[f]
		if !ok {
			fo = uint32(len(tb.files))
			tb.files = append(append(tb.files, f...), 0)
			tb.fileOff[f] = fo
		}
		tb.cu = append(tb.cu, fo)
	}
	tb.cuOff[o] = off
	return off
}

func (tb *tables) pc(t target) uint32 {
	if t.o == nil {
		return 0
	}
	data := t.o.symData(t.i)
	if len(data) == 0 {
		return 0
	}
	if off, ok := tb.pcOff[string(data)]; ok {
		return off
	}
	off := uint32(len(tb.pctab))
	tb.pctab = append(tb.pctab, data...)
	tb.pcOff[string(data)] = off
	return off
}

const funcSize = 44 // runtime._func before its variable parts

// build sizes and fills the tables whose content needs no addresses of
// rodata; finish writes the rest. It returns the buffer.
func (tb *tables) build(l *linker, textBase, maxpc, roAt, roEnd uint64) ([]byte, error) {
	tb.funcs = append(tb.funcs, l.order[secText]...)
	sort.Slice(tb.funcs, func(i, j int) bool { return tb.funcs[i].addr < tb.funcs[j].addr })
	tb.nfunc = len(tb.funcs)

	tb.names = []byte{0}
	tb.files = []byte{0}
	tb.pctab = []byte{0} // an offset of 0 means no table
	ftab := make([]byte, tb.nfunc*8+4+8)
	var body []byte
	align := func(n int) {
		for len(body)%n != 0 {
			body = append(body, 0)
		}
	}
	tb.funcOff = make([]uint32, tb.nfunc)
	base := uint32(len(ftab))

	deferKey := symKey("runtime.deferreturn", 1)
	for fi, p := range tb.funcs {
		o, i := p.t.o, p.t.i
		var info funcInfo
		var pcsp, pcfile, pcline, pcinline uint32
		var pcdata []uint32
		var funcdata []uint32 // placeholders until addresses are known
		var fdSyms []target
		var hasInfo bool
		for _, a := range o.auxs(i) {
			t, err := l.s.resolve(o, a.sym)
			if err != nil {
				return nil, err
			}
			switch a.typ {
			case auxFuncInfo:
				if t.o != nil {
					info = parseFuncInfo(t.o.symData(t.i))
					hasInfo = true
				}
			case auxPcsp:
				pcsp = tb.pc(t)
			case auxPcfile:
				pcfile = tb.pc(t)
			case auxPcline:
				pcline = tb.pc(t)
			case auxPcinline:
				pcinline = tb.pc(t)
			case auxPcdata:
				pcdata = append(pcdata, tb.pc(t))
			case auxFuncdata:
				fdSyms = append(fdSyms, t)
			}
		}
		tb.funcInfo[p] = info
		nameOff := tb.name(p.t.name)
		var cu uint32 = ^uint32(0)
		if o.nfile() > 0 {
			cu = tb.cuOffset(o)
		}
		var deferreturn uint32
		for j := range o.nreloc(i) {
			r := o.reloc(i, j)
			if r.typ != rCALL && r.typ != rCALLARM64 {
				continue
			}
			if t, err := l.s.resolve(o, r.sym); err == nil && t.key == deferKey {
				deferreturn = uint32(r.off)
				if l.s.arch == "amd64" {
					deferreturn--
				}
				break
			}
		}

		const (
			pcdataInlTree   = 2 // abi.PCDATA_InlTreeIndex
			funcdataInlTree = 3 // abi.FUNCDATA_InlTree
		)
		npc := len(pcdata)
		if len(info.inl) > 0 && npc < pcdataInlTree+1 {
			npc = pcdataInlTree + 1
		}
		for len(pcdata) < npc {
			pcdata = append(pcdata, 0)
		}
		if len(info.inl) > 0 {
			pcdata[pcdataInlTree] = pcinline
		}
		nfd := len(fdSyms)
		if len(info.inl) > 0 && nfd < funcdataInlTree+1 {
			nfd = funcdataInlTree + 1
		}
		funcdata = make([]uint32, nfd)
		_ = hasInfo

		align(8)
		off := base + uint32(len(body))
		tb.funcOff[fi] = off

		hdr := make([]byte, funcSize)
		le := binary.LittleEndian
		le.PutUint32(hdr[0:], uint32(p.addr-textBase))
		le.PutUint32(hdr[4:], nameOff)
		le.PutUint32(hdr[8:], info.args)
		le.PutUint32(hdr[12:], deferreturn)
		le.PutUint32(hdr[16:], pcsp)
		le.PutUint32(hdr[20:], pcfile)
		le.PutUint32(hdr[24:], pcline)
		le.PutUint32(hdr[28:], uint32(npc))
		le.PutUint32(hdr[32:], cu)
		le.PutUint32(hdr[36:], uint32(info.startLine))
		hdr[40] = info.funcID
		hdr[41] = info.flag
		hdr[43] = uint8(nfd)
		body = append(body, hdr...)
		for _, v := range pcdata {
			body = le.AppendUint32(body, v)
		}
		fdAt := len(body)
		for range funcdata {
			body = le.AppendUint32(body, ^uint32(0))
		}
		tb.pendFuncdata = append(tb.pendFuncdata, pendFD{at: fdAt, syms: fdSyms, inl: info.inl, o: o, nfd: nfd})
		le.PutUint32(ftab[fi*8:], uint32(p.addr-textBase))
		le.PutUint32(ftab[fi*8+4:], off)
		_ = roEnd
		_ = roAt
	}
	binary.LittleEndian.PutUint32(ftab[tb.nfunc*8:], uint32(maxpc-textBase))
	tb.ftabLen = len(ftab) + len(body)

	// Inline trees, whose funcs' names go in the name table first.
	inlBlob := []byte{}
	for k := range tb.pendFuncdata {
		pf := &tb.pendFuncdata[k]
		if len(pf.inl) == 0 {
			continue
		}
		for len(inlBlob)%4 != 0 {
			inlBlob = append(inlBlob, 0)
		}
		pf.inlAt = len(inlBlob)
		for _, n := range pf.inl {
			t, err := l.s.resolve(pf.o, n.fn)
			if err != nil {
				return nil, err
			}
			var funcID uint8
			var startLine int32
			if def, ok := l.s.def(t); ok {
				for _, a := range def.o.auxs(def.i) {
					if a.typ == auxFuncInfo {
						if ft, err := l.s.resolve(def.o, a.sym); err == nil && ft.o != nil {
							fi := parseFuncInfo(ft.o.symData(ft.i))
							funcID, startLine = fi.funcID, fi.startLine
						}
					}
				}
			}
			e := make([]byte, 16)
			e[0] = funcID
			binary.LittleEndian.PutUint32(e[4:], tb.name(t.name))
			binary.LittleEndian.PutUint32(e[8:], uint32(n.parentPC))
			binary.LittleEndian.PutUint32(e[12:], uint32(startLine))
			inlBlob = append(inlBlob, e...)
		}
	}

	// findfunctab: 4096 bytes a bucket, 16 sub-buckets each.
	const (
		bucket  = 4096
		nsub    = 16
		subsize = bucket / nsub
		noIdx   = 0x7fffffff
		recSize = 4 + nsub
	)
	span := maxpc - textBase
	n := int((span + subsize - 1) / subsize)
	nb := int((span + bucket - 1) / bucket)
	indexes := make([]int32, n)
	for i := range indexes {
		indexes[i] = noIdx
	}
	for idx, p := range tb.funcs {
		start := p.addr - textBase
		end := span
		if idx+1 < len(tb.funcs) {
			end = tb.funcs[idx+1].addr - textBase
		}
		for q := start; q < end; q += subsize {
			if i := int(q / subsize); indexes[i] > int32(idx) {
				indexes[i] = int32(idx)
			}
		}
		if i := int((end - 1) / subsize); indexes[i] > int32(idx) {
			indexes[i] = int32(idx)
		}
	}
	ff := make([]byte, nb*recSize)
	for i := range nb {
		b := indexes[i*nsub]
		binary.LittleEndian.PutUint32(ff[i*recSize:], uint32(b))
		for j := 0; j < nsub && i*nsub+j < n; j++ {
			d := indexes[i*nsub+j] - b
			if d >= 256 {
				return nil, refuse("too many functions in a page for hot reload's table")
			}
			ff[i*recSize+4+j] = uint8(d)
		}
	}

	// Assemble: header, names, cu, files, pctab, ftab+funcs, findfunc, inl trees.
	var blob []byte
	// The buffer starts where rodata symbols end, so alignment is relative to it.
	for (roEnd+uint64(len(blob)))%8 != 0 {
		blob = append(blob, 0)
	}
	tb.offHeader = len(blob)
	blob = append(blob, make([]byte, 72)...)
	tb.offNames = len(blob)
	blob = append(blob, tb.names...)
	for (roEnd+uint64(len(blob)))%4 != 0 {
		blob = append(blob, 0)
	}
	tb.offCu = len(blob)
	for _, v := range tb.cu {
		blob = binary.LittleEndian.AppendUint32(blob, v)
	}
	tb.offFiles = len(blob)
	blob = append(blob, tb.files...)
	tb.offPctab = len(blob)
	blob = append(blob, tb.pctab...)
	for (roEnd+uint64(len(blob)))%8 != 0 {
		blob = append(blob, 0)
	}
	tb.offFtab = len(blob)
	blob = append(blob, ftab[:tb.nfunc*8+4]...)
	for len(blob)-tb.offFtab < len(ftab) {
		blob = append(blob, 0)
	}
	blob = append(blob, body...)
	for (roEnd+uint64(len(blob)))%4 != 0 {
		blob = append(blob, 0)
	}
	tb.offFindfunc = len(blob)
	blob = append(blob, ff...)
	for (roEnd+uint64(len(blob)))%4 != 0 {
		blob = append(blob, 0)
	}
	tb.offInl = len(blob)
	blob = append(blob, inlBlob...)
	tb.bodyAt = tb.offFtab + len(ftab)
	tb.base = base
	return blob, nil
}

type pendFD struct {
	at    int
	syms  []target
	inl   []inlNode
	inlAt int
	o     *object
	nfd   int
}

// finish writes the offsets of funcdata, now that rodata is placed.
func (tb *tables) finish(l *linker, blob []byte, roEnd, textBase, roAt uint64) {
	le := binary.LittleEndian
	const funcdataInlTree = 3
	for _, pf := range tb.pendFuncdata {
		at := tb.bodyAt + pf.at
		for j, t := range pf.syms {
			if t.o == nil || t.o.symSize(t.i) == 0 {
				continue
			}
			p := l.placed[t.key]
			if p == nil {
				continue
			}
			le.PutUint32(blob[at+j*4:], uint32(p.addr-roAt))
		}
		if len(pf.inl) > 0 {
			le.PutUint32(blob[at+funcdataInlTree*4:], uint32(roEnd+uint64(tb.offInl+pf.inlAt)-roAt))
		}
	}
	_ = textBase
}

// moduleLayout is what writeModule needs of a patch's addresses.
type moduleLayout struct {
	text, etext, minpc, maxpc uint64
	noptrdata, enoptrdata     uint64
	data, edata               uint64
	bss, ebss                 uint64
	noptrbss, enoptrbss       uint64
	end, gcdata, gcbss        uint64
	types, etypes             uint64
	rodata, gofunc, epclntab  uint64
}

// writeModule writes the patch's runtime.moduledata.
func (tb *tables) writeModule(b []byte, m *moduleLayout, roEnd uint64, magic []byte) {
	le := binary.LittleEndian
	u64 := func(off int, v uint64) { le.PutUint64(b[off:], v) }
	slice := func(off int, at uint64, n int) {
		u64(off, at)
		u64(off+8, uint64(n))
		u64(off+16, uint64(n))
	}
	addr := func(off int) uint64 { return roEnd + uint64(off) }

	u64(mdPcHeader, addr(tb.offHeader))
	slice(mdFuncnametab, addr(tb.offNames), tb.offCu-tb.offNames)
	slice(mdCutab, addr(tb.offCu), len(tb.cu))
	slice(mdFiletab, addr(tb.offFiles), tb.offPctab-tb.offFiles)
	slice(mdPctab, addr(tb.offPctab), tb.offFtab-tb.offPctab)
	slice(mdPclntable, addr(tb.offFtab), tb.offFindfunc-tb.offFtab)
	slice(mdFtab, addr(tb.offFtab), tb.nfunc+1)
	u64(mdFindfunctab, addr(tb.offFindfunc))
	u64(mdMinpc, m.minpc)
	u64(mdMaxpc, m.maxpc)
	u64(mdText, m.text)
	u64(mdEtext, m.etext)
	u64(mdNoptrdata, m.noptrdata)
	u64(mdEnoptrdata, m.enoptrdata)
	u64(mdData, m.data)
	u64(mdEdata, m.edata)
	u64(mdBss, m.bss)
	u64(mdEbss, m.ebss)
	u64(mdNoptrbss, m.noptrbss)
	u64(mdEnoptrbss, m.enoptrbss)
	u64(mdEnd, m.end)
	u64(mdGcdata, m.gcdata)
	u64(mdGcbss, m.gcbss)
	u64(mdTypes, m.types)
	u64(mdEtypes, m.etypes)
	u64(mdRodata, m.rodata)
	u64(mdGofunc, m.gofunc)
	u64(mdEpclntab, m.epclntab)
	_ = magic
}

// gcPrograms returns the runtime's programs for the pointer masks of data and
// bss: a literal of every bit, which is what progToPointerMask expands.
func (l *linker) gcPrograms(dataAt, edata, enoptr, ebss uint64) (gcdata, gcbss []byte, err error) {
	dataMask := make([]bool, (edata-dataAt)/8)
	for _, p := range l.order[secData] {
		l.markPointers(dataMask, p, dataAt)
	}
	bssMask := make([]bool, (ebss-enoptr)/8)
	for _, p := range l.order[secBSS] {
		l.markPointers(bssMask, p, enoptr)
	}
	return gcProgram(dataMask), gcProgram(bssMask), nil
}

// markPointers marks the words of p that may hold pointers, by its type; a
// symbol without one is scanned whole, which static data is safe to be.
func (l *linker) markPointers(mask []bool, p *placed, base uint64) {
	first := (p.addr - base) / 8
	words := rnd(p.size, 8) / 8
	bits := l.typeBits(p)
	for w := uint64(0); w < words && first+w < uint64(len(mask)); w++ {
		if bits == nil || (w < uint64(len(bits))*8 && bits[w/8]&(1<<(w%8)) != 0) {
			mask[first+w] = true
		}
	}
}

// typeBits returns the pointer mask of the type a variable was declared with.
func (l *linker) typeBits(p *placed) []byte {
	o, i := p.t.o, p.t.i
	for _, a := range o.auxs(i) {
		if a.typ != auxGotype {
			continue
		}
		t, err := l.s.resolve(o, a.sym)
		if err != nil {
			return nil
		}
		t, ok := l.s.def(t)
		if !ok {
			return nil
		}
		d := t.o.symData(t.i)
		if len(d) < typeHeader || d[typeTFlag]&tflagGCMaskOnDemand != 0 {
			return nil
		}
		for j := range t.o.nreloc(t.i) {
			r := t.o.reloc(t.i, j)
			if r.off != typeGCData {
				continue
			}
			g, err := l.s.resolve(t.o, r.sym)
			if err != nil {
				return nil
			}
			g, ok := l.s.def(g)
			if !ok || !strings.HasPrefix(g.name, "runtime.gcbits.") {
				return nil
			}
			return g.o.symData(g.i)
		}
		// No pointers at all: an empty mask, not a missing one.
		if binary.LittleEndian.Uint64(d[typePtrBytes:]) == 0 {
			return []byte{}
		}
	}
	return nil
}

// gcProgram encodes a mask as literals (runtime/mbitmap.go, runGCProg).
func gcProgram(mask []bool) []byte {
	var out []byte
	for at := 0; at < len(mask); at += 120 {
		n := min(120, len(mask)-at)
		out = append(out, byte(n))
		for i := 0; i < n; i += 8 {
			var b byte
			for k := 0; k < 8 && i+k < n; k++ {
				if mask[at+i+k] {
					b |= 1 << k
				}
			}
			out = append(out, b)
		}
	}
	return append(out, 0)
}
