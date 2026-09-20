package asm

import "unsafe"

// flushICache makes code written through m visible to instruction fetch.
// Executable memory is written through the data cache and fetched through
// the instruction cache, which ARM keeps coherent only through explicit
// maintenance: clean every data line to the point of unification, then
// invalidate every instruction line. Both are user-mode operations here,
// which is the sequence libplatform's sys_icache_invalidate and compiler-rt's
// __clear_cache perform, so no runtime dependency is owed for it.
func flushICache(m memory) {
	if len(m) == 0 {
		return
	}
	start := uintptr(unsafe.Pointer(&m[0]))
	flush(start, start+uintptr(len(m)))
}

func flush(start, end uintptr)
