package arm64

import "unsafe"

// Flush makes newly written executable bytes visible to instruction fetch:
// clean data cache lines to the point of unification, then invalidate
// instruction cache lines.
func (Arch) Flush(code []byte) {
	if len(code) == 0 {
		return
	}
	start := uintptr(unsafe.Pointer(&code[0]))
	flush(start, start+uintptr(len(code)))
}

func flush(start, end uintptr)
