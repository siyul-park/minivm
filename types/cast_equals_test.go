package types

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/stretchr/testify/require"
)

func TestPrimitive_Cast(t *testing.T) {
	tests := []struct {
		typ   Type
		other Type
		want  bool
	}{
		{TypeI32, TypeI32, true},
		{TypeI32, TypeI64, false},
		{TypeI64, TypeI64, true},
		{TypeI64, TypeF32, false},
		{TypeF32, TypeF32, true},
		{TypeF32, TypeF64, false},
		{TypeF64, TypeF64, true},
		{TypeF64, TypeI32, false},
		// refType.Cast always returns true
		{TypeRef, TypeRef, true},
		{TypeRef, TypeI32, true},
	}
	for _, tt := range tests {
		t.Run(tt.typ.String()+"_"+tt.other.String(), func(t *testing.T) {
			require.Equal(t, tt.want, tt.typ.Cast(tt.other))
		})
	}
}

func TestPrimitive_Equals(t *testing.T) {
	tests := []struct {
		typ   Type
		other Type
		want  bool
	}{
		{TypeI32, TypeI32, true},
		{TypeI32, TypeI64, false},
		{TypeI64, TypeI64, true},
		{TypeF32, TypeF32, true},
		{TypeF32, TypeI32, false},
		{TypeF64, TypeF64, true},
		{TypeF64, TypeRef, false},
		{TypeRef, TypeRef, true},
		{TypeRef, TypeF64, false},
	}
	for _, tt := range tests {
		t.Run(tt.typ.String()+"_"+tt.other.String(), func(t *testing.T) {
			require.Equal(t, tt.want, tt.typ.Equals(tt.other))
		})
	}
}

func TestBool(t *testing.T) {
	require.Equal(t, I32(1), Bool(true))
	require.Equal(t, I32(0), Bool(false))
}

func TestBoxBool(t *testing.T) {
	require.Equal(t, BoxedTrue, BoxBool(true))
	require.Equal(t, BoxedFalse, BoxBool(false))
}

func TestBoxed_Bool(t *testing.T) {
	require.True(t, BoxI32(1).Bool())
	require.False(t, BoxI32(0).Bool())
}

func TestStringType_Kind(t *testing.T) {
	require.Equal(t, KindRef, TypeString.Kind())
}

func TestStringType_Cast(t *testing.T) {
	require.True(t, TypeString.Cast(TypeString))
	require.False(t, TypeString.Cast(TypeI32))
}

func TestStringType_Equals(t *testing.T) {
	require.True(t, TypeString.Equals(TypeString))
	require.False(t, TypeString.Equals(TypeI32))
}

func TestArrayType_Kind(t *testing.T) {
	require.Equal(t, KindRef, TypeI32Array.Kind())
}

func TestArrayType_Cast(t *testing.T) {
	require.True(t, TypeI32Array.Cast(TypeI32Array))
	require.False(t, TypeI32Array.Cast(TypeI64Array))
}

func TestArrayType_Equals(t *testing.T) {
	require.True(t, TypeI32Array.Equals(TypeI32Array))
	require.False(t, TypeI32Array.Equals(TypeF32Array))
	require.True(t, NewArrayType(TypeI32).Equals(TypeI32Array))
}

func TestArray_Refs(t *testing.T) {
	arr := NewArray(NewArrayType(TypeRef), BoxRef(1), BoxRef(2), BoxI32(0))
	refs := arr.Refs()
	require.Equal(t, []Ref{1, 2}, refs)
}

func TestArray_Refs_Empty(t *testing.T) {
	arr := NewArray(NewArrayType(TypeI32), BoxI32(10), BoxI32(20))
	refs := arr.Refs()
	require.Empty(t, refs)
}

func TestFunctionBuilder(t *testing.T) {
	b := NewFunctionBuilder(nil)
	b.WithParams(TypeI32, TypeI64)
	b.WithReturns(TypeF64)
	b.WithLocals(TypeRef)
	b.Emit(instr.New(instr.NOP))

	fn := b.Build()
	require.NotNil(t, fn)
	require.Equal(t, []Type{TypeI32, TypeI64}, fn.Typ.Params)
	require.Equal(t, []Type{TypeF64}, fn.Typ.Returns)
	require.Equal(t, []Type{TypeRef}, fn.Locals)
	require.NotEmpty(t, fn.Code)
}

func TestFunctionType_Kind(t *testing.T) {
	ft := &FunctionType{}
	require.Equal(t, KindRef, ft.Kind())
}

func TestFunctionType_Cast(t *testing.T) {
	ft1 := &FunctionType{Params: []Type{TypeI32}, Returns: []Type{TypeI64}}
	ft2 := &FunctionType{Params: []Type{TypeI32}, Returns: []Type{TypeI64}}
	ft3 := &FunctionType{Params: []Type{TypeF32}}

	require.True(t, ft1.Cast(ft1))
	require.True(t, ft1.Cast(ft2))
	require.False(t, ft1.Cast(ft3))
	require.False(t, ft1.Cast(TypeI32))
}

func TestFunctionType_Equals(t *testing.T) {
	ft1 := &FunctionType{Params: []Type{TypeI32}}
	ft2 := &FunctionType{Params: []Type{TypeI32}}
	ft3 := &FunctionType{Params: []Type{TypeF64}}

	require.True(t, ft1.Equals(ft1))
	require.True(t, ft1.Equals(ft2))
	require.False(t, ft1.Equals(ft3))
	require.False(t, ft1.Equals(TypeI32))
}

func TestStructType_Kind(t *testing.T) {
	st := NewStructType()
	require.Equal(t, KindRef, st.Kind())
}

func TestStructType_Cast(t *testing.T) {
	st1 := NewStructType(NewStructField(TypeI32))
	// StructType.Cast: o==other is always true after type assertion, so any *StructType returns true
	require.True(t, st1.Cast(st1))
	require.False(t, st1.Cast(TypeI32))
}

func TestStructType_Equals(t *testing.T) {
	st1 := NewStructType(NewStructField(TypeI32))
	st2 := NewStructType(NewStructField(TypeI32))
	st3 := NewStructType()

	require.True(t, st1.Equals(st1))
	require.True(t, st1.Equals(st2))
	require.False(t, st1.Equals(st3))
	require.False(t, st1.Equals(TypeI32))
}

func TestStruct_Refs(t *testing.T) {
	f := NewStructField(TypeRef)
	st := NewStructType(f)
	s := NewStruct(st, BoxRef(5))
	refs := s.Refs()
	require.Equal(t, []Ref{5}, refs)
}

func TestStruct_Refs_NoRefFields(t *testing.T) {
	f := NewStructField(TypeI32)
	st := NewStructType(f)
	s := NewStruct(st, BoxI32(42))
	refs := s.Refs()
	require.Empty(t, refs)
}

func TestKind_String(t *testing.T) {
	tests := []struct {
		kind Kind
		want string
	}{
		{KindI32, "i32"},
		{KindI64, "i64"},
		{KindF32, "f32"},
		{KindF64, "f64"},
		{KindRef, "ref"},
		{Kind(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			require.Equal(t, tt.want, tt.kind.String())
		})
	}
}
