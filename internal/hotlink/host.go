package hotlink

import (
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// Field offsets of runtime.moduledata, which module.go writes too. They are
// checked against the host's symbols as it is read, so a runtime that moved
// them is refused rather than misread.
const (
	mdPcHeader    = 0
	mdFuncnametab = 8
	mdCutab       = 32
	mdFiletab     = 56
	mdPctab       = 80
	mdPclntable   = 104
	mdFtab        = 128
	mdFindfunctab = 152
	mdMinpc       = 160
	mdMaxpc       = 168
	mdText        = 176
	mdEtext       = 184
	mdNoptrdata   = 192
	mdEnoptrdata  = 200
	mdData        = 208
	mdEdata       = 216
	mdBss         = 224
	mdEbss        = 232
	mdNoptrbss    = 240
	mdEnoptrbss   = 248
	mdEnd         = 272
	mdGcdata      = 280
	mdGcbss       = 288
	mdTypes       = 296
	mdTypedesclen = 304
	mdEtypes      = 312
	mdItaboffset  = 320
	mdItabsize    = 328
	mdRodata      = 336
	mdGofunc      = 344
	mdEpclntab    = 352
	mdNext        = 560
	mdSize        = 568
)

// host is the executable a running program started from.
type host struct {
	syms  map[string]uint64 // by name, at link time
	types map[typeKey]uint64
	itabs map[[2]uint64]uint64

	text, etext uint64 // the program's code
	pcHeader    []byte

	typesAt, etypes uint64 // where type descriptors lie
	seen            map[uint64]bool

	segs []segment
}

type segment struct {
	addr uint64
	data []byte
}

// typeKey names a type descriptor: its hash, and its string to tell apart
// the rare types whose hashes collide.
type typeKey struct {
	hash uint32
	str  string
}

func (h *host) read(addr uint64, n int) []byte {
	for _, s := range h.segs {
		if addr >= s.addr && addr+uint64(n) <= s.addr+uint64(len(s.data)) {
			return s.data[addr-s.addr:][:n]
		}
	}
	return nil
}

func (h *host) u64(addr uint64) uint64 {
	b := h.read(addr, 8)
	if b == nil {
		return 0
	}
	return binary.LittleEndian.Uint64(b)
}

// openHost reads the executable's symbols, types and itabs.
func openHost(path string) (*host, error) {
	h := &host{syms: map[string]uint64{}, types: map[typeKey]uint64{}, itabs: map[[2]uint64]uint64{}}
	var err error
	if f, e := macho.Open(path); e == nil {
		defer f.Close()
		err = h.readMacho(f)
	} else if f, e := elf.Open(path); e == nil {
		defer f.Close()
		err = h.readELF(f)
	} else if f, e := pe.Open(path); e == nil {
		defer f.Close()
		err = h.readPE(f)
	} else {
		return nil, fmt.Errorf("%s: an executable format hot reload cannot read", path)
	}
	if err != nil {
		return nil, err
	}
	return h, h.readModule()
}

func (h *host) readMacho(f *macho.File) error {
	if f.Symtab == nil {
		return errors.New("the executable has no symbol table")
	}
	for _, l := range f.Loads {
		s, ok := l.(*macho.Segment)
		if !ok || s.Name == "__PAGEZERO" || s.Name == "__LINKEDIT" || s.Filesz == 0 {
			continue
		}
		data, err := s.Data()
		if err != nil {
			return err
		}
		h.segs = append(h.segs, segment{s.Addr, data})
	}
	for _, s := range f.Symtab.Syms {
		if s.Sect != 0 && s.Name != "" {
			h.syms[s.Name] = s.Value
		}
	}
	return nil
}

func (h *host) readELF(f *elf.File) error {
	syms, err := f.Symbols()
	if err != nil {
		return fmt.Errorf("the executable has no symbol table: %w", err)
	}
	for _, p := range f.Progs {
		if p.Type != elf.PT_LOAD {
			continue
		}
		data := make([]byte, p.Filesz)
		if _, err := p.ReadAt(data, 0); err != nil {
			return err
		}
		h.segs = append(h.segs, segment{p.Vaddr, data})
	}
	for _, s := range syms {
		if s.Section != elf.SHN_UNDEF && s.Name != "" {
			h.syms[s.Name] = s.Value
		}
	}
	return nil
}

func (h *host) readPE(f *pe.File) error {
	if len(f.Symbols) == 0 {
		return errors.New("the executable has no symbol table")
	}
	var base uint64
	switch o := f.OptionalHeader.(type) {
	case *pe.OptionalHeader64:
		base = o.ImageBase
	case *pe.OptionalHeader32:
		base = uint64(o.ImageBase)
	}
	for _, s := range f.Sections {
		if s.Characteristics&pe.IMAGE_SCN_CNT_UNINITIALIZED_DATA != 0 || s.Size == 0 {
			continue
		}
		data, err := s.Data()
		if err != nil {
			return err
		}
		h.segs = append(h.segs, segment{base + uint64(s.VirtualAddress), data})
	}
	for _, s := range f.Symbols {
		if s.SectionNumber <= 0 || int(s.SectionNumber) > len(f.Sections) || s.Name == "" {
			continue
		}
		sect := f.Sections[s.SectionNumber-1]
		h.syms[s.Name] = base + uint64(sect.VirtualAddress) + uint64(s.Value)
	}
	return nil
}

// readModule reads the runtime's description of the program, and through it
// every type descriptor and itab of the program, which symbol tables do not
// list.
func (h *host) readModule() error {
	md, ok := h.syms["runtime.firstmoduledata"]
	if !ok {
		return errors.New("the executable has no runtime.firstmoduledata")
	}
	h.text, h.etext = h.u64(md+mdText), h.u64(md+mdEtext)
	types, etypes := h.u64(md+mdTypes), h.u64(md+mdEtypes)
	if h.text != h.syms["runtime.text"] || h.etext != h.syms["runtime.etext"] || types != h.syms["runtime.types"] || etypes != h.syms["runtime.etypes"] || h.u64(md+mdNext) != 0 {
		return errors.New("a runtime.moduledata of another layout")
	}
	h.pcHeader = h.read(h.u64(md+mdPcHeader), 8)
	if h.pcHeader == nil {
		return errors.New("the executable has no function table")
	}
	h.typesAt, h.etypes = types, etypes
	// The descriptors with a typelink lie one after the other, and those
	// without among other data; but what is alive is reachable, so they are
	// found by what they are referred to from.
	var queue []uint64
	end := types + h.u64(md+mdTypedesclen)
	for td := types + 8; td < end; {
		td = (td + 7) &^ 7
		b := h.read(td, typeHeader)
		if b == nil {
			return fmt.Errorf("a type descriptor out of range at %#x", td)
		}
		size, err := h.descriptorSize(td, b)
		if err != nil {
			return err
		}
		queue = append(queue, td)
		td += uint64(size)
	}
	for p, e := types+h.u64(md+mdItaboffset), types+h.u64(md+mdItaboffset)+h.u64(md+mdItabsize); p < e; {
		inter, typ := h.u64(p), h.u64(p+8)
		h.itabs[[2]uint64{inter, typ}] = p
		queue = append(queue, inter, typ)
		b := h.read(inter, 80)
		if b == nil {
			return fmt.Errorf("an itab out of range at %#x", p)
		}
		n := max(binary.LittleEndian.Uint64(b[64:]), 1)
		p += 24 + 8*n
	}
	h.seen = map[uint64]bool{}
	h.walkTypes(queue)
	return nil
}

// walkTypes adds to what it knows the descriptors given, and all they refer to.
func (h *host) walkTypes(queue []uint64) {
	le := binary.LittleEndian
	for len(queue) > 0 {
		td := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if td < h.typesAt || td >= h.etypes || h.seen[td] {
			continue
		}
		h.seen[td] = true
		t := h.read(td, typeHeader)
		if t == nil {
			continue
		}
		h.types[typeKey{le.Uint32(t[typeHash:]), h.typeString(h.typesAt, t)}] = td
		k := t[typeKind] & 0x1f
		full := h.read(td, 80)
		if full == nil {
			continue
		}
		base, _, err := kindSizes(k, full)
		if err != nil {
			continue
		}
		off := func(rel int32) {
			if rel > 0 {
				queue = append(queue, h.typesAt+uint64(rel))
			}
		}
		off(int32(le.Uint32(t[44:]))) // PtrToThis
		switch k {
		case kArray, kChan, kPointer, kSlice:
			queue = append(queue, le.Uint64(full[48:]))
		case kMap:
			queue = append(queue, le.Uint64(full[48:]), le.Uint64(full[56:]), le.Uint64(full[64:]))
		case kStruct:
			fields, n := le.Uint64(full[56:]), le.Uint64(full[64:])
			for i := range n {
				queue = append(queue, h.u64(fields+i*24+8))
			}
		case kInterface:
			methods, n := le.Uint64(full[56:]), le.Uint64(full[64:])
			for i := range n {
				if b := h.read(methods+i*8+4, 4); b != nil {
					off(int32(le.Uint32(b)))
				}
			}
		case kFunc:
			params := td + uint64(base)
			if t[typeTFlag]&tflagUncommon != 0 {
				params += uncommonSize
			}
			in := uint64(le.Uint16(full[48:]))
			out := uint64(le.Uint16(full[50:]) &^ (1 << 15))
			for i := range in + out {
				queue = append(queue, h.u64(params+i*8))
			}
		}
		if t[typeTFlag]&tflagUncommon != 0 {
			u := h.read(td+uint64(base), uncommonSize)
			if u == nil {
				continue
			}
			moff, count := uint64(le.Uint32(u[8:])), uint64(le.Uint16(u[4:]))
			at := td + uint64(base) + moff
			for i := range count {
				if b := h.read(at+i*methodSize+4, 4); b != nil {
					off(int32(le.Uint32(b)))
				}
			}
		}
	}
}

// The layout of abi.Type and of what follows it.
const (
	typeHeader   = 48
	typeSize     = 0
	typePtrBytes = 8
	typeHash     = 16
	typeTFlag    = 20
	typeKind     = 23
	typeGCData   = 32
	typeStr      = 40

	tflagUncommon       = 1
	tflagExtraStar      = 2
	tflagGCMaskOnDemand = 16

	uncommonSize = 16
	methodSize   = 16
)

// Kinds of types (abi.Kind).
const (
	kBool = iota + 1
	kInt
	kInt8
	kInt16
	kInt32
	kInt64
	kUint
	kUint8
	kUint16
	kUint32
	kUint64
	kUintptr
	kFloat32
	kFloat64
	kComplex64
	kComplex128
	kArray
	kChan
	kFunc
	kInterface
	kMap
	kPointer
	kSlice
	kString
	kStruct
	kUnsafePointer
)

// kindSizes returns the size of a descriptor of kind k before its uncommon
// type, given the bytes of the descriptor, and what follows the uncommon type.
func kindSizes(k byte, t []byte) (base, add int, err error) {
	switch k {
	case kArray:
		return 72, 0, nil
	case kChan:
		return 64, 0, nil
	case kFunc:
		in := int(binary.LittleEndian.Uint16(t[48:]))
		out := int(binary.LittleEndian.Uint16(t[50:]) &^ (1 << 15))
		return 56, (in + out) * 8, nil
	case kInterface:
		return 80, int(binary.LittleEndian.Uint64(t[64:])) * 8, nil
	case kMap:
		return 136, 0, nil
	case kPointer, kSlice:
		return 56, 0, nil
	case kStruct:
		return 80, int(binary.LittleEndian.Uint64(t[64:])) * 24, nil
	case kBool, kInt, kInt8, kInt16, kInt32, kInt64, kUint, kUint8, kUint16, kUint32, kUint64, kUintptr,
		kFloat32, kFloat64, kComplex64, kComplex128, kString, kUnsafePointer:
		return typeHeader, 0, nil
	}
	return 0, 0, fmt.Errorf("a type descriptor of unknown kind %d", k)
}

func (h *host) descriptorSize(td uint64, hdr []byte) (int, error) {
	k := hdr[typeKind] & 0x1f
	full := h.read(td, 136)
	if full == nil {
		full = h.read(td, 80)
	}
	if full == nil {
		full = hdr
	}
	if len(full) < 80 && (k == kInterface || k == kStruct || k == kFunc) {
		return 0, fmt.Errorf("a type descriptor cut short at %#x", td)
	}
	base, add, err := kindSizes(k, full)
	if err != nil {
		return 0, err
	}
	size := base + add
	if hdr[typeTFlag]&tflagUncommon != 0 {
		u := h.read(td+uint64(base), uncommonSize)
		if u == nil {
			return 0, fmt.Errorf("an uncommon type out of range at %#x", td)
		}
		size += uncommonSize + int(binary.LittleEndian.Uint16(u[4:]))*methodSize
	}
	return size, nil
}

func (h *host) typeString(types uint64, t []byte) string {
	off := int32(binary.LittleEndian.Uint32(t[typeStr:]))
	if off == 0 {
		return ""
	}
	at := uint64(int64(types) + int64(off))
	head := h.read(at, 3)
	if head == nil {
		return ""
	}
	n, w := binary.Uvarint(head[1:])
	s := h.read(at+1+uint64(w), int(n))
	if s == nil {
		return ""
	}
	return strings.TrimPrefix(string(s), starIf(t[typeTFlag]))
}

func starIf(tflag byte) string {
	if tflag&tflagExtraStar != 0 {
		return "*"
	}
	return ""
}
