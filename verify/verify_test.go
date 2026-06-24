package verify

import (
	"math"
	"testing"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestVerify(t *testing.T) {
	t.Run("valid arithmetic", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
		})
		require.NoError(t, Verify(prog))
	})

	t.Run("valid function returns", func(t *testing.T) {
		fn := &types.Function{
			Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Code: instr.Marshal([]instr.Instruction{instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN)}),
		}
		prog := program.New([]instr.Instruction{instr.New(instr.NOP)}, program.WithConstants(fn))
		require.NoError(t, Verify(prog))
	})

	t.Run("valid direct call", func(t *testing.T) {
		fn := &types.Function{
			Typ:  &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
			Code: instr.Marshal([]instr.Instruction{instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)}),
		}
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))
		require.NoError(t, Verify(prog))
	})

	t.Run("valid balanced merge", func(t *testing.T) {
		b := program.NewBuilder()
		els, end := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0)
		b.BrIf(els)
		b.Emit(instr.I32_CONST, 1)
		b.Br(end)
		b.Bind(els)
		b.Emit(instr.I32_CONST, 2)
		b.Bind(end)
		b.Emit(instr.DROP)
		prog, err := b.Build()
		require.NoError(t, err)
		require.NoError(t, Verify(prog))
	})

	t.Run("valid loop fixpoint", func(t *testing.T) {
		b := program.NewBuilder()
		loop := b.Label()
		b.Bind(loop)
		b.Emit(instr.I32_CONST, 1)
		b.BrIf(loop)
		prog, err := b.Build()
		require.NoError(t, err)
		require.NoError(t, Verify(prog))
	})

	t.Run("stack underflow", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.I32_ADD)})
		require.ErrorIs(t, Verify(prog), ErrStackUnderflow)
	})

	t.Run("operand type mismatch", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(1))),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
		})
		require.ErrorIs(t, Verify(prog), ErrTypeMismatch)
	})

	t.Run("unknown opcode", func(t *testing.T) {
		prog := &program.Program{Code: []byte{0xFE}}
		require.ErrorIs(t, Verify(prog), ErrUnknownOpcode)
	})

	t.Run("truncated instruction", func(t *testing.T) {
		prog := &program.Program{Code: []byte{byte(instr.I32_CONST), 0x01}}
		require.ErrorIs(t, Verify(prog), ErrTruncated)
	})

	t.Run("constant index out of range", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.CONST_GET, 5)})
		require.ErrorIs(t, Verify(prog), ErrIndexOutOfRange)
	})

	t.Run("local index out of range", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.LOCAL_GET, 9)})
		require.ErrorIs(t, Verify(prog), ErrIndexOutOfRange)
	})

	t.Run("local delete index out of range", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.LOCAL_DELETE, 9)})
		require.ErrorIs(t, Verify(prog), ErrIndexOutOfRange)
	})

	t.Run("upvalue delete index out of range", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.UPVAL_DELETE, 0)})
		require.ErrorIs(t, Verify(prog), ErrIndexOutOfRange)
	})

	t.Run("invalid jump", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.BR, 100)})
		require.ErrorIs(t, Verify(prog), analysis.ErrInvalidJump)
	})

	t.Run("function falls through", func(t *testing.T) {
		fn := &types.Function{
			Typ:  &types.FunctionType{},
			Code: instr.Marshal([]instr.Instruction{instr.New(instr.I32_CONST, 1)}),
		}
		prog := program.New([]instr.Instruction{instr.New(instr.NOP)}, program.WithConstants(fn))

		var ve *VerifyError
		require.ErrorAs(t, Verify(prog), &ve)
		require.ErrorIs(t, ve.Err, ErrFallThrough)
		require.Equal(t, 1, ve.Slot)
	})

	t.Run("unbalanced merge", func(t *testing.T) {
		b := program.NewBuilder()
		els, end := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0)
		b.BrIf(els)
		b.Emit(instr.I32_CONST, 1)
		b.Br(end)
		b.Bind(els)
		b.Emit(instr.I32_CONST, 2)
		b.Emit(instr.I32_CONST, 3)
		b.Bind(end)
		b.Emit(instr.DROP)
		prog, err := b.Build()
		require.NoError(t, err)
		require.ErrorIs(t, Verify(prog), ErrStackMismatch)
	})

	t.Run("unknown extension rejected when registry known", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.EXT, uint64(3)<<8, 0)})
		require.ErrorIs(t, Verify(prog, WithExtensions(1)), ErrUnknownExtension)
		require.NoError(t, Verify(prog))
	})

	t.Run("valid protected region", func(t *testing.T) {
		b := program.NewBuilder()
		start, end, catch := b.Label(), b.Label(), b.Label()
		b.Bind(start)
		b.Emit(instr.I32_CONST, 1)
		b.Emit(instr.THROW)
		b.Bind(end)
		b.Bind(catch)
		b.Emit(instr.DROP)
		b.Try(start, end, catch, 0)
		prog, err := b.Build()
		require.NoError(t, err)
		require.NoError(t, Verify(prog))
	})

	t.Run("handler target off an instruction boundary", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.THROW),
		}, program.WithHandlers(instr.Handler{Start: 0, End: 5, Catch: 1}))
		require.ErrorIs(t, Verify(prog), ErrHandlerTarget)
	})

	t.Run("handler range out of bounds", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.THROW),
		}, program.WithHandlers(instr.Handler{Start: 0, End: 99, Catch: 5}))
		require.ErrorIs(t, Verify(prog), ErrHandlerRange)
	})
}
