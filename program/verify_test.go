package program_test

import (
	"math"
	"testing"

	"github.com/siyul-park/minivm/instr"
	program "github.com/siyul-park/minivm/program"
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
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
		})
		require.NoError(t, program.Verify(prog))
	})

	// Each row builds a top-level NOP program with fn as its sole constant and
	// checks the verifier's outcome. fn is built through types.FunctionBuilder
	// except for "control/function branch to end", which needs a raw jump
	// operand (see its comment) that no label-based builder call can produce.
	functionCases := []struct {
		name  string
		fn    *types.Function
		check func(t *testing.T, err error)
	}{
		{
			// i8 and i1 share the i32 representation: an i8 param and an i1
			// comparison result both satisfy i32 operands.
			name: "valid/narrow int operands",
			fn: types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI8}, Returns: []types.Type{types.TypeI32}}).
				Emit(
					instr.New(instr.LOCAL_GET, 0),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_LT_S),
					instr.New(instr.LOCAL_GET, 0),
					instr.New(instr.I32_ADD),
					instr.New(instr.RETURN),
				).MustBuild(),
			check: func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			// Width-closed bitwise ops on a shared narrow kind keep that kind
			// (i8 & i8 → i8); the result still satisfies an i32 operand, so
			// chaining another i32 op on it verifies.
			name: "valid/narrow bitwise operands",
			fn: types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI8}, Returns: []types.Type{types.TypeI32}}).
				Emit(
					instr.New(instr.LOCAL_GET, 0),
					instr.New(instr.LOCAL_GET, 0),
					instr.New(instr.I32_AND),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_OR),
					instr.New(instr.RETURN),
				).MustBuild(),
			check: func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			name: "calls/function returns",
			fn: types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
				Emit(instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN)).
				MustBuild(),
			check: func(t *testing.T, err error) { require.NoError(t, err) },
		},
		{
			// Left as a raw literal: it needs a BR_IF with a hand-picked jump
			// operand landing past the function's last real instruction.
			// FunctionBuilder only ever emits branches to labels it resolved
			// to instructions it actually assembled, so it cannot produce this
			// out-of-range target; the point of the case is exercising the
			// verifier's rejection of a malformed function body.
			name: "control/function branch to end",
			fn: &types.Function{
				Typ: &types.FunctionType{},
				Code: instr.Marshal([]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.BR_IF, 0),
				}),
			},
			check: func(t *testing.T, err error) { require.ErrorIs(t, err, program.ErrInvalidJump) },
		},
		{
			name: "control/function falls through",
			fn: types.NewFunctionBuilder(&types.FunctionType{}).
				Emit(instr.New(instr.I32_CONST, 1)).
				MustBuild(),
			check: func(t *testing.T, err error) {
				var ve *program.VerifyError
				require.ErrorAs(t, err, &ve)
				require.ErrorIs(t, ve.Err, program.ErrFallThrough)
				require.Equal(t, 1, ve.Slot)
			},
		},
	}
	for _, tc := range functionCases {
		t.Run(tc.name, func(t *testing.T) {
			prog := program.New([]instr.Instruction{instr.New(instr.NOP)}, program.WithConstants(tc.fn))
			tc.check(t, program.Verify(prog))
		})
	}

	t.Run("calls/direct call", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}}).
			Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)).
			MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))
		require.NoError(t, program.Verify(prog))
	})

	// Both rows build the same if/else merge skeleton; the unbalanced row
	// pushes one extra value in the else arm before the merge, parameterizing
	// the single difference between the two cases.
	mergeCases := []struct {
		name    string
		extra   func(b *program.Builder)
		wantErr error
	}{
		{name: "control/balanced merge"},
		{
			name:    "stack/unbalanced merge",
			extra:   func(b *program.Builder) { b.Emit(instr.I32_CONST, 3) },
			wantErr: program.ErrStackMismatch,
		},
	}
	for _, tc := range mergeCases {
		t.Run(tc.name, func(t *testing.T) {
			b := program.NewBuilder()
			els, end := b.Label(), b.Label()
			b.Emit(instr.I32_CONST, 0)
			b.BrIf(els)
			b.Emit(instr.I32_CONST, 1)
			b.Br(end)
			b.Bind(els)
			b.Emit(instr.I32_CONST, 2)
			if tc.extra != nil {
				tc.extra(b)
			}
			b.Bind(end)
			b.Emit(instr.DROP)
			prog, err := b.Build()
			require.NoError(t, err)
			if tc.wantErr != nil {
				require.ErrorIs(t, program.Verify(prog), tc.wantErr)
			} else {
				require.NoError(t, program.Verify(prog))
			}
		})
	}

	t.Run("control/loop fixpoint", func(t *testing.T) {
		b := program.NewBuilder()
		loop := b.Label()
		b.Bind(loop)
		b.Emit(instr.I32_CONST, 1)
		b.BrIf(loop)
		prog, err := b.Build()
		require.NoError(t, err)
		require.NoError(t, program.Verify(prog))
	})

	t.Run("valid/top-level locals", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.DROP),
		}, program.WithLocals(types.TypeI32))
		require.NoError(t, program.Verify(prog))
	})

	boundsCases := []struct {
		name   string
		instrs []instr.Instruction
		opts   []func(*program.Program)
	}{
		{
			name:   "bounds/top-level local",
			instrs: []instr.Instruction{instr.New(instr.LOCAL_GET, 0), instr.New(instr.DROP)},
		},
		{
			name:   "bounds/constant index",
			instrs: []instr.Instruction{instr.New(instr.CONST_GET, 5)},
		},
		{
			name:   "bounds/local index",
			instrs: []instr.Instruction{instr.New(instr.LOCAL_GET, 9)},
		},
		{
			name:   "bounds/global index",
			instrs: []instr.Instruction{instr.New(instr.GLOBAL_GET, 9)},
			opts:   []func(*program.Program){program.WithGlobals(types.TypeI32)},
		},
	}
	for _, tc := range boundsCases {
		t.Run(tc.name, func(t *testing.T) {
			prog := program.New(tc.instrs, tc.opts...)
			require.ErrorIs(t, program.Verify(prog), program.ErrIndexOutOfRange)
		})
	}

	t.Run("stack/underflow", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.I32_ADD)})
		require.ErrorIs(t, program.Verify(prog), program.ErrStackUnderflow)
	})

	t.Run("valid/array mutation", func(t *testing.T) {
		// ARRAY_APPEND is variable-arity, so the verifier treats it as
		// indeterminate (stopping dataflow); ARRAY_DELETE/ARRAY_SLICE verify by
		// their fixed operand kinds.
		prog := program.New([]instr.Instruction{
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
		}, program.WithTypes(types.NewArrayType(types.TypeI32)))
		require.NoError(t, program.Verify(prog))
	})

	t.Run("stack/array delete underflow", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.ARRAY_DELETE)})
		require.ErrorIs(t, program.Verify(prog), program.ErrStackUnderflow)
	})

	// Each row starts from the identical F32_CONST(1) and differs only in the
	// instructions that follow and any extra program options; all assert
	// ErrTypeMismatch.
	typeMismatchCases := []struct {
		name   string
		follow []instr.Instruction
		opts   []func(*program.Program)
	}{
		{
			name:   "types/operand mismatch",
			follow: []instr.Instruction{instr.New(instr.I32_CONST, 2), instr.New(instr.I32_ADD)},
		},
		{
			name:   "types/global set mismatch",
			follow: []instr.Instruction{instr.New(instr.GLOBAL_SET, 0)},
			opts:   []func(*program.Program){program.WithGlobals(types.TypeI32)},
		},
		{
			name:   "types/global tee mismatch",
			follow: []instr.Instruction{instr.New(instr.GLOBAL_TEE, 0)},
			opts:   []func(*program.Program){program.WithGlobals(types.TypeI32)},
		},
	}
	for _, tc := range typeMismatchCases {
		t.Run(tc.name, func(t *testing.T) {
			instrs := append([]instr.Instruction{instr.New(instr.F32_CONST, uint64(math.Float32bits(1)))}, tc.follow...)
			prog := program.New(instrs, tc.opts...)
			require.ErrorIs(t, program.Verify(prog), program.ErrTypeMismatch)
		})
	}

	t.Run("structure/unknown opcode", func(t *testing.T) {
		prog := &program.Program{Code: []byte{0xFE}}
		require.ErrorIs(t, program.Verify(prog), program.ErrUnknownOpcode)
	})

	t.Run("structure/truncated instruction", func(t *testing.T) {
		prog := &program.Program{Code: []byte{byte(instr.I32_CONST), 0x01}}
		require.ErrorIs(t, program.Verify(prog), program.ErrTruncated)
	})

	t.Run("valid/global index", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.DROP),
		}, program.WithGlobals(types.TypeI32))
		require.NoError(t, program.Verify(prog))
	})

	t.Run("types/dynamic global accepts scalar", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.GLOBAL_SET, 0),
		}, program.WithGlobals(types.TypeAny))
		require.NoError(t, program.Verify(prog))
	})

	t.Run("types/global concrete ref mismatch", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.GLOBAL_SET, 0),
		}, program.WithConstants(types.TypedArray[float32]{1}), program.WithGlobals(types.NewArrayType(types.TypeI32)))
		require.ErrorIs(t, program.Verify(prog), program.ErrTypeMismatch)
	})

	t.Run("control/invalid jump", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.New(instr.BR, 100)})
		require.ErrorIs(t, program.Verify(prog), program.ErrInvalidJump)
	})

	t.Run("control/branch table target inside instruction", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.BR_TABLE, 0, 7),
			instr.New(instr.I64_CONST, uint64(7)<<48),
		})
		require.ErrorIs(t, program.Verify(prog), program.ErrInvalidJump)
	})

	t.Run("handlers/valid protected region", func(t *testing.T) {
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
		require.NoError(t, program.Verify(prog))
	})

	t.Run("handlers/target off instruction boundary", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.THROW),
		}, program.WithHandlers(instr.Handler{Start: 0, End: 5, Catch: 1}))
		require.ErrorIs(t, program.Verify(prog), program.ErrHandlerTarget)
	})

	t.Run("handlers/range out of bounds", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.THROW),
		}, program.WithHandlers(instr.Handler{Start: 0, End: 99, Catch: 5}))
		require.ErrorIs(t, program.Verify(prog), program.ErrHandlerRange)
	})
}

func TestVerifyError_Error(t *testing.T) {
	err := &program.VerifyError{Slot: 2, IP: 7, Opcode: instr.I32_ADD, Err: program.ErrStackUnderflow}
	require.Equal(t, "verify: slot 2, ip 7, i32.add: stack underflow", err.Error())
}

func TestVerifyError_Unwrap(t *testing.T) {
	err := &program.VerifyError{Err: program.ErrTypeMismatch}
	require.ErrorIs(t, err, program.ErrTypeMismatch)
}
