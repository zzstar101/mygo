package hotpatch

import (
	"encoding/gob"
	"fmt"
	"net"
	"reflect"
	"runtime"
	"sync/atomic"
	"unsafe"
)

// Probe has no purpose but its address, which tells how far the system moved
// the program from where it was linked, and to keep region linked.
//
//go:noinline
func Probe() *[RegionSize]byte { return &region }

// RegionSize is the memory set aside for patches.
const RegionSize = 64 << 20

// region is where patches go: within the program's image, because direct calls
// on arm64 reach only 128 MB and the system fills the memory around an image
// before Go starts. Nothing touches it until a patch does, so it costs address
// space, of a development build only.
var region [RegionSize]byte

// Serve connects to mygo dev at addr and applies the patches it sends, each
// through run, which must call its argument on the thread where app code is
// not running, and return once it did. applied runs after each. It returns when
// the connection ends.
func Serve(addr string, run func(func()), applied func()) error {
	c, err := net.Dial("unix", addr)
	if err != nil {
		return err
	}
	defer c.Close()
	enc, dec := gob.NewEncoder(c), gob.NewDecoder(c)
	if err := enc.Encode(Hello{runtime.Version(), uint64(reflect.ValueOf(Probe).Pointer())}); err != nil {
		return err
	}
	var res Reserve
	if err := dec.Decode(&res); err != nil {
		return err
	}
	var reserved Reserved
	if at, size, err := reserve(res.At, res.Limit); err != nil {
		reserved.Err = err.Error()
	} else {
		reserved.At, reserved.Size = at, size
	}
	if err := enc.Encode(reserved); err != nil {
		return err
	}
	if reserved.Err != "" {
		return fmt.Errorf("hotpatch: %s", reserved.Err)
	}
	for {
		var p Patch
		if err := dec.Decode(&p); err != nil {
			return err
		}
		var reply Reply
		run(func() {
			if err := apply(&p); err != nil {
				reply.Err = err.Error()
			}
		})
		if err := enc.Encode(reply); err != nil {
			return err
		}
		if reply.Err == "" && applied != nil {
			applied()
		}
	}
}

// at returns the address as a pointer. What it points to is outside the Go
// heap: the program's image, or memory set aside.
func at(addr uint64) unsafe.Pointer {
	return *(*unsafe.Pointer)(unsafe.Pointer(&addr))
}

func bytesAt(addr, n uint64) []byte {
	return unsafe.Slice((*byte)(at(addr)), n)
}

// layoutPage is the alignment internal/hotlink gives the parts of a patch, the
// largest page of the platforms supported but for rare arm64 Linux kernels.
const layoutPage = 16384

func roundPage(n uint64) uint64 { return (n + layoutPage - 1) &^ (layoutPage - 1) }

// apply brings the patch into the program. Nothing it does can be undone, and
// what it replaces stays in memory: goroutines may be running it.
func apply(p *Patch) error {
	copy(bytesAt(p.Base, uint64(len(p.Text))), p.Text)
	copy(bytesAt(p.RodataAt, uint64(len(p.Rodata))), p.Rodata)
	copy(bytesAt(p.DataAt, uint64(len(p.Data))), p.Data)
	if err := protectCode(p.Base, uint64(len(p.Text))); err != nil {
		return err
	}
	if err := protectRead(p.RodataAt, p.DataAt-p.RodataAt); err != nil {
		return err
	}

	// As plugin.Open has the runtime take a module (runtime/plugin.go): at the
	// end of the list, then modulesinit, which publishes the new list and
	// makes the masks of its globals.
	last := (*uint64)(at(p.LastModule))
	next := (*uint64)(at(*last + p.NextOffset))
	atomic.StoreUint64(next, p.Module)
	atomic.StoreUint64(last, p.Module)
	callNoArgs(uintptr(p.ModulesInit))

	for _, w := range p.Writes {
		copy(bytesAt(w.At, uint64(len(w.Data))), w.Data)
	}
	for _, j := range p.Jumps {
		if err := writeJump(j.From, j.To); err != nil {
			return err
		}
	}
	return nil
}

// callNoArgs calls the function at pc.
func callNoArgs(pc uintptr) {
	fv := &pc
	(*(*func())(unsafe.Pointer(&fv)))()
}

// jumpInsn returns what to put at from to jump to to, in the 8 aligned bytes
// that hold the start of a function.
func jumpWord(arch string, old uint64, from, to uint64) (uint64, bool) {
	d := int64(to) - int64(from)
	switch arch {
	case "arm64":
		if d >= 1<<27 || d < -(1<<27) {
			return 0, false
		}
		return old&^0xffffffff | uint64(0x14000000|uint32(d>>2)&0x03ffffff), true
	case "amd64":
		d -= 5
		if d != int64(int32(d)) {
			return 0, false
		}
		return old&^0xffffffffff | 0xe9 | uint64(uint32(d))<<8, true
	}
	return 0, false
}
