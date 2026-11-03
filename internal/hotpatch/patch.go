// Package hotpatch applies what a save compiled to the program that is
// running: new code goes into memory beside the program's, the runtime learns
// of it as of a plugin, and the functions it replaces jump to it. The process,
// its windows, its web pages and its memory stay.
//
// Only development builds (the build tag mygo_hot, which mygo dev -hot sets) link
// it. internal/hotlink makes the patches, in mygo dev.
package hotpatch

import "encoding/gob"

// Patch is a piece of code linked to run at Base, and what to do with it.
type Patch struct {
	Base uint64 // where it goes, in the region Hello gave

	Text   []byte // executable, at Base
	Rodata []byte // read-only, at RodataAt
	Data   []byte // writable, at DataAt; zeros follow it up to End

	RodataAt, DataAt, End uint64

	Module      uint64   // its runtime.moduledata
	NextOffset  uint64   // of the field that links modules
	LastModule  uint64   // runtime.lastmoduledatap
	ModulesInit uint64   // runtime.modulesinit
	Itabs       []uint64 // the itabs it brings
	ItabAdd     uint64   // runtime.itabAdd
	ItabLock    uint64   // runtime.itabLock
	Lock        uint64   // runtime.lock2
	Unlock      uint64   // runtime.unlock2
	Jumps       []Jump
	Writes      []Write
	Functions   []string // the names of the functions it replaces, to report
}

// Jump redirects a function to its new code.
type Jump struct{ From, To uint64 }

// Write sets a variable whose initial value the edit changed.
type Write struct {
	At   uint64
	Data []byte
}

// Hello is what a program tells mygo dev as it connects.
type Hello struct {
	Version string // runtime.Version()
	Probe   uint64 // the address of Probe, which tells how the image slid
}

// Reserve asks a program to set memory aside for patches. Calls reach only so
// far, so it must lie just after the program's image, below Limit; the system
// keeps some memory there, so the program looks from At.
type Reserve struct {
	At, Limit uint64
}

// Reserved is what a program answers a Reserve with.
type Reserved struct {
	At, Size uint64
	Err      string
}

// Request is what mygo dev asks of a program.
type Request struct {
	Patch *Patch
	// Itab asks whether the program has an itab of this interface and type, in
	// the table of the runtime, whose address follows.
	Itab *ItabQuery
}

// ItabQuery asks for an itab.
type ItabQuery struct {
	Inter, Type uint64
	Table       uint64 // runtime.itabTable
}

// Reply is what a program answers a Request with.
type Reply struct {
	Err  string
	Itab uint64
}

// Client asks a program for what it can do, over a connection of Serve's.
type Client struct {
	Enc *gob.Encoder
	Dec *gob.Decoder
}

// Do sends a request and waits for the answer.
func (c *Client) Do(r Request) (Reply, error) {
	var reply Reply
	if err := c.Enc.Encode(r); err != nil {
		return reply, err
	}
	if err := c.Dec.Decode(&reply); err != nil {
		return reply, err
	}
	return reply, nil
}
