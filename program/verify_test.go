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
	// Wraps fn as the sole constant of a top-level NOP program.
	withFunction := func(fn *types.Function) *program.Program {
		return program.New([]instr.Instruction{instr.MustNew(instr.NOP)}, program.WithConstants(fn))
	}
	// Assembles a program through the label-based builder.
	assemble := func(emit func(b *program.Builder)) *program.Program {
		b := program.NewBuilder()
		emit(b)
		prog, err := b.Build()
		require.NoError(t, err)
		return prog
	}
	// Builds the if/else merge skeleton; the else arm pushes extra before the merge.
	merge := func(extra ...uint64) *program.Program {
		return assemble(func(b *program.Builder) {
			els, end := b.Label(), b.Label()
			b.Emit(instr.I32_CONST, 0)
			b.BrIf(els)
			b.Emit(instr.I32_CONST, 1)
			b.Br(end)
			b.Bind(els)
			b.Emit(instr.I32_CONST, 2)
			for _, v := range extra {
				b.Emit(instr.I32_CONST, v)
			}
			b.Bind(end)
			b.Emit(instr.DROP)
		})
	}
	f32 := instr.MustNew(instr.F32_CONST, uint64(math.Float32bits(1)))

	tests := []struct {
		name     string
		prog     *program.Program
		wantErr  error
		wantSlot int // asserted only when positive
	}{
		{
			name: "valid/arithmetic",
			prog: program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 1),
				instr.MustNew(instr.I32_CONST, 2),
				instr.MustNew(instr.I32_ADD),
			}),
		},
		{
			// i8 and i1 share the i32 representation: an i8 param and an i1
			// comparison result both satisfy i32 operands.
			name: "valid/narrow int operands",
			prog: withFunction(types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI8()}, Returns: []types.Type{types.TypeI32()}}).
				Append(
					instr.MustNew(instr.LOCAL_GET, 0),
					instr.MustNew(instr.I32_CONST, 1),
					instr.MustNew(instr.I32_LT_S),
					instr.MustNew(instr.LOCAL_GET, 0),
					instr.MustNew(instr.I32_ADD),
					instr.MustNew(instr.RETURN),
				).MustBuild()),
		},
		{
			// Width-closed bitwise ops on a shared narrow kind keep that kind
			// (i8 & i8 -> i8); the result still satisfies an i32 operand, so
			// chaining another i32 op on it verifies.
			name: "valid/narrow bitwise operands",
			prog: withFunction(types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI8()}, Returns: []types.Type{types.TypeI32()}}).
				Append(
					instr.MustNew(instr.LOCAL_GET, 0),
					instr.MustNew(instr.LOCAL_GET, 0),
					instr.MustNew(instr.I32_AND),
					instr.MustNew(instr.I32_CONST, 1),
					instr.MustNew(instr.I32_OR),
					instr.MustNew(instr.RETURN),
				).MustBuild()),
		},
		{
			name: "calls/function returns",
			prog: withFunction(types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32()}}).
				Append(instr.MustNew(instr.I32_CONST, 7), instr.MustNew(instr.RETURN)).
				MustBuild()),
		},
		{
			// A raw literal: it needs a BR_IF with a hand-picked jump operand
			// landing past the function's last real instruction, which
			// FunctionBuilder cannot produce.
			name: "control/function branch to end",
			prog: withFunction(&types.Function{
				Typ: &types.FunctionType{},
				Code: instr.Marshal([]instr.Instruction{
					instr.MustNew(instr.I32_CONST, 1),
					instr.MustNew(instr.BR_IF, 0),
				}),
			}),
			wantErr: program.ErrInvalidJump,
		},
		{
			name: "control/function falls through",
			prog: withFunction(types.NewFunctionBuilder(&types.FunctionType{}).
				Append(instr.MustNew(instr.I32_CONST, 1)).
				MustBuild()),
			wantErr:  program.ErrFallThrough,
			wantSlot: 1,
		},
		{
			name: "calls/direct call",
			prog: program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 5),
				instr.MustNew(instr.CONST_GET, 0),
				instr.MustNew(instr.CALL),
			}, program.WithConstants(types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32()}, Returns: []types.Type{types.TypeI32()}}).
				Append(instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.RETURN)).
				MustBuild())),
		},
		{
			name: "control/balanced merge",
			prog: merge(),
		},
		{
			name:    "stack/unbalanced merge",
			prog:    merge(3),
			wantErr: program.ErrStackMismatch,
		},
		{
			name: "control/loop fixpoint",
			prog: assemble(func(b *program.Builder) {
				loop := b.Label()
				b.Bind(loop)
				b.Emit(instr.I32_CONST, 1)
				b.BrIf(loop)
			}),
		},
		{
			name: "valid/top-level locals",
			prog: program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 7),
				instr.MustNew(instr.LOCAL_SET, 0),
				instr.MustNew(instr.LOCAL_GET, 0),
				instr.MustNew(instr.DROP),
			}, program.WithLocals(types.TypeI32())),
		},
		{
			name:    "bounds/top-level local",
			prog:    program.New([]instr.Instruction{instr.MustNew(instr.LOCAL_GET, 0), instr.MustNew(instr.DROP)}),
			wantErr: program.ErrIndexOutOfRange,
		},
		{
			name:    "bounds/constant index",
			prog:    program.New([]instr.Instruction{instr.MustNew(instr.CONST_GET, 5)}),
			wantErr: program.ErrIndexOutOfRange,
		},
		{
			name:    "bounds/local index",
			prog:    program.New([]instr.Instruction{instr.MustNew(instr.LOCAL_GET, 9)}),
			wantErr: program.ErrIndexOutOfRange,
		},
		{
			name:    "bounds/global index",
			prog:    program.New([]instr.Instruction{instr.MustNew(instr.GLOBAL_GET, 9)}, program.WithGlobals(types.TypeI32())),
			wantErr: program.ErrIndexOutOfRange,
		},
		{
			name:    "stack/underflow",
			prog:    program.New([]instr.Instruction{instr.MustNew(instr.I32_ADD)}),
			wantErr: program.ErrStackUnderflow,
		},
		{
			// ARRAY_APPEND is variable-arity, so the verifier treats it as
			// indeterminate (stopping dataflow); ARRAY_DELETE/ARRAY_SLICE verify
			// by their fixed operand kinds.
			name: "valid/array mutation",
			prog: program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 0),
				instr.MustNew(instr.ARRAY_NEW_DEFAULT, 0),
				instr.MustNew(instr.I32_CONST, 10),
				instr.MustNew(instr.I32_CONST, 1),
				instr.MustNew(instr.ARRAY_APPEND),
				instr.MustNew(instr.I32_CONST, 0),
				instr.MustNew(instr.I32_CONST, 1),
				instr.MustNew(instr.ARRAY_SLICE),
				instr.MustNew(instr.I32_CONST, 0),
				instr.MustNew(instr.ARRAY_DELETE),
				instr.MustNew(instr.DROP),
			}, program.WithTypes(types.NewArrayType(types.TypeI32()))),
		},
		{
			name:    "stack/array delete underflow",
			prog:    program.New([]instr.Instruction{instr.MustNew(instr.ARRAY_DELETE)}),
			wantErr: program.ErrStackUnderflow,
		},
		{
			name:    "types/operand mismatch",
			prog:    program.New([]instr.Instruction{f32, instr.MustNew(instr.I32_CONST, 2), instr.MustNew(instr.I32_ADD)}),
			wantErr: program.ErrTypeMismatch,
		},
		{
			name:    "types/global set mismatch",
			prog:    program.New([]instr.Instruction{f32, instr.MustNew(instr.GLOBAL_SET, 0)}, program.WithGlobals(types.TypeI32())),
			wantErr: program.ErrTypeMismatch,
		},
		{
			name:    "types/global tee mismatch",
			prog:    program.New([]instr.Instruction{f32, instr.MustNew(instr.GLOBAL_TEE, 0)}, program.WithGlobals(types.TypeI32())),
			wantErr: program.ErrTypeMismatch,
		},
		{
			name:    "structure/unknown opcode",
			prog:    &program.Program{Code: []byte{0xFE}},
			wantErr: instr.ErrUnknownOpcode,
		},
		{
			name:    "structure/truncated instruction",
			prog:    &program.Program{Code: []byte{byte(instr.I32_CONST), 0x01}},
			wantErr: program.ErrTruncated,
		},
		{
			name: "valid/global index",
			prog: program.New([]instr.Instruction{
				instr.MustNew(instr.GLOBAL_GET, 0),
				instr.MustNew(instr.DROP),
			}, program.WithGlobals(types.TypeI32())),
		},
		{
			name: "types/dynamic global accepts scalar",
			prog: program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 1),
				instr.MustNew(instr.GLOBAL_SET, 0),
			}, program.WithGlobals(types.TypeAny())),
		},
		{
			name: "types/global concrete ref mismatch",
			prog: program.New([]instr.Instruction{
				instr.MustNew(instr.CONST_GET, 0),
				instr.MustNew(instr.GLOBAL_SET, 0),
			}, program.WithConstants(types.TypedArray[float32]{1}), program.WithGlobals(types.NewArrayType(types.TypeI32()))),
			wantErr: program.ErrTypeMismatch,
		},
		{
			name:    "control/invalid jump",
			prog:    program.New([]instr.Instruction{instr.MustNew(instr.BR, 100)}),
			wantErr: program.ErrInvalidJump,
		},
		{
			name: "control/branch table target inside instruction",
			prog: program.New([]instr.Instruction{
				instr.MustNew(instr.BR_TABLE, 0, 7),
				instr.MustNew(instr.I64_CONST, uint64(7)<<48),
			}),
			wantErr: program.ErrInvalidJump,
		},
		{
			name: "handlers/valid protected region",
			prog: assemble(func(b *program.Builder) {
				start, end, catch := b.Label(), b.Label(), b.Label()
				b.Bind(start)
				b.Emit(instr.I32_CONST, 1)
				b.Emit(instr.THROW)
				b.Bind(end)
				b.Bind(catch)
				b.Emit(instr.DROP)
				b.Try(start, end, catch, 0)
			}),
		},
		{
			name: "handlers/target off instruction boundary",
			prog: program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 1),
				instr.MustNew(instr.THROW),
			}, program.WithHandlers(instr.Handler{Start: 0, End: 5, Catch: 1})),
			wantErr: program.ErrHandlerTarget,
		},
		{
			name: "handlers/range out of bounds",
			prog: program.New([]instr.Instruction{
				instr.MustNew(instr.I32_CONST, 1),
				instr.MustNew(instr.THROW),
			}, program.WithHandlers(instr.Handler{Start: 0, End: 99, Catch: 5})),
			wantErr: program.ErrHandlerRange,
		},
		{
			// array.new pops a runtime count of elements: dataflow ends on its path.
			name: "valid/array.new with several elements before a merge",
			prog: assemble(func(b *program.Builder) {
				els, end := b.Label(), b.Label()
				typ := uint64(b.Type(types.TypeI32Array()))
				b.Emit(instr.I32_CONST, 0).BrIf(els)
				b.Emit(instr.I32_CONST, 10).Emit(instr.I32_CONST, 20).Emit(instr.I32_CONST, 30)
				b.Emit(instr.I32_CONST, 3).Emit(instr.ARRAY_NEW, typ)
				b.Br(end)
				b.Bind(els).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, typ)
				b.Bind(end).Emit(instr.DROP)
			}),
		},
		{
			name: "valid/function nested in a literal",
			prog: program.New([]instr.Instruction{instr.MustNew(instr.NOP)}, program.WithConstants(
				&types.ArrayLiteral{Typ: types.NewArrayType(types.TypeAny()), Elems: []types.Value{
					types.NewFunction(nil, nil, []instr.Instruction{instr.MustNew(instr.RETURN)}),
				}},
			)),
		},
		{
			name: "constants/invalid function nested in an array literal",
			prog: program.New([]instr.Instruction{instr.MustNew(instr.NOP)}, program.WithConstants(
				types.I32(1),
				&types.ArrayLiteral{Typ: types.NewArrayType(types.TypeAny()), Elems: []types.Value{
					types.NewFunction(nil, nil, []instr.Instruction{instr.MustNew(instr.DROP), instr.MustNew(instr.RETURN)}),
				}},
			)),
			wantErr:  program.ErrStackUnderflow,
			wantSlot: 2,
		},
		{
			name: "constants/invalid function nested in a map and struct literal",
			prog: program.New([]instr.Instruction{instr.MustNew(instr.NOP)}, program.WithConstants(
				&types.MapLiteral{
					Typ:  types.NewMapType(types.TypeI32(), types.TypeAny()),
					Keys: []types.Value{types.I32(1)},
					Values: []types.Value{&types.StructLiteral{
						Typ: types.NewStructType(types.NewStructField(types.TypeAny())),
						Fields: []types.Value{
							types.NewFunction(nil, nil, []instr.Instruction{instr.MustNew(instr.DROP), instr.MustNew(instr.RETURN)}),
						},
					}},
				},
			)),
			wantErr:  program.ErrStackUnderflow,
			wantSlot: 1,
		},
		{
			name:    "nil program",
			prog:    nil,
			wantErr: program.ErrNil,
		},
	}

	cyclic := &types.ArrayLiteral{Typ: types.NewArrayType(types.TypeAny())}
	cyclic.Elems = []types.Value{cyclic}
	tests = append(tests, struct {
		name     string
		prog     *program.Program
		wantErr  error
		wantSlot int
	}{name: "cyclic constant literal", prog: program.New(nil, program.WithConstants(cyclic))})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := program.Verify(tt.prog)
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.wantErr)
			if tt.wantSlot > 0 {
				var ve *program.VerifyError
				require.ErrorAs(t, err, &ve)
				require.Equal(t, tt.wantSlot, ve.Slot)
			}
		})
	}
}

func TestSnapshot(t *testing.T) {
	t.Run("returns a verified copy", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.MustNew(instr.NOP)})
		snap, err := program.Snapshot(prog)
		require.NoError(t, err)
		require.Equal(t, prog.Code, snap.Code)

		prog.Code[0] = byte(instr.UNREACHABLE)
		require.Equal(t, byte(instr.NOP), snap.Code[0])
	})

	t.Run("rejects a malformed program", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.MustNew(instr.DROP)})
		snap, err := program.Snapshot(prog)
		var verr *program.VerifyError
		require.ErrorAs(t, err, &verr)
		require.Nil(t, snap)
	})

	t.Run("rejects a nil program", func(t *testing.T) {
		_, err := program.Snapshot(nil)
		require.ErrorIs(t, err, program.ErrNil)
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

func FuzzVerify(f *testing.F) {
	f.Add([]byte(nil))
	f.Add([]byte{byte(instr.NOP)})
	f.Add([]byte{byte(instr.I32_CONST), 1})
	f.Add([]byte{0xff})

	f.Fuzz(func(t *testing.T, code []byte) {
		if len(code) > 4096 {
			t.Skip()
		}
		_ = program.Verify(&program.Program{Code: code})
	})
}
