package program

import (
	"math"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestVerify(t *testing.T) {
	t.Run("policy/every opcode", func(t *testing.T) {
		policies := map[instr.Opcode]string{
			instr.NOP:          "fixed zero effect",
			instr.UNREACHABLE:  "terminator",
			instr.DUP:          "duplicates the current top kind",
			instr.SWAP:         "swaps the current top kinds",
			instr.BR:           "fixed zero effect",
			instr.SELECT:       "unifies the selected operand kinds",
			instr.CALL:         "uses the statically known callee signature",
			instr.RETURN:       "checks the declared return arity",
			instr.RETURN_CALL:  "uses the statically known callee signature",
			instr.GLOBAL_TEE:   "preserves the stored value on the stack",
			instr.LOCAL_GET:    "uses the declared local kind",
			instr.LOCAL_TEE:    "preserves the stored value on the stack",
			instr.CONST_GET:    "uses the constant value kind",
			instr.UPVAL_GET:    "uses the declared capture kind",
			instr.STRUCT_NEW:   "uses the declared struct field count",
			instr.ARRAY_APPEND: "stops dataflow at its stack-counted arity",
			instr.MAP_NEW:      "stops dataflow at its stack-counted arity",
			instr.CLOSURE_NEW:  "stops dataflow at its capture-counted arity",
		}
		for code := 0; code < 256; code++ {
			op := instr.Opcode(code)
			if !instr.Valid(op) {
				continue
			}
			typ := instr.TypeOf(op)
			if typ.Pop != nil || typ.Push != nil {
				continue
			}
			require.NotEmpty(t, policies[op], "%s has neither a fixed stack effect nor an explicit verifier policy", typ.Mnemonic)
		}
	})
	t.Run("valid/arithmetic", func(t *testing.T) {
		prog := New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
		})
		require.NoError(t, Verify(prog))
	})

	t.Run("valid/narrow int operands", func(t *testing.T) {
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

	t.Run("valid/narrow bitwise operands", func(t *testing.T) {
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

	t.Run("calls/function returns", func(t *testing.T) {
		fn := &types.Function{
			Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Code: instr.Marshal([]instr.Instruction{instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN)}),
		}
		prog := New([]instr.Instruction{instr.New(instr.NOP)}, WithConstants(fn))
		require.NoError(t, Verify(prog))
	})

	t.Run("calls/direct call", func(t *testing.T) {
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

	t.Run("control/balanced merge", func(t *testing.T) {
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

	t.Run("control/loop fixpoint", func(t *testing.T) {
		b := NewBuilder()
		loop := b.Label()
		b.Bind(loop)
		b.Emit(instr.I32_CONST, 1)
		b.BrIf(loop)
		prog, err := b.Build()
		require.NoError(t, err)
		require.NoError(t, Verify(prog))
	})

	t.Run("valid/top-level locals", func(t *testing.T) {
		prog := New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.DROP),
		}, WithLocals(types.TypeI32))
		require.NoError(t, Verify(prog))
	})

	t.Run("bounds/top-level local", func(t *testing.T) {
		prog := New([]instr.Instruction{
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.DROP),
		})
		require.ErrorIs(t, Verify(prog), ErrIndexOutOfRange)
	})

	t.Run("stack/underflow", func(t *testing.T) {
		prog := New([]instr.Instruction{instr.New(instr.I32_ADD)})
		require.ErrorIs(t, Verify(prog), ErrStackUnderflow)
	})

	t.Run("valid/array mutation", func(t *testing.T) {
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

	t.Run("stack/array delete underflow", func(t *testing.T) {
		prog := New([]instr.Instruction{instr.New(instr.ARRAY_DELETE)})
		require.ErrorIs(t, Verify(prog), ErrStackUnderflow)
	})

	t.Run("types/operand mismatch", func(t *testing.T) {
		prog := New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(1))),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
		})
		require.ErrorIs(t, Verify(prog), ErrTypeMismatch)
	})

	t.Run("structure/unknown opcode", func(t *testing.T) {
		prog := &Program{Code: []byte{0xFE}}
		require.ErrorIs(t, Verify(prog), ErrUnknownOpcode)
	})

	t.Run("structure/truncated instruction", func(t *testing.T) {
		prog := &Program{Code: []byte{byte(instr.I32_CONST), 0x01}}
		require.ErrorIs(t, Verify(prog), ErrTruncated)
	})

	t.Run("bounds/constant index", func(t *testing.T) {
		prog := New([]instr.Instruction{instr.New(instr.CONST_GET, 5)})
		require.ErrorIs(t, Verify(prog), ErrIndexOutOfRange)
	})

	t.Run("bounds/local index", func(t *testing.T) {
		prog := New([]instr.Instruction{instr.New(instr.LOCAL_GET, 9)})
		require.ErrorIs(t, Verify(prog), ErrIndexOutOfRange)
	})

	t.Run("valid/global index", func(t *testing.T) {
		prog := New([]instr.Instruction{
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.DROP),
		}, WithGlobals(types.TypeI32))
		require.NoError(t, Verify(prog))
	})

	t.Run("types/global set mismatch", func(t *testing.T) {
		prog := New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(1))),
			instr.New(instr.GLOBAL_SET, 0),
		}, WithGlobals(types.TypeI32))
		require.ErrorIs(t, Verify(prog), ErrTypeMismatch)
	})

	t.Run("types/global tee mismatch", func(t *testing.T) {
		prog := New([]instr.Instruction{
			instr.New(instr.F32_CONST, uint64(math.Float32bits(1))),
			instr.New(instr.GLOBAL_TEE, 0),
		}, WithGlobals(types.TypeI32))
		require.ErrorIs(t, Verify(prog), ErrTypeMismatch)
	})

	t.Run("types/dynamic global accepts scalar", func(t *testing.T) {
		prog := New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.GLOBAL_SET, 0),
		}, WithGlobals(types.TypeRef))
		require.NoError(t, Verify(prog))
	})

	t.Run("types/global concrete ref mismatch", func(t *testing.T) {
		prog := New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.GLOBAL_SET, 0),
		}, WithConstants(types.TypedArray[float32]{1}), WithGlobals(types.NewArrayType(types.TypeI32)))
		require.ErrorIs(t, Verify(prog), ErrTypeMismatch)
	})

	t.Run("bounds/global index", func(t *testing.T) {
		prog := New([]instr.Instruction{instr.New(instr.GLOBAL_GET, 9)}, WithGlobals(types.TypeI32))
		require.ErrorIs(t, Verify(prog), ErrIndexOutOfRange)
	})

	t.Run("control/invalid jump", func(t *testing.T) {
		prog := New([]instr.Instruction{instr.New(instr.BR, 100)})
		require.ErrorIs(t, Verify(prog), ErrInvalidJump)
	})

	t.Run("control/branch table target inside instruction", func(t *testing.T) {
		prog := New([]instr.Instruction{
			instr.New(instr.BR_TABLE, 0, 7),
			instr.New(instr.I64_CONST, uint64(7)<<48),
		})
		require.ErrorIs(t, Verify(prog), ErrInvalidJump)
	})

	t.Run("control/function branch to end", func(t *testing.T) {
		fn := &types.Function{
			Typ: &types.FunctionType{},
			Code: instr.Marshal([]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.BR_IF, 0),
			}),
		}
		prog := New([]instr.Instruction{instr.New(instr.NOP)}, WithConstants(fn))
		require.ErrorIs(t, Verify(prog), ErrInvalidJump)
	})

	t.Run("control/function falls through", func(t *testing.T) {
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

	t.Run("stack/unbalanced merge", func(t *testing.T) {
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

	t.Run("handlers/valid protected region", func(t *testing.T) {
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

	t.Run("handlers/target off instruction boundary", func(t *testing.T) {
		prog := New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.THROW),
		}, WithHandlers(instr.Handler{Start: 0, End: 5, Catch: 1}))
		require.ErrorIs(t, Verify(prog), ErrHandlerTarget)
	})

	t.Run("handlers/range out of bounds", func(t *testing.T) {
		prog := New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.THROW),
		}, WithHandlers(instr.Handler{Start: 0, End: 99, Catch: 5}))
		require.ErrorIs(t, Verify(prog), ErrHandlerRange)
	})
}

func TestVerifyError_Error(t *testing.T) {
	err := &VerifyError{Slot: 2, IP: 7, Opcode: instr.I32_ADD, Err: ErrStackUnderflow}
	require.Equal(t, "verify: slot 2, ip 7, i32.add: stack underflow", err.Error())
}

func TestVerifyError_Unwrap(t *testing.T) {
	err := &VerifyError{Err: ErrTypeMismatch}
	require.ErrorIs(t, err, ErrTypeMismatch)
}
