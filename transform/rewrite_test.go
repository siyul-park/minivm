package transform

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestRewriter_Run(t *testing.T) {
	t.Run("insert shifts a forward branch", func(t *testing.T) {
		b := types.NewFunctionBuilder(nil)
		l := b.Label()
		b.BrIf(l)
		b.Emit(instr.New(instr.I32_CONST, 1))
		b.Bind(l)
		b.Emit(instr.New(instr.I32_CONST, 2))
		fn := b.MustBuild()

		r := newRewriter(fn)
		r.insert(3, instr.New(instr.NOP))
		code, _, ok := r.run()
		require.True(t, ok)

		require.Equal(t, byte(instr.NOP), code[3])
		require.Equal(t, 6, instr.ReadI16(instr.Instruction(code).Operand(0)))
	})

	t.Run("insert past the target leaves the branch", func(t *testing.T) {
		b := types.NewFunctionBuilder(nil)
		l := b.Label()
		b.BrIf(l)
		b.Emit(instr.New(instr.I32_CONST, 1))
		b.Bind(l)
		b.Emit(instr.New(instr.I32_CONST, 2))
		fn := b.MustBuild()

		r := newRewriter(fn)
		r.insert(13, instr.New(instr.NOP))
		code, _, ok := r.run()
		require.True(t, ok)
		require.Equal(t, 5, instr.ReadI16(instr.Instruction(code).Operand(0)))
	})

	t.Run("replace shrinks a forward branch", func(t *testing.T) {
		b := types.NewFunctionBuilder(nil)
		l := b.Label()
		b.BrIf(l)
		b.Emit(instr.New(instr.I32_CONST, 1))
		b.Bind(l)
		b.Emit(instr.New(instr.I32_CONST, 2))
		fn := b.MustBuild()

		r := newRewriter(fn)
		r.replace(3, 8, instr.New(instr.LOCAL_GET, 0))
		code, _, ok := r.run()
		require.True(t, ok)
		require.Equal(t, 2, instr.ReadI16(instr.Instruction(code).Operand(0)))
	})

	t.Run("remaps handler boundaries", func(t *testing.T) {
		fn := &types.Function{
			Typ:      &types.FunctionType{},
			Code:     instr.Marshal([]instr.Instruction{instr.New(instr.NOP), instr.New(instr.NOP), instr.New(instr.NOP)}),
			Handlers: []instr.Handler{{Start: 1, End: 2, Catch: 2, Depth: 0}},
		}

		r := newRewriter(fn)
		r.insert(1, instr.New(instr.NOP), instr.New(instr.NOP))
		_, handlers, ok := r.run()
		require.True(t, ok)
		require.Equal(t, []instr.Handler{{Start: 3, End: 4, Catch: 4, Depth: 0}}, handlers)
	})

	t.Run("bails when a branch overflows int16", func(t *testing.T) {
		code := make([]byte, 32771)
		for i := range code {
			code[i] = byte(instr.NOP)
		}
		code[0] = byte(instr.BR_IF)
		instr.Instruction(code).SetOperand(0, uint64(32767))
		fn := &types.Function{Typ: &types.FunctionType{}, Code: code}

		r := newRewriter(fn)
		r.insert(100, instr.New(instr.NOP), instr.New(instr.NOP))
		_, _, ok := r.run()
		require.False(t, ok)
	})
}
