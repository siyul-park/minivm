package asm

import "unsafe"

// flushICache makes newly written executable bytes visible to instruction
// fetch: clean data cache lines to the point of unification, then invalidate
// instruction cache lines.
func flushICache(m memory) {
	if len(m) == 0 {
		return
	}
	start := uintptr(unsafe.Pointer(&m[0]))
	flush(start, start+uintptr(len(m)))
}

func flush(start, end uintptr)
