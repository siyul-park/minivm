package types_test

import (
	"fmt"
	"testing"

	"github.com/siyul-park/minivm/instr"
	types "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestNewFunctionBuilder(t *testing.T) {
	b := types.NewFunctionBuilder(nil)
	fn, err := b.Build()
	require.NoError(t, err)
	require.Equal(t, &types.FunctionType{}, fn.Typ)
}

func TestFunctionBuilder_Params(t *testing.T) {
	fn := types.NewFunctionBuilder(nil).Params(types.TypeI32, types.TypeAny).MustBuild()
	require.Equal(t, []types.Type{types.TypeI32, types.TypeAny}, fn.Typ.Params)
}

func TestFunctionBuilder_Returns(t *testing.T) {
	fn := types.NewFunctionBuilder(nil).Returns(types.TypeI32, types.TypeAny).MustBuild()
	require.Equal(t, []types.Type{types.TypeI32, types.TypeAny}, fn.Typ.Returns)
}

func TestFunctionBuilder_Locals(t *testing.T) {
	fn := types.NewFunctionBuilder(nil).Locals(types.TypeI32, types.TypeAny).MustBuild()
	require.Equal(t, []types.Type{types.TypeI32, types.TypeAny}, fn.Locals)
}

func TestFunctionBuilder_Captures(t *testing.T) {
	fn := types.NewFunctionBuilder(nil).Captures(types.TypeI32, types.TypeF64).MustBuild()
	require.Equal(t, []types.Type{types.TypeI32, types.TypeF64}, fn.Captures)
}

func TestFunctionBuilder_Emit(t *testing.T) {
	fn := types.NewFunctionBuilder(nil).Emit(instr.New(instr.I32_CONST, 42), instr.New(instr.RETURN)).MustBuild()
	require.Equal(t, []instr.Instruction{instr.New(instr.I32_CONST, 42), instr.New(instr.RETURN)}, instr.Unmarshal(fn.Code))
}

func TestFunctionBuilder_Label(t *testing.T) {
	b := types.NewFunctionBuilder(nil)
	require.NotEqual(t, b.Label(), b.Label())
}

func TestFunctionBuilder_Bind(t *testing.T) {
	b := types.NewFunctionBuilder(nil)
	end := b.Label()
	require.Same(t, b, b.Br(end).Emit(instr.New(instr.NOP)).Bind(end).Emit(instr.New(instr.RETURN)))
	fn := b.MustBuild()
	require.Equal(t, 1, instr.ParseI16(fn.Code, 1))
}

func TestFunctionBuilder_Br(t *testing.T) {
	b := types.NewFunctionBuilder(nil)
	end := b.Label()
	fn := b.Br(end).Emit(instr.New(instr.NOP)).Bind(end).MustBuild()
	require.Equal(t, instr.BR, instr.Instruction(fn.Code).Opcode())
}

func TestFunctionBuilder_BrIf(t *testing.T) {
	b := types.NewFunctionBuilder(nil)
	end := b.Label()
	fn := b.BrIf(end).Emit(instr.New(instr.NOP)).Bind(end).MustBuild()
	require.Equal(t, instr.BR_IF, instr.Instruction(fn.Code).Opcode())
}

func TestFunctionBuilder_BrTable(t *testing.T) {
	b := types.NewFunctionBuilder(nil)
	first, def := b.Label(), b.Label()
	fn := b.BrTable(def, first).Bind(first).Emit(instr.New(instr.NOP)).Bind(def).MustBuild()
	require.Equal(t, instr.BR_TABLE, instr.Instruction(fn.Code).Opcode())
	require.Equal(t, []uint64{1, 0, 1}, instr.Instruction(fn.Code).Operands())
}

func TestFunctionBuilder_Try(t *testing.T) {
	b := types.NewFunctionBuilder(nil)
	start, end, catch := b.Label(), b.Label(), b.Label()
	require.Same(t, b, b.Bind(start).Emit(instr.New(instr.NOP)).Bind(end).Emit(instr.New(instr.RETURN)).Bind(catch).Try(start, end, catch, 2))
	fn := b.MustBuild()
	require.Equal(t, []instr.Handler{{Start: 0, End: 1, Catch: 2, Depth: 2}}, fn.Handlers)
}

func TestFunctionBuilder_MustBuild(t *testing.T) {
	t.Run("valid body", func(t *testing.T) {
		require.NotNil(t, types.NewFunctionBuilder(nil).MustBuild())
	})

	t.Run("invalid body", func(t *testing.T) {
		b := types.NewFunctionBuilder(nil)
		b.Br(b.Label())
		require.Panics(t, func() { b.MustBuild() })
	})
}

func TestFunctionBuilder_Build(t *testing.T) {
	t.Run("valid body", func(t *testing.T) {
		fn, err := types.NewFunctionBuilder(nil).Emit(instr.New(instr.RETURN)).Build()
		require.NoError(t, err)
		require.Equal(t, []instr.Instruction{instr.New(instr.RETURN)}, instr.Unmarshal(fn.Code))
	})

	t.Run("unbound label", func(t *testing.T) {
		b := types.NewFunctionBuilder(nil)
		b.Br(b.Label())
		fn, err := b.Build()
		require.Nil(t, fn)
		require.ErrorIs(t, err, instr.ErrUnboundLabel)
	})
}

func TestNewFunction(t *testing.T) {
	typ := &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI64}}
	locals := []types.Type{types.TypeAny}
	body := []instr.Instruction{instr.New(instr.RETURN)}
	fn := types.NewFunction(typ, locals, body)
	require.Same(t, typ, fn.Typ)
	require.Equal(t, locals, fn.Locals)
	require.Equal(t, body, instr.Unmarshal(fn.Code))

	require.Equal(t, &types.FunctionType{}, types.NewFunction(nil, nil, nil).Typ)
}

func TestFunction_Kind(t *testing.T) {
	fn := types.NewFunction(nil, nil, nil)
	require.Equal(t, types.KindRef, fn.Kind())
}

func TestFunction_Type(t *testing.T) {
	fn := types.NewFunction(nil, nil, nil)
	require.Equal(t, &types.FunctionType{}, fn.Type())
}

func TestFunction_String(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		fn := types.NewFunction(nil, nil, nil)
		require.Equal(t, "func()\n", fn.String())
	})

	t.Run("with captures", func(t *testing.T) {
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Captures(types.TypeI32, types.TypeAny).
			Locals(types.TypeI64).
			Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.RETURN)).
			MustBuild()
		require.Contains(t, fn.String(), "capture i32\ncapture any\n")
	})
}

func TestFunction_Slots(t *testing.T) {
	tests := []struct {
		fn   *types.Function
		want []types.Kind
	}{
		{fn: types.NewFunction(nil, nil, nil)},
		{fn: types.NewFunction(nil, []types.Type{types.TypeI32, types.TypeAny}, nil), want: []types.Kind{types.KindI32, types.KindRef}},
		{fn: types.NewFunction(&types.FunctionType{Params: []types.Type{types.TypeI64}}, nil, nil), want: []types.Kind{types.KindI64}},
		{fn: types.NewFunction(&types.FunctionType{Params: []types.Type{types.TypeI64}}, []types.Type{types.TypeF32}, nil), want: []types.Kind{types.KindI64, types.KindF32}},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("params=%v,locals=%v", tt.fn.Typ.Params, tt.fn.Locals), func(t *testing.T) {
			require.Equal(t, tt.want, tt.fn.Slots())
		})
	}
}

func TestFunction_Declared(t *testing.T) {
	tests := []struct {
		fn   *types.Function
		want []types.Type
	}{
		{fn: types.NewFunction(nil, nil, nil)},
		{fn: types.NewFunction(nil, []types.Type{types.TypeI32, types.TypeAny}, nil), want: []types.Type{types.TypeI32, types.TypeAny}},
		{fn: types.NewFunction(&types.FunctionType{Params: []types.Type{types.TypeI64}}, nil, nil), want: []types.Type{types.TypeI64}},
		{fn: types.NewFunction(&types.FunctionType{Params: []types.Type{types.TypeI64}}, []types.Type{types.TypeF32}, nil), want: []types.Type{types.TypeI64, types.TypeF32}},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("params=%v,locals=%v", tt.fn.Typ.Params, tt.fn.Locals), func(t *testing.T) {
			require.Equal(t, tt.want, tt.fn.Declared())
		})
	}
}

func TestFunctionType_Kind(t *testing.T) {
	require.Equal(t, types.KindRef, (&types.FunctionType{}).Kind())
}

func TestFunctionType_String(t *testing.T) {
	tests := []struct {
		typ  *types.FunctionType
		want string
	}{
		{typ: &types.FunctionType{}, want: "func()"},
		{typ: &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI64}}, want: "func(i32) i64"},
		{typ: &types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeAny}, Returns: []types.Type{types.TypeI64, types.TypeF32}}, want: "func(i32, any) (i64, f32)"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("params=%v,returns=%v", tt.typ.Params, tt.typ.Returns), func(t *testing.T) {
			require.Equal(t, tt.want, tt.typ.String())
		})
	}
}

func TestFunctionType_Cast(t *testing.T) {
	typ := &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeAny}}

	require.True(t, typ.Cast(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeAny}}))
	require.False(t, typ.Cast(&types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeAny}}))
	require.False(t, typ.Cast(types.TypeI32))
}

func TestFunctionType_Equals(t *testing.T) {
	typ := &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeAny}}

	require.True(t, typ.Equals(typ))
	require.True(t, typ.Equals(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeAny}}))
	require.False(t, typ.Equals(&types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeAny}}))
	require.False(t, typ.Equals(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI64}}))
	require.False(t, typ.Equals(&types.FunctionType{Params: []types.Type{types.TypeI32}}))
	require.False(t, typ.Equals(types.TypeI32))
}
