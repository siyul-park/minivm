package program_test

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	program "github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestWithConstants(t *testing.T) {
	prog := program.New(nil, program.WithConstants(types.I32(1), types.I64(2)))
	require.Len(t, prog.Constants, 2)
	require.Equal(t, types.I32(1), prog.Constants[0])
	require.Equal(t, types.I64(2), prog.Constants[1])
}

func TestWithTypes(t *testing.T) {
	prog := program.New(nil, program.WithTypes(types.TypeI32(), types.TypeI64()))
	require.Len(t, prog.Types, 2)
	require.Equal(t, types.TypeI32(), prog.Types[0])
	require.Equal(t, types.TypeI64(), prog.Types[1])
}

func TestWithLocals(t *testing.T) {
	prog := program.New(nil, program.WithLocals(types.TypeI32(), types.TypeI64()))
	require.Len(t, prog.Locals, 2)
	require.Equal(t, types.TypeI32(), prog.Locals[0])
	require.Equal(t, types.TypeI64(), prog.Locals[1])
}

func TestWithGlobals(t *testing.T) {
	prog := program.New(nil, program.WithGlobals(types.TypeI32(), types.TypeAny()))
	require.Equal(t, []types.Type{types.TypeI32(), types.TypeAny()}, prog.Globals)
}

func TestWithHandlers(t *testing.T) {
	h := instr.Handler{Start: 0, End: 10, Catch: 20, Depth: 1}
	prog := program.New(nil, program.WithHandlers(h))
	require.Len(t, prog.Handlers, 1)
	require.Equal(t, h, prog.Handlers[0])
}

func TestNew(t *testing.T) {
	body := []instr.Instruction{instr.MustNew(instr.I32_CONST, 42), instr.MustNew(instr.DROP)}
	prog := program.New(body, program.WithLocals(types.TypeI32()), program.WithGlobals(types.TypeAny()))
	require.Equal(t, body, instr.Unmarshal(prog.Code))
	require.Equal(t, []types.Type{types.TypeI32()}, prog.Locals)
	require.Equal(t, []types.Type{types.TypeAny()}, prog.Globals)
}

func TestProgram_Clone(t *testing.T) {
	t.Run("nil receiver", func(t *testing.T) {
		var prog *program.Program
		require.Nil(t, prog.Clone())
	})

	t.Run("copies program slices", func(t *testing.T) {
		h := instr.Handler{Start: 0, End: 1, Catch: 2}
		prog := program.New([]instr.Instruction{instr.MustNew(instr.NOP)},
			program.WithLocals(types.TypeI32()),
			program.WithGlobals(types.TypeI64()),
			program.WithConstants(types.I32(1)),
			program.WithTypes(types.TypeF32()),
			program.WithHandlers(h),
		)
		clone := prog.Clone()
		require.Equal(t, prog, clone)

		clone.Code[0] = byte(instr.DROP)
		clone.Locals[0] = types.TypeI64()
		clone.Globals[0] = types.TypeI32()
		clone.Constants[0] = types.I32(2)
		clone.Types[0] = types.TypeF64()
		clone.Handlers[0].Catch = 9
		require.Equal(t, byte(instr.NOP), prog.Code[0])
		require.Equal(t, types.TypeI32(), prog.Locals[0])
		require.Equal(t, types.TypeI64(), prog.Globals[0])
		require.Equal(t, types.I32(1), prog.Constants[0])
		require.Equal(t, types.TypeF32(), prog.Types[0])
		require.Equal(t, 2, prog.Handlers[0].Catch)
	})

	t.Run("copies function constants", func(t *testing.T) {
		typ := &types.FunctionType{Returns: []types.Type{types.TypeI32()}}
		fn := &types.Function{
			Typ:      typ,
			Locals:   []types.Type{types.TypeI32()},
			Captures: []types.Type{types.TypeI64()},
			Code:     instr.Marshal([]instr.Instruction{instr.MustNew(instr.NOP)}),
			Handlers: []instr.Handler{{Start: 0, End: 1, Catch: 2}},
		}
		prog := program.New(nil, program.WithConstants(fn))
		clone := prog.Clone()

		got, ok := clone.Constants[0].(*types.Function)
		require.True(t, ok)
		require.NotSame(t, fn, got)
		require.Same(t, typ, got.Typ)
		require.Equal(t, fn, got)

		got.Code[0] = byte(instr.DROP)
		got.Locals[0] = types.TypeI64()
		got.Captures[0] = types.TypeI32()
		got.Handlers[0].Catch = 9
		require.Equal(t, byte(instr.NOP), fn.Code[0])
		require.Equal(t, types.TypeI32(), fn.Locals[0])
		require.Equal(t, types.TypeI64(), fn.Captures[0])
		require.Equal(t, 2, fn.Handlers[0].Catch)

		fn.Code[0] = byte(instr.RETURN)
		require.Equal(t, byte(instr.DROP), got.Code[0])
	})

	t.Run("copies functions nested in literals", func(t *testing.T) {
		fn := types.NewFunction(nil, nil, []instr.Instruction{instr.MustNew(instr.NOP)})
		lit := &types.ArrayLiteral{Typ: types.NewArrayType(types.TypeAny()), Elems: []types.Value{fn}}
		prog := program.New(nil, program.WithConstants(lit))
		clone := prog.Clone()

		got, ok := clone.Constants[0].(*types.ArrayLiteral)
		require.True(t, ok)
		require.NotSame(t, lit, got)
		nested := got.Elems[0].(*types.Function)
		require.NotSame(t, fn, nested)
		require.Equal(t, fn, nested)

		nested.Code[0] = byte(instr.DROP)
		got.Elems[0] = types.I32(1)
		require.Equal(t, byte(instr.NOP), fn.Code[0])
		require.Same(t, fn, lit.Elems[0])
	})

	t.Run("copies raw mutable constants in literals", func(t *testing.T) {
		raw := types.NewArray(types.NewArrayType(types.TypeI32()), types.BoxI32(1))
		lit := &types.StructLiteral{
			Typ:    types.NewStructType(types.NewStructField(raw.Type())),
			Fields: []types.Value{raw},
		}
		clone := program.New(nil, program.WithConstants(lit)).Clone()

		got := clone.Constants[0].(*types.StructLiteral).Fields[0].(*types.Array)
		require.NotSame(t, raw, got)
		got.Elems[0] = types.BoxI32(9)
		require.Equal(t, types.BoxI32(1), raw.Elems[0])
	})

	t.Run("keeps aliasing", func(t *testing.T) {
		fn := types.NewFunction(nil, nil, []instr.Instruction{instr.MustNew(instr.NOP)})
		raw := types.NewArray(types.NewArrayType(types.TypeI32()), types.BoxI32(1))
		typed := types.TypedArray[int32]{1}
		lit := &types.ArrayLiteral{Typ: types.NewArrayType(types.TypeAny()), Elems: []types.Value{fn, raw, typed}}
		prog := program.New(nil, program.WithConstants(fn, raw, typed, lit, lit))
		clone := prog.Clone()

		top := clone.Constants
		require.Same(t, top[3], top[4])
		elems := top[3].(*types.ArrayLiteral).Elems
		require.Same(t, top[0], elems[0])
		require.Same(t, top[1], elems[1])
		require.NotSame(t, fn, top[0])
		top[2].(types.TypedArray[int32])[0] = 7
		require.Equal(t, int32(7), elems[2].(types.TypedArray[int32])[0])
		require.Equal(t, int32(1), typed[0])
	})
}

func TestProgram_String(t *testing.T) {
	t.Run("with code", func(t *testing.T) {
		prog := program.New([]instr.Instruction{instr.MustNew(instr.NOP)})
		require.Equal(t, ".code\n0000:\tnop\n", prog.String())
	})
	t.Run("empty", func(t *testing.T) {
		prog := program.New(nil)
		require.Equal(t, ".code\n", prog.String())
	})
	t.Run("with handlers", func(t *testing.T) {
		prog := program.New(nil, program.WithHandlers(instr.Handler{Start: 0, End: 10, Catch: 20, Depth: 1}))
		require.Contains(t, prog.String(), ".handlers")
		require.Contains(t, prog.String(), "start=0")
		require.Contains(t, prog.String(), "end=10")
		require.Contains(t, prog.String(), "catch=20")
		require.Contains(t, prog.String(), "depth=1")
	})
}
