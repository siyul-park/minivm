package arm64_test

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	asmarm64 "github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	jitarm64 "github.com/siyul-park/minivm/internal/jit/arm64"
	"github.com/siyul-park/minivm/internal/jit/backend"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"

	"github.com/stretchr/testify/require"
)

// TestEmitter_CallDynamicCallee proves the backend boundary e.callee
// enforces on its own: a callee no operation defines (a block parameter
// here, see backend.Compiler.Def) declines rather than reading as a
// constant. Neither frontend hands this machine such a value (see
// frontend/walk.go's callee), so this backend contract test builds the
// malformed SSA shape directly (see docs/testing.md).
func TestEmitter_CallDynamicCallee(t *testing.T) {
	callee := &types.Function{Typ: &types.FunctionType{}}
	in := &jit.Input{
		Address:  1,
		Function: &types.Function{Typ: &types.FunctionType{}},
		Objects:  jit.Objects{1: {Fn: &types.Function{Typ: &types.FunctionType{}}}, 2: {Fn: callee}},
	}

	b := ssa.New("f")
	block := b.Block()
	arg := b.Param(block, ssa.TypeRef)
	state := b.Value(ssa.TypeState)
	b.Add(block, ssa.Operation{Op: ssa.OpExec, Code: instr.CALL, Args: []ssa.Value{arg}, State: state})
	b.Term(block, ssa.Terminator{Op: ssa.OpReturn})
	fn := b.Build()

	a := asm.New(asmarm64.New())
	m := jitarm64.New()
	_, ok := backend.Compile(m, a, in, jit.Anchor{Addr: 1}, fn)
	require.False(t, ok)
}
