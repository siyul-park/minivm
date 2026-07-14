package types

import (
	"fmt"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/stretchr/testify/require"
)

func TestNewFunctionBuilder(t *testing.T) {
	b := NewFunctionBuilder(nil)
	fn, err := b.Build()
	require.NoError(t, err)
	require.Equal(t, &FunctionType{}, fn.Typ)
}

func TestFunctionBuilder_WithParams(t *testing.T) {
	fn := NewFunctionBuilder(nil).WithParams(TypeI32, TypeRef).MustBuild()
	require.Equal(t, []Type{TypeI32, TypeRef}, fn.Typ.Params)
}

func TestFunctionBuilder_WithReturns(t *testing.T) {
	fn := NewFunctionBuilder(nil).WithReturns(TypeI32, TypeRef).MustBuild()
	require.Equal(t, []Type{TypeI32, TypeRef}, fn.Typ.Returns)
}

func TestFunctionBuilder_WithLocals(t *testing.T) {
	fn := NewFunctionBuilder(nil).WithLocals(TypeI32, TypeRef).MustBuild()
	require.Equal(t, []Type{TypeI32, TypeRef}, fn.Locals)
}

func TestFunctionBuilder_WithCaptures(t *testing.T) {
	fn := NewFunctionBuilder(nil).WithCaptures(TypeI32, TypeF64).MustBuild()
	require.Equal(t, []Type{TypeI32, TypeF64}, fn.Captures)
}

func TestFunctionBuilder_Emit(t *testing.T) {
	fn := NewFunctionBuilder(nil).Emit(instr.New(instr.I32_CONST, 42), instr.New(instr.RETURN)).MustBuild()
	require.Equal(t, []instr.Instruction{instr.New(instr.I32_CONST, 42), instr.New(instr.RETURN)}, instr.Unmarshal(fn.Code))
}

func TestFunctionBuilder_Label(t *testing.T) {
	b := NewFunctionBuilder(nil)
	require.NotEqual(t, b.Label(), b.Label())
}

func TestFunctionBuilder_Bind(t *testing.T) {
	b := NewFunctionBuilder(nil)
	end := b.Label()
	require.Same(t, b, b.Br(end).Emit(instr.New(instr.NOP)).Bind(end).Emit(instr.New(instr.RETURN)))
	fn := b.MustBuild()
	require.Equal(t, 1, instr.ParseI16(fn.Code, 1))
}

func TestFunctionBuilder_Br(t *testing.T) {
	b := NewFunctionBuilder(nil)
	end := b.Label()
	fn := b.Br(end).Emit(instr.New(instr.NOP)).Bind(end).MustBuild()
	require.Equal(t, instr.BR, instr.Instruction(fn.Code).Opcode())
}

func TestFunctionBuilder_BrIf(t *testing.T) {
	b := NewFunctionBuilder(nil)
	end := b.Label()
	fn := b.BrIf(end).Emit(instr.New(instr.NOP)).Bind(end).MustBuild()
	require.Equal(t, instr.BR_IF, instr.Instruction(fn.Code).Opcode())
}

func TestFunctionBuilder_BrTable(t *testing.T) {
	b := NewFunctionBuilder(nil)
	first, def := b.Label(), b.Label()
	fn := b.BrTable(def, first).Bind(first).Emit(instr.New(instr.NOP)).Bind(def).MustBuild()
	require.Equal(t, instr.BR_TABLE, instr.Instruction(fn.Code).Opcode())
	require.Equal(t, []uint64{1, 0, 1}, instr.Instruction(fn.Code).Operands())
}

func TestFunctionBuilder_Try(t *testing.T) {
	b := NewFunctionBuilder(nil)
	start, end, catch := b.Label(), b.Label(), b.Label()
	require.Same(t, b, b.Bind(start).Emit(instr.New(instr.NOP)).Bind(end).Emit(instr.New(instr.RETURN)).Bind(catch).Try(start, end, catch, 2))
	fn := b.MustBuild()
	require.Equal(t, []instr.Handler{{Start: 0, End: 1, Catch: 2, Depth: 2}}, fn.Handlers)
}

func TestFunctionBuilder_MustBuild(t *testing.T) {
	t.Run("valid body", func(t *testing.T) {
		require.NotNil(t, NewFunctionBuilder(nil).MustBuild())
	})

	t.Run("invalid body", func(t *testing.T) {
		b := NewFunctionBuilder(nil)
		b.Br(b.Label())
		require.Panics(t, func() { b.MustBuild() })
	})
}

func TestFunctionBuilder_Build(t *testing.T) {
	t.Run("valid body", func(t *testing.T) {
		fn, err := NewFunctionBuilder(nil).Emit(instr.New(instr.RETURN)).Build()
		require.NoError(t, err)
		require.Equal(t, []instr.Instruction{instr.New(instr.RETURN)}, instr.Unmarshal(fn.Code))
	})

	t.Run("unbound label", func(t *testing.T) {
		b := NewFunctionBuilder(nil)
		b.Br(b.Label())
		fn, err := b.Build()
		require.Nil(t, fn)
		require.ErrorIs(t, err, instr.ErrUnboundLabel)
	})
}

func TestNewFunction(t *testing.T) {
	typ := &FunctionType{Params: []Type{TypeI32}, Returns: []Type{TypeI64}}
	locals := []Type{TypeRef}
	body := []instr.Instruction{instr.New(instr.RETURN)}
	fn := NewFunction(typ, locals, body)
	require.Same(t, typ, fn.Typ)
	require.Equal(t, locals, fn.Locals)
	require.Equal(t, body, instr.Unmarshal(fn.Code))

	require.Equal(t, &FunctionType{}, NewFunction(nil, nil, nil).Typ)
}

func TestFunction_Kind(t *testing.T) {
	fn := NewFunction(nil, nil, nil)
	require.Equal(t, KindRef, fn.Kind())
}

func TestFunction_Type(t *testing.T) {
	fn := NewFunction(nil, nil, nil)
	require.Equal(t, &FunctionType{}, fn.Type())
}

func TestFunction_String(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		fn := NewFunction(nil, nil, nil)
		require.Equal(t, "func()\n", fn.String())
	})

	t.Run("with captures", func(t *testing.T) {
		fn := NewFunctionBuilder(&FunctionType{Returns: []Type{TypeI32}}).
			WithCaptures(TypeI32, TypeRef).
			WithLocals(TypeI64).
			Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.RETURN)).
			MustBuild()
		require.Contains(t, fn.String(), "capture i32\ncapture ref\n")
	})
}

func TestFunction_LocalKinds(t *testing.T) {
	tests := []struct {
		fn   *Function
		want []Kind
	}{
		{fn: NewFunction(nil, nil, nil)},
		{fn: NewFunction(nil, []Type{TypeI32, TypeRef}, nil), want: []Kind{KindI32, KindRef}},
		{fn: NewFunction(&FunctionType{Params: []Type{TypeI64}}, nil, nil), want: []Kind{KindI64}},
		{fn: NewFunction(&FunctionType{Params: []Type{TypeI64}}, []Type{TypeF32}, nil), want: []Kind{KindI64, KindF32}},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("params=%v,locals=%v", tt.fn.Typ.Params, tt.fn.Locals), func(t *testing.T) {
			require.Equal(t, tt.want, tt.fn.LocalKinds())
		})
	}
}

func TestFunctionType_Kind(t *testing.T) {
	require.Equal(t, KindRef, (&FunctionType{}).Kind())
}

func TestFunctionType_String(t *testing.T) {
	tests := []struct {
		typ  *FunctionType
		want string
	}{
		{typ: &FunctionType{}, want: "func()"},
		{typ: &FunctionType{Params: []Type{TypeI32}, Returns: []Type{TypeI64}}, want: "func(i32) i64"},
		{typ: &FunctionType{Params: []Type{TypeI32, TypeRef}, Returns: []Type{TypeI64, TypeF32}}, want: "func(i32, ref) (i64, f32)"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("params=%v,returns=%v", tt.typ.Params, tt.typ.Returns), func(t *testing.T) {
			require.Equal(t, tt.want, tt.typ.String())
		})
	}
}

func TestFunctionType_Cast(t *testing.T) {
	typ := &FunctionType{Params: []Type{TypeI32}, Returns: []Type{TypeRef}}

	require.True(t, typ.Cast(&FunctionType{Params: []Type{TypeI32}, Returns: []Type{TypeRef}}))
	require.False(t, typ.Cast(&FunctionType{Params: []Type{TypeI64}, Returns: []Type{TypeRef}}))
	require.False(t, typ.Cast(TypeI32))
}

func TestFunctionType_Equals(t *testing.T) {
	typ := &FunctionType{Params: []Type{TypeI32}, Returns: []Type{TypeRef}}

	require.True(t, typ.Equals(typ))
	require.True(t, typ.Equals(&FunctionType{Params: []Type{TypeI32}, Returns: []Type{TypeRef}}))
	require.False(t, typ.Equals(&FunctionType{Params: []Type{TypeI64}, Returns: []Type{TypeRef}}))
	require.False(t, typ.Equals(&FunctionType{Params: []Type{TypeI32}, Returns: []Type{TypeI64}}))
	require.False(t, typ.Equals(&FunctionType{Params: []Type{TypeI32}}))
	require.False(t, typ.Equals(TypeI32))
}
