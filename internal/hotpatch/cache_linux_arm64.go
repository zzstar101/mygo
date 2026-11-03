package hotpatch

// clearCache makes the processors see new code in [addr, addr+n).
//
//go:noescape
func clearCache(addr, n uint64)
