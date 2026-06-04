package types

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/stretchr/testify/require"
)

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
			Build()
		require.Contains(t, fn.String(), "capture i32\ncapture ref\n")
	})
}

func TestFunction_LocalKinds(t *testing.T) {
	tests := []struct {
		name string
		fn   *Function
		want []Kind
	}{
		{name: "none", fn: NewFunction(nil, nil, nil)},
		{name: "locals only", fn: NewFunction(nil, []Type{TypeI32, TypeRef}, nil), want: []Kind{KindI32, KindRef}},
		{name: "params only", fn: NewFunction(&FunctionType{Params: []Type{TypeI64}}, nil, nil), want: []Kind{KindI64}},
		{name: "params and locals", fn: NewFunction(&FunctionType{Params: []Type{TypeI64}}, []Type{TypeF32}, nil), want: []Kind{KindI64, KindF32}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.fn.LocalKinds())
		})
	}
}

func TestFunctionBuilder_WithParams(t *testing.T) {
	fn := NewFunctionBuilder(nil).WithParams(TypeI32, TypeRef).Build()
	require.Equal(t, []Type{TypeI32, TypeRef}, fn.Typ.Params)
}

func TestFunctionBuilder_WithReturns(t *testing.T) {
	fn := NewFunctionBuilder(nil).WithReturns(TypeI32, TypeRef).Build()
	require.Equal(t, []Type{TypeI32, TypeRef}, fn.Typ.Returns)
}

func TestFunctionBuilder_WithCaptures(t *testing.T) {
	fn := NewFunctionBuilder(nil).WithCaptures(TypeI32, TypeF64).Build()
	require.Equal(t, []Type{TypeI32, TypeF64}, fn.Captures)
}

func TestFunctionType_Kind(t *testing.T) {
	require.Equal(t, KindRef, (&FunctionType{}).Kind())
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
