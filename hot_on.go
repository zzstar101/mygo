//go:build mygo_hot

package mygo

import (
	"fmt"
	"os"

	"github.com/egoist/mygo/internal/hotpatch"
)

// mygo dev -hot builds apps with the tag mygo_hot and tells them where to connect
// with MYGO_HOT: an edit then reaches the running app (internal/hotpatch).
// Without the tag, none of this is linked.
func init() {
	addr := os.Getenv("MYGO_HOT")
	if addr == "" {
		return
	}
	// Not meant for child processes.
	os.Unsetenv("MYGO_HOT")
	go func() {
		err := hotpatch.Serve(addr, onMain, func() {
			for _, w := range Windows() {
				w.Invalidate()
			}
		})
		if err != nil && !loop.stopped() {
			fmt.Fprintln(os.Stderr, "mygo: hot reload:", err)
		}
	}()
}
