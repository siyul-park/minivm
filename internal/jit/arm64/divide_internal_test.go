package arm64

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	asmarm64 "github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/backend"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"

	"github.com/stretchr/testify/require"
)

// TestEmitter_Divide proves the shape checks divide declines on: neither
// reaches program.Verify, since a well-formed function never has them (a
// wrong arity is rejected by the verifier and a lane mismatch by the type
// checker before either pipeline runs), so each is built by hand (see
// docs/testing.md). The first two carry no state: divide checks arity and
// lane before it ever reads op.State, and newCompiler independently rejects
// any OpState whose frame names a function jit.Input cannot resolve (see
// Compiler.measure), which a state-carrying version of these two shapes
// would trip before Lower ever ran, proving the wrong thing.
func TestEmitter_Divide(t *testing.T) {
	in := &jit.Input{Address: 1, Function: &types.Function{Typ: &types.FunctionType{}}}
	m := machine{scratch: []asm.PReg{asmarm64.X10, asmarm64.X11, asmarm64.X12, asmarm64.X13, asmarm64.X14}}

	t.Run("declines a divide missing its divisor", func(t *testing.T) {
		b := ssa.New("f")
		block := b.Block()
		lhs, result := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(block, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{lhs}})
		b.Add(block, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_DIV_S, Args: []ssa.Value{lhs}, Results: []ssa.Value{result}})
		b.Term(block, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{result}})
		fn := b.Build()

		a := asm.New(asmarm64.New())
		_, ok := backend.Compile(m, a, in, jit.Anchor{Addr: 1}, fn)
		require.False(t, ok)
	})

	t.Run("declines a divide whose operands disagree with its own lane", func(t *testing.T) {
		b := ssa.New("f")
		block := b.Block()
		lhs, rhs, result := b.Value(ssa.TypeI64), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(block, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI64(1), Results: []ssa.Value{lhs}})
		b.Add(block, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{rhs}})
		b.Add(block, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_DIV_S, Args: []ssa.Value{lhs, rhs}, Results: []ssa.Value{result}})
		b.Term(block, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{result}})
		fn := b.Build()

		a := asm.New(asmarm64.New())
		_, ok := backend.Compile(m, a, in, jit.Anchor{Addr: 1}, fn)
		require.False(t, ok)
	})

	t.Run("declines a divide with no state to resume a zero divisor into", func(t *testing.T) {
		b := ssa.New("f")
		block := b.Block()
		lhs, rhs, result := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(block, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(6), Results: []ssa.Value{lhs}})
		b.Add(block, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(2), Results: []ssa.Value{rhs}})
		b.Add(block, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_DIV_S, Args: []ssa.Value{lhs, rhs}, Results: []ssa.Value{result}})
		b.Term(block, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{result}})
		fn := b.Build()

		a := asm.New(asmarm64.New())
		_, ok := backend.Compile(m, a, in, jit.Anchor{Addr: 1}, fn)
		require.False(t, ok)
	})
}
