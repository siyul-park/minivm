package types

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBool(t *testing.T) {
	require.Equal(t, True, Bool(true))
	require.Equal(t, False, Bool(false))
}

func TestI1_Kind(t *testing.T) {
	require.Equal(t, KindI1, I1(false).Kind())
}

func TestI1_Type(t *testing.T) {
	require.Equal(t, TypeI1, I1(false).Type())
}

func TestI1_String(t *testing.T) {
	require.Equal(t, "false", I1(false).String())
	require.Equal(t, "true", I1(true).String())
}

func TestI8_Kind(t *testing.T) {
	require.Equal(t, KindI8, I8(0).Kind())
}

func TestI8_Type(t *testing.T) {
	require.Equal(t, TypeI8, I8(0).Type())
}

func TestI8_String(t *testing.T) {
	require.Equal(t, "-8", I8(-8).String())
}

func TestI32_Kind(t *testing.T) {
	require.Equal(t, KindI32, I32(0).Kind())
}

func TestI32_Type(t *testing.T) {
	require.Equal(t, TypeI32, I32(0).Type())
}

func TestI32_String(t *testing.T) {
	require.Equal(t, "-32", I32(-32).String())
}

func TestI64_Kind(t *testing.T) {
	require.Equal(t, KindI64, I64(0).Kind())
}

func TestI64_Type(t *testing.T) {
	require.Equal(t, TypeI64, I64(0).Type())
}

func TestI64_String(t *testing.T) {
	require.Equal(t, "-64", I64(-64).String())
}

func TestF32_Kind(t *testing.T) {
	require.Equal(t, KindF32, F32(0).Kind())
}

func TestF32_Type(t *testing.T) {
	require.Equal(t, TypeF32, F32(0).Type())
}

func TestF32_String(t *testing.T) {
	require.Equal(t, "3.5", F32(3.5).String())
}

func TestF64_Kind(t *testing.T) {
	require.Equal(t, KindF64, F64(0).Kind())
}

func TestF64_Type(t *testing.T) {
	require.Equal(t, TypeF64, F64(0).Type())
}

func TestF64_String(t *testing.T) {
	require.Equal(t, "6.25", F64(6.25).String())
}

func TestRef_Kind(t *testing.T) {
	require.Equal(t, KindRef, Ref(0).Kind())
}

func TestRef_Type(t *testing.T) {
	require.Equal(t, TypeRef, Ref(0).Type())
}

func TestRef_String(t *testing.T) {
	require.Equal(t, "7", Ref(7).String())
}

func TestPrimitiveType_Kind(t *testing.T) {
	tests := []struct {
		typ  Type
		kind Kind
	}{
		{typ: TypeI1, kind: KindI1},
		{typ: TypeI8, kind: KindI8},
		{typ: TypeI32, kind: KindI32},
		{typ: TypeI64, kind: KindI64},
		{typ: TypeF32, kind: KindF32},
		{typ: TypeF64, kind: KindF64},
		{typ: TypeRef, kind: KindRef},
	}
	for _, tt := range tests {
		t.Run(tt.typ.String(), func(t *testing.T) {
			require.Equal(t, tt.kind, tt.typ.Kind())
		})
	}
}

func TestPrimitiveType_String(t *testing.T) {
	tests := []struct {
		typ Type
		str string
	}{
		{typ: TypeI1, str: "i1"},
		{typ: TypeI8, str: "i8"},
		{typ: TypeI32, str: "i32"},
		{typ: TypeI64, str: "i64"},
		{typ: TypeF32, str: "f32"},
		{typ: TypeF64, str: "f64"},
		{typ: TypeRef, str: "ref"},
	}
	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			require.Equal(t, tt.str, tt.typ.String())
		})
	}
}

func TestPrimitiveType_Cast(t *testing.T) {
	tests := []struct {
		typ    Type
		other  Type
		result bool
	}{
		{TypeI1, TypeI1, true},
		{TypeI1, TypeI32, false},
		{TypeI8, TypeI8, true},
		{TypeI8, TypeI32, false},
		{TypeI8, TypeI1, false},
		{TypeI32, TypeI32, true},
		{TypeI32, TypeI64, false},
		{TypeI32, TypeI8, false},
		{TypeI32, TypeI1, false},
		{TypeI64, TypeI64, true},
		{TypeI64, TypeI32, false},
		{TypeF32, TypeF32, true},
		{TypeF32, TypeF64, false},
		{TypeF64, TypeF64, true},
		{TypeF64, TypeF32, false},
		{TypeRef, TypeI32, true},
		{TypeRef, TypeRef, true},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%s", tt.typ, tt.other), func(t *testing.T) {
			require.Equal(t, tt.result, tt.typ.Cast(tt.other))
		})
	}
}

func TestPrimitiveType_Equals(t *testing.T) {
	tests := []struct {
		typ    Type
		other  Type
		result bool
	}{
		{TypeI1, TypeI1, true},
		{TypeI1, TypeI32, false},
		{TypeI8, TypeI8, true},
		{TypeI8, TypeI32, false},
		{TypeI8, TypeI1, false},
		{TypeI32, TypeI32, true},
		{TypeI32, TypeI64, false},
		{TypeI32, TypeI8, false},
		{TypeI64, TypeI64, true},
		{TypeI64, TypeI32, false},
		{TypeF32, TypeF32, true},
		{TypeF32, TypeF64, false},
		{TypeF64, TypeF64, true},
		{TypeF64, TypeF32, false},
		{TypeRef, TypeRef, true},
		{TypeRef, TypeI32, false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/%s", tt.typ, tt.other), func(t *testing.T) {
			require.Equal(t, tt.result, tt.typ.Equals(tt.other))
		})
	}
}
