// Host is a program that hot reload's tests patch while it runs. It reads
// commands from its standard input and answers each on its standard output.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/egoist/mygo/internal/hotlink/testdata/host/lib"
	"github.com/egoist/mygo/internal/hotpatch"
)

// point has a layout the tests' edits may change.
type point struct{ x, y int }

// Area is called by nothing in the program, which only edits may change.
func (p *point) Area() int { return p.x * p.y }

func (p *point) sum() int { return p.x + p.y }

var (
	calls    int                              // survives patches
	limit    = 10                             // an initial value edits change
	state    = map[string]int{"kept": 42}     // survives patches
	sentinel = errors.New("the host's error") // identity matters
	held     func() string                    // a closure of the old code
	work     = make(chan func() string, 1)
)

// Types the program reaches in ways that have no typelink.
type onlyValue struct{ a int }
type onlyPointer struct{ a int }
type namedSlice []int

var (
	heldValue   any = onlyValue{1}
	heldPointer any = &onlyPointer{2}
	heldNamed   any = namedSlice{1, 2}
)

// dynamicIface and dynamic meet only in a conversion as the program runs.
type dynamicIface interface{ value() int }
type dynamic struct{ n int }

func (d *dynamic) value() int { return d.n }

var (
	heldDynamic dynamicIface
	source      any = &dynamic{7}
)

//go:noinline
func message() string { return "v1" }

//go:noinline
func stress() string { return "no stress in v1" }

func main() {
	addr := os.Getenv("MYGO_HOT")
	var ready sync.WaitGroup
	ready.Add(1)
	if addr != "" {
		go func() {
			err := hotpatch.Serve(addr, func(f func()) {
				done := make(chan struct{})
				work <- func() string { f(); close(done); return "" }
				<-done
			}, func() { fmt.Println("applied") })
			if err != nil {
				fmt.Println("serve:", err)
			}
		}()
	}
	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	held = func() string { return "the old closure " + message() }
	for {
		select {
		case f := <-work:
			f()
		case line, ok := <-lines:
			if !ok {
				return
			}
			calls++
			switch line {
			case "convert":
				heldDynamic = source.(dynamicIface)
				fmt.Println("made")
			case "message":
				fmt.Println(message())
			case "lib":
				fmt.Println(lib.Name(), lib.Version)
			case "stress":
				fmt.Println(stress())
			case "held":
				fmt.Println(held())
			case "state":
				p := &point{x: 1, y: 2}
				fmt.Println(calls, limit, state["kept"], p.sum())
			}
		}
	}
}
