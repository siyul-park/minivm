// Package jit lowers minivm bytecode functions into native code via the
// asm package. The compiler delegates per-arch instruction emission to a
// Lowerer registered through Register.
//
// jit deliberately does not import interp: callers wrap Module.Segments in
// their own VM-stack-aware adapters on the consumer side.
package jit

import (
	"github.com/siyul-park/minivm/asm"
)

// Module is the output of Compile for one *types.Function.
//
//   - Addr is the heap index of the source function in the caller's heap.
//   - Entry, when non-nil, is the whole-function direct-callable. Other
//     JIT-compiled callers can target Entry.Addr() via direct BL.
//   - Segments maps source IP → per-block native chunks for partial JIT.
//     Stacks records how many VM stack values each segment consumes as
//     Callable args. Segment stack results return through Callable returns;
//     scratch carries VM context and next interpreter IP.
//   - Signature describes Entry's ABI; it is meaningless when Entry is nil.
type Module struct {
	Addr      int
	Entry     asm.Callable
	Signature asm.Signature
	Segments  map[int]asm.Callable
	Stacks    map[int]int
	Bytes     []int
	Links     int
	Skips     int
}

func newModule(addr int) *Module {
	return &Module{
		Addr:     addr,
		Segments: map[int]asm.Callable{},
		Stacks:   map[int]int{},
	}
}
