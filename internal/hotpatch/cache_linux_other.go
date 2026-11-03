//go:build linux && !amd64 && !arm64

package hotpatch

// clearCache has nothing to do where hot reload does not run: reserve's callers
// never get here, since mygo dev does not build with the tag on these platforms.
func clearCache(addr, n uint64) {}
