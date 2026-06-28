package program

import (
	"math"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestVerify(t *testing.T) {
	t.Run("valid arithmetic", func(t *testing.T) {
		prog := New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
		})
		require.NoError(t, Verify(prog))
	})

	t.Run("valid narrow int operands", func(t *testing.T) {
		// i8 and i1 share the i32 representation: an i8 param and an i1
		// comparison result both satisfy i32 operands.
		fn := &types.Function{
			Typ: &types.FunctionType{Params: []types.Type{types.TypeI8}, Returns: []types.Type{types.TypeI32}},
			Code: instr.Marshal([]instr.Instruction{
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_LT_S),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_ADD),
				instr.New(instr.RETURN),
			}),
		}
		prog := New([]instr.Instruction{instr.New(instr.NOP)}, WithConstants(fn))
		require.NoError(t, Verify(prog))
	})

	t.Run("valid narrow bitwise operands", func(t *testing.T) {
		// Width-closed bitwise ops on a shared narrow kind keep that kind
		// (i8 & i8 → i8); the result still satisfies an i32 operand, so chaining
		// another i32 op on it verifies.
		fn := &types.Function{
			Typ: &types.FunctionType{Params: []types.Type{types.TypeI8}, Returns: []types.Type{types.TypeI32}},
			Code: instr.Marshal([]instr.Instruction{
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_AND),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_OR),
				instr.New(instr.RETURN),
			}),
		}
		prog := New([]instr.Instruction{instr.New(instr.NOP)}, WithConstants(fn))
		require.NoError(t, Verify(prog))
	})

	t.Run("valid function returns", func(t *testing.T) {
		fn := &types.Function{
			Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Code: instr.Marshal([]instr.Instruction{instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN)}),
		}
		prog := New([]instr.Instruction{instr.New(instr.NOP)}, WithConstants(fn))
		require.NoError(t, Verify(prog))
	})

	t.Run("valid direct call", func(t *testing.T) {
		fn := &types.Function{
			Typ:  &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
			Code: instr.Marshal([]instr.Instruction{instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)}),
		}
		prog := New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, WithConstants(fn))
		require.NoError(t, Verify(prog))
	})

	t.Run("valid balanced merge", func(t *testing.T) {
		b := NewBuilder()
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
		b := NewBuilder()
		loop := b.Label()
		b.Bind(loop)
		b.Emit(instr.I32_CONST, 1)
		b.BrIf(loop)
		prog, err := b.Build()
		require.NoError(t, err)
		require.NoError(t, Verify(prog))
	})

	t.Run("valid top-level locals", func(t *testing.T) {
		prog := New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.DROP),
		}, WithLocals(types.TypeI32))
		require.NoError(t, Verify(prog))
	})

	t.Run("top-level local out of range", func(t *testing.T) {
		prog := New([]instr.Instruction{
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.DROP),
		})
		require.ErrorIs(t, Verify(prog), ErrIndexOutOfRange)
	})

	t.Run("stack underflow", func(t *testing.T) {
		prog := New([]instr.Instruction{instr.New(instr.I32_ADD)})
		require.ErrorIs(t, Verify(prog), ErrStackUnderflow)
	})

	t.Run("valid array mutation", func(t *testing.T) {
		// ARRAY_APPEND is variable-arity, so the verifier treats it as
		// indeterminate (stopping dataflow); ARRAY_DELETE/ARRAY_SLICE verify by
		// their fixed operand kinds.
		prog := New([]instr.Instruction{
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.ARRAY_NEW_DEFAULT, 0),
			instr.New(instr.I32_CONST, 10),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.ARRAY_APPEND),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.ARRAY_SLICE),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.ARRAY_DELETE),
			instr.New(instr.DROP),
		}, WithTypes(types.NewArrayType(types.TypeI32)))
		require.NoError(t, Verify(prog))
	})

	t.Run("array delete underflow", func(t *testing.T) {
		prog := New([]instr.Instruction{instr.New(instr.ARRAY_DELETE)})
		require.ErrorIs(t, Verify(prog), ErrStackUnderflow)
	})

	t.Run("operand type mismatch", func(t *testing.T) {
		prog := New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(1))),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
		})
		require.ErrorIs(t, Verify(prog), ErrTypeMismatch)
	})

	t.Run("unknown opcode", func(t *testing.T) {
		prog := &Program{Code: []byte{0xFE}}
		require.ErrorIs(t, Verify(prog), ErrUnknownOpcode)
	})

	t.Run("truncated instruction", func(t *testing.T) {
		prog := &Program{Code: []byte{byte(instr.I32_CONST), 0x01}}
		require.ErrorIs(t, Verify(prog), ErrTruncated)
	})

	t.Run("constant index out of range", func(t *testing.T) {
		prog := New([]instr.Instruction{instr.New(instr.CONST_GET, 5)})
		require.ErrorIs(t, Verify(prog), ErrIndexOutOfRange)
	})

	t.Run("local index out of range", func(t *testing.T) {
		prog := New([]instr.Instruction{instr.New(instr.LOCAL_GET, 9)})
		require.ErrorIs(t, Verify(prog), ErrIndexOutOfRange)
	})

	t.Run("invalid jump", func(t *testing.T) {
		prog := New([]instr.Instruction{instr.New(instr.BR, 100)})
		require.ErrorIs(t, Verify(prog), ErrInvalidJump)
	})

	t.Run("function falls through", func(t *testing.T) {
		fn := &types.Function{
			Typ:  &types.FunctionType{},
			Code: instr.Marshal([]instr.Instruction{instr.New(instr.I32_CONST, 1)}),
		}
		prog := New([]instr.Instruction{instr.New(instr.NOP)}, WithConstants(fn))

		var ve *VerifyError
		require.ErrorAs(t, Verify(prog), &ve)
		require.ErrorIs(t, ve.Err, ErrFallThrough)
		require.Equal(t, 1, ve.Slot)
	})

	t.Run("unbalanced merge", func(t *testing.T) {
		b := NewBuilder()
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

	t.Run("valid protected region", func(t *testing.T) {
		b := NewBuilder()
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
		prog := New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.THROW),
		}, WithHandlers(instr.Handler{Start: 0, End: 5, Catch: 1}))
		require.ErrorIs(t, Verify(prog), ErrHandlerTarget)
	})

	t.Run("handler range out of bounds", func(t *testing.T) {
		prog := New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.THROW),
		}, WithHandlers(instr.Handler{Start: 0, End: 99, Catch: 5}))
		require.ErrorIs(t, Verify(prog), ErrHandlerRange)
	})
}
