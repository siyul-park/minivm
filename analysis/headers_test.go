package analysis_test

import (
	"testing"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestHeaders(t *testing.T) {
	t.Run("finds the offset a back edge targets", func(t *testing.T) {
		b := instr.NewBuilder()
		header, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(header)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 10).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(header)
		b.Bind(done).Emit(instr.RETURN)
		code, err := b.Assemble()
		require.NoError(t, err)
		fn := &types.Function{Locals: []types.Type{types.TypeI32}, Code: instr.Marshal(code)}
		headerOffset := instr.New(instr.I32_CONST, 0).Width() + instr.New(instr.LOCAL_SET, 0).Width()

		got, err := analysis.Headers(fn)
		require.NoError(t, err)
		require.Equal(t, []int{headerOffset}, got)
	})

	t.Run("finds none in straight-line code", func(t *testing.T) {
		fn := &types.Function{Code: instr.Marshal([]instr.Instruction{instr.New(instr.RETURN)})}

		got, err := analysis.Headers(fn)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("propagates an invalid jump", func(t *testing.T) {
		fn := types.NewFunctionBuilder(nil).Emit(instr.New(instr.BR, 10)).MustBuild()

		_, err := analysis.Headers(fn)
		require.ErrorIs(t, err, analysis.ErrInvalidJump)
	})
}
