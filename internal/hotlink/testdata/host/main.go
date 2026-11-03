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

func (p *point) sum() int { return p.x + p.y }

var (
	calls    int                              // survives patches
	limit    = 10                             // an initial value edits change
	state    = map[string]int{"kept": 42}     // survives patches
	sentinel = errors.New("the host's error") // identity matters
	held     func() string                    // a closure of the old code
	work     = make(chan func() string, 1)
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
