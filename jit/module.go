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
//     Each segment shares the segment-level exit convention (rNext carries
//     the next interpreter IP).
//   - Signature describes Entry's ABI; it is meaningless when Entry is nil.
//   - ParamKinds and ReturnKinds let the consumer box/unbox stack values
//     across the Entry boundary without re-deriving them from fn.Typ.
type Module struct {
	Addr        int
	Entry       asm.Callable
	Segments    map[int]asm.Callable
	Signature   asm.Signature
	ParamKinds  []types.Kind
	ReturnKinds []types.Kind
}
