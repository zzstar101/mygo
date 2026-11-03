package hotpatch

// clearCache has nothing to do: x86 keeps instructions coherent with memory.
func clearCache(addr, n uint64) {}
