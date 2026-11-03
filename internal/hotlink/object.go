package hotlink

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unsafe"
)

// The object file format of the Go toolchain, after cmd/internal/goobj, which
// programs outside cmd cannot import. checkToolchain says which Go this reads.
const objMagic = "\x00go120ld"

const (
	pkgIdxNone     = (1<<31 - 1) - iota // non-package symbols
	pkgIdxHashed64                      // short content-addressed symbols
	pkgIdxHashed                        // content-addressed symbols
	pkgIdxBuiltin                       // the runtime's predefined symbols
	pkgIdxSelf                          // symbols of the object's own package
	pkgIdxInvalid  = 0
)

const (
	blkAutolib = iota
	blkPkgIdx
	blkFile
	blkSymdef
	blkHashed64def
	blkHasheddef
	blkNonpkgdef
	blkNonpkgref
	blkRefFlags
	blkHash64
	blkHash
	blkRelocIdx
	blkAuxIdx
	blkDataIdx
	blkReloc
	blkAux
	blkData
	blkRefName
	blkEnd
	nBlk
)

const (
	stringRefSize = 8
	symSize       = stringRefSize + 2 + 1 + 1 + 1 + 4 + 4
	relocSize     = 4 + 1 + 2 + 8 + 8
	auxSize       = 1 + 8
	hash64Size    = 8
	hashSize      = 16

	abiStatic = ^uint16(0) // a symbol local to its object
)

const (
	symFlagDupok = 1 << 0
	symFlagLeaf  = 1 << 3

	symFlag2Itab    = 1 << 1
	symFlag2PkgInit = 1 << 3
)

const (
	auxGotype = iota
	auxFuncInfo
	auxFuncdata
	auxDwarfInfo
	auxDwarfLoc
	auxDwarfRanges
	auxDwarfLines
	auxPcsp
	auxPcfile
	auxPcline
	auxPcinline
	auxPcdata
)

// The kinds of symbols (cmd/internal/objabi.SymKind).
const (
	kindText          = 1
	kindTextFIPS      = 2
	kindRodata        = 3
	kindRodataFIPS    = 4
	kindNoptrData     = 5
	kindNoptrDataFIPS = 6
	kindData          = 7
	kindDataFIPS      = 8
	kindBSS           = 9
	kindNoptrBSS      = 10
	kindTLSBSS        = 11
)

// symRef is a reference within an object.
type symRef struct{ pkg, idx uint32 }

// object is one Go object file: a package, or an assembly file of one.
type object struct {
	pkg string // its import path
	b   []byte

	offs [nBlk]uint32

	ndef, nhashed64, nhashed, nnonpkgdef, nnonpkgref uint32
}

// readArchive reads the Go objects of a package's archive.
func readArchive(pkg, file string) ([]*object, error) {
	b, err := mapFile(file)
	if err != nil {
		return nil, err
	}
	return parseArchive(pkg, b)
}

func parseArchive(pkg string, b []byte) ([]*object, error) {
	const magic = "!<arch>\n"
	if !bytes.HasPrefix(b, []byte(magic)) {
		return nil, fmt.Errorf("%s: not an archive", pkg)
	}
	var objs []*object
	for p := b[len(magic):]; len(p) >= 60; {
		name := strings.TrimRight(string(p[:16]), " /")
		size, err := strconv.Atoi(strings.TrimSpace(string(p[48:58])))
		if err != nil || size < 0 || 60+size > len(p) {
			return nil, fmt.Errorf("%s: a malformed archive", pkg)
		}
		body := p[60 : 60+size]
		if name != "__.PKGDEF" && bytes.HasPrefix(body, []byte("go object ")) {
			i := bytes.Index(body, []byte("\n!\n"))
			if i < 0 {
				return nil, fmt.Errorf("%s: %s has no object", pkg, name)
			}
			o, err := parseObject(pkg, body[i+3:])
			if err != nil {
				return nil, fmt.Errorf("%s: %s: %w", pkg, name, err)
			}
			objs = append(objs, o)
		}
		p = p[60+size+size&1:]
	}
	if len(objs) == 0 {
		return nil, fmt.Errorf("%s: an archive without objects", pkg)
	}
	return objs, nil
}

func parseObject(pkg string, b []byte) (*object, error) {
	if !bytes.HasPrefix(b, []byte(objMagic)) {
		return nil, errors.New("the object format of another Go version")
	}
	o := &object{pkg: pkg, b: b}
	off := len(objMagic) + 8 + 4 // the fingerprint and the flags
	for i := range o.offs {
		o.offs[i] = binary.LittleEndian.Uint32(b[off:])
		off += 4
	}
	count := func(blk int) uint32 { return (o.offs[blk+1] - o.offs[blk]) / symSize }
	o.ndef, o.nhashed64, o.nhashed = count(blkSymdef), count(blkHashed64def), count(blkHasheddef)
	o.nnonpkgdef, o.nnonpkgref = count(blkNonpkgdef), count(blkNonpkgref)
	return o, nil
}

func (o *object) u32(off uint32) uint32 { return binary.LittleEndian.Uint32(o.b[off:]) }

// stringAt returns a string of the object, without a copy: the many names of a
// program stay in the file's pages, which the system can drop.
func (o *object) stringAt(off uint32) string {
	n, at := o.u32(off), o.u32(off+4)
	if n == 0 {
		return ""
	}
	return unsafe.String(&o.b[at], n)
}

// ndefined is the number of symbols the object defines, which come first;
// the non-package references it only names follow them.
func (o *object) ndefined() uint32 { return o.ndef + o.nhashed64 + o.nhashed + o.nnonpkgdef }

func (o *object) symOff(i uint32) uint32 { return o.offs[blkSymdef] + i*symSize }

func (o *object) symName(i uint32) string { return o.stringAt(o.symOff(i)) }
func (o *object) symABI(i uint32) uint16 {
	return binary.LittleEndian.Uint16(o.b[o.symOff(i)+8:])
}
func (o *object) symKind(i uint32) uint8  { return o.b[o.symOff(i)+10] }
func (o *object) symFlag(i uint32) uint8  { return o.b[o.symOff(i)+11] }
func (o *object) symFlag2(i uint32) uint8 { return o.b[o.symOff(i)+12] }
func (o *object) symSize(i uint32) uint32 { return o.u32(o.symOff(i) + 13) }
func (o *object) symAlign(i uint32) uint32 {
	return o.u32(o.symOff(i) + 17)
}

// symHash is the content hash of a content-addressed symbol, if i is one.
func (o *object) symHash(i uint32) string {
	switch {
	case i < o.ndef:
		return ""
	case i < o.ndef+o.nhashed64:
		return string(o.b[o.offs[blkHash64]+(i-o.ndef)*hash64Size:][:hash64Size])
	case i < o.ndef+o.nhashed64+o.nhashed:
		return string(o.b[o.offs[blkHash]+(i-o.ndef-o.nhashed64)*hashSize:][:hashSize])
	}
	return ""
}

func (o *object) symData(i uint32) []byte {
	idx := o.offs[blkDataIdx] + i*4
	base := o.offs[blkData]
	return o.b[base+o.u32(idx) : base+o.u32(idx+4)]
}

// reloc is a relocation of a symbol.
type reloc struct {
	off  int32
	siz  uint8
	typ  uint16
	add  int64
	sym  symRef
	weak bool
}

const relocWeak = 0x8000

func (o *object) nreloc(i uint32) int {
	idx := o.offs[blkRelocIdx] + i*4
	return int(o.u32(idx+4) - o.u32(idx))
}

func (o *object) reloc(i uint32, j int) reloc {
	idx := o.offs[blkRelocIdx] + i*4
	at := o.offs[blkReloc] + (o.u32(idx)+uint32(j))*relocSize
	b := o.b[at:]
	typ := binary.LittleEndian.Uint16(b[5:])
	return reloc{
		off:  int32(binary.LittleEndian.Uint32(b)),
		siz:  b[4],
		typ:  typ &^ relocWeak,
		add:  int64(binary.LittleEndian.Uint64(b[7:])),
		sym:  symRef{binary.LittleEndian.Uint32(b[15:]), binary.LittleEndian.Uint32(b[19:])},
		weak: typ&relocWeak != 0,
	}
}

// aux is a symbol attached to another, as a function's metadata.
type aux struct {
	typ uint8
	sym symRef
}

func (o *object) auxs(i uint32) []aux {
	idx := o.offs[blkAuxIdx] + i*4
	first, end := o.u32(idx), o.u32(idx+4)
	out := make([]aux, 0, end-first)
	for j := first; j < end; j++ {
		b := o.b[o.offs[blkAux]+j*auxSize:]
		out = append(out, aux{b[0], symRef{binary.LittleEndian.Uint32(b[1:]), binary.LittleEndian.Uint32(b[5:])}})
	}
	return out
}

func (o *object) npkg() int {
	return int(o.offs[blkPkgIdx+1]-o.offs[blkPkgIdx]) / stringRefSize
}

func (o *object) pkgName(i uint32) string {
	return o.stringAt(o.offs[blkPkgIdx] + i*stringRefSize)
}

func (o *object) nfile() int {
	return int(o.offs[blkFile+1]-o.offs[blkFile]) / stringRefSize
}

func (o *object) file(i int) string {
	return o.stringAt(o.offs[blkFile] + uint32(i)*stringRefSize)
}

// funcInfo is what the compiler says about a function (goobj.FuncInfo).
type funcInfo struct {
	args      uint32
	funcID    uint8
	flag      uint8
	startLine int32
	inl       []inlNode
}

type inlNode struct {
	fn       symRef
	parentPC int32
}

func parseFuncInfo(b []byte) funcInfo {
	u32 := binary.LittleEndian.Uint32
	fi := funcInfo{args: u32(b), funcID: b[8], flag: b[9], startLine: int32(u32(b[12:]))}
	nfile := u32(b[16:])
	at := 20 + 4*nfile
	ninl := u32(b[at:])
	at += 4
	for k := uint32(0); k < ninl; k++ {
		n := b[at+k*24:]
		fi.inl = append(fi.inl, inlNode{symRef{u32(n[12:]), u32(n[16:])}, int32(u32(n[20:]))})
	}
	return fi
}
