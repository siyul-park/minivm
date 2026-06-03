// Package jit lowers minivm bytecode functions into native code via the
// asm package. The compiler delegates per-arch instruction emission to a
// Lowerer registered through Register.
//
// jit deliberately does not import interp: callers wrap Module.Segments in
// their own VM-stack-aware adapters on the consumer side.
package jit

import (
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/types"
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
//   - Params and Returns let the consumer box/unbox stack values
//     across the Entry boundary without re-deriving them from fn.Typ.
type Module struct {
	Addr      int
	Params    []types.Kind
	Returns   []types.Kind
	Entry     asm.Callable
	Signature asm.Signature
	Segments  map[int]asm.Callable
	Stacks    map[int]int
	Bytes     []int
	Links     int
	Skips     int
}

// NewModule returns a default Module that carries fn's boxing metadata.
// The Segments map starts empty; the compiler fills it as segments link.
func NewModule(fn *types.Function, addr int) *Module {
	var params, returns []types.Kind
	if fn != nil && fn.Typ != nil {
		params = make([]types.Kind, len(fn.Typ.Params))
		for i, t := range fn.Typ.Params {
			params[i] = t.Kind()
		}
		returns = make([]types.Kind, len(fn.Typ.Returns))
		for i, t := range fn.Typ.Returns {
			returns[i] = t.Kind()
		}
	}
	return &Module{
		Addr:     addr,
		Segments: map[int]asm.Callable{},
		Stacks:   map[int]int{},
		Params:   params,
		Returns:  returns,
	}
}
