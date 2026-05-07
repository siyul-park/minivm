package types

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrimitive_Kind(t *testing.T) {
	tests := []struct {
		val  Value
		kind Kind
	}{
		{
			val:  I32(0),
			kind: KindI32,
		},
		{
			val:  I64(0),
			kind: KindI64,
		},
		{
			val:  F32(0),
			kind: KindF32,
		},
		{
			val:  F64(0),
			kind: KindF64,
		},
		{
			val:  Ref(0),
			kind: KindRef,
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.kind, tt.val.Kind())
		})
	}
}

func TestPrimitive_Type(t *testing.T) {
	tests := []struct {
		val Value
		typ Type
	}{
		{
			val: I32(0),
			typ: TypeI32,
		},
		{
			val: I64(0),
			typ: TypeI64,
		},
		{
			val: F32(0),
			typ: TypeF32,
		},
		{
			val: F64(0),
			typ: TypeF64,
		},
		{
			val: Ref(0),
			typ: TypeRef,
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.typ, tt.val.Type())
		})
	}
}

func TestPrimitive_String(t *testing.T) {
	tests := []struct {
		val Value
		str string
	}{
		{
			val: I32(0),
			str: "0",
		},
		{
			val: I64(0),
			str: "0",
		},
		{
			val: F32(0),
			str: "0",
		},
		{
			val: F64(0),
			str: "0",
		},
		{
			val: Ref(0),
			str: "0",
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.str, tt.val.String())
		})
	}
}

func TestBool(t *testing.T) {
	require.Equal(t, True, Bool(true))
	require.Equal(t, False, Bool(false))
}

func TestPrimitiveType_Kind(t *testing.T) {
	tests := []struct {
		typ  Type
		kind Kind
	}{
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
		{TypeI32, TypeI32, true},
		{TypeI32, TypeI64, false},
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
		{TypeI32, TypeI32, true},
		{TypeI32, TypeI64, false},
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
