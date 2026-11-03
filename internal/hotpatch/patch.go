// Package hotpatch applies what a save compiled to the program that is
// running: new code goes into memory beside the program's, the runtime learns
// of it as of a plugin, and the functions it replaces jump to it. The process,
// its windows, its web pages and its memory stay.
//
// Only development builds (the build tag mygo_hot, which mygo dev sets) link
// it. internal/hotlink makes the patches, in mygo dev.
package hotpatch

// Patch is a piece of code linked to run at Base, and what to do with it.
type Patch struct {
	Base uint64 // where it goes, in the region Hello gave

	Text   []byte // executable, at Base
	Rodata []byte // read-only, at RodataAt
	Data   []byte // writable, at DataAt; zeros follow it up to End

	RodataAt, DataAt, End uint64

	Module      uint64 // its runtime.moduledata
	NextOffset  uint64 // of the field that links modules
	LastModule  uint64 // runtime.lastmoduledatap
	ModulesInit uint64 // runtime.modulesinit
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

// Reply is what a program answers a Patch with.
type Reply struct {
	Err string
}
