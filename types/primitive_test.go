package types

import (
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
	typ := I1(false).Type()
	require.Equal(t, TypeI1, typ)
	require.Equal(t, KindI1, typ.Kind())
	require.Equal(t, "i1", typ.String())
	require.True(t, typ.Cast(TypeI1))
	require.False(t, typ.Cast(TypeI32))
	require.True(t, typ.Equals(TypeI1))
	require.False(t, typ.Equals(TypeI32))
}

func TestI1_String(t *testing.T) {
	require.Equal(t, "false", I1(false).String())
	require.Equal(t, "true", I1(true).String())
}

func TestI8_Kind(t *testing.T) {
	require.Equal(t, KindI8, I8(0).Kind())
}

func TestI8_Type(t *testing.T) {
	typ := I8(0).Type()
	require.Equal(t, TypeI8, typ)
	require.Equal(t, KindI8, typ.Kind())
	require.Equal(t, "i8", typ.String())
	require.True(t, typ.Cast(TypeI8))
	require.False(t, typ.Cast(TypeI32))
	require.False(t, typ.Cast(TypeI1))
	require.True(t, typ.Equals(TypeI8))
	require.False(t, typ.Equals(TypeI32))
	require.False(t, typ.Equals(TypeI1))
}

func TestI8_String(t *testing.T) {
	require.Equal(t, "-8", I8(-8).String())
}

func TestI32_Kind(t *testing.T) {
	require.Equal(t, KindI32, I32(0).Kind())
}

func TestI32_Type(t *testing.T) {
	typ := I32(0).Type()
	require.Equal(t, TypeI32, typ)
	require.Equal(t, KindI32, typ.Kind())
	require.Equal(t, "i32", typ.String())
	require.True(t, typ.Cast(TypeI32))
	require.False(t, typ.Cast(TypeI64))
	require.False(t, typ.Cast(TypeI8))
	require.False(t, typ.Cast(TypeI1))
	require.True(t, typ.Equals(TypeI32))
	require.False(t, typ.Equals(TypeI64))
	require.False(t, typ.Equals(TypeI8))
}

func TestI32_String(t *testing.T) {
	require.Equal(t, "-32", I32(-32).String())
}

func TestI64_Kind(t *testing.T) {
	require.Equal(t, KindI64, I64(0).Kind())
}

func TestI64_Type(t *testing.T) {
	typ := I64(0).Type()
	require.Equal(t, TypeI64, typ)
	require.Equal(t, KindI64, typ.Kind())
	require.Equal(t, "i64", typ.String())
	require.True(t, typ.Cast(TypeI64))
	require.False(t, typ.Cast(TypeI32))
	require.True(t, typ.Equals(TypeI64))
	require.False(t, typ.Equals(TypeI32))
}

func TestI64_String(t *testing.T) {
	require.Equal(t, "-64", I64(-64).String())
}

func TestF32_Kind(t *testing.T) {
	require.Equal(t, KindF32, F32(0).Kind())
}

func TestF32_Type(t *testing.T) {
	typ := F32(0).Type()
	require.Equal(t, TypeF32, typ)
	require.Equal(t, KindF32, typ.Kind())
	require.Equal(t, "f32", typ.String())
	require.True(t, typ.Cast(TypeF32))
	require.False(t, typ.Cast(TypeF64))
	require.True(t, typ.Equals(TypeF32))
	require.False(t, typ.Equals(TypeF64))
}

func TestF32_String(t *testing.T) {
	require.Equal(t, "3.5", F32(3.5).String())
}

func TestF64_Kind(t *testing.T) {
	require.Equal(t, KindF64, F64(0).Kind())
}

func TestF64_Type(t *testing.T) {
	typ := F64(0).Type()
	require.Equal(t, TypeF64, typ)
	require.Equal(t, KindF64, typ.Kind())
	require.Equal(t, "f64", typ.String())
	require.True(t, typ.Cast(TypeF64))
	require.False(t, typ.Cast(TypeF32))
	require.True(t, typ.Equals(TypeF64))
	require.False(t, typ.Equals(TypeF32))
}

func TestF64_String(t *testing.T) {
	require.Equal(t, "6.25", F64(6.25).String())
}

func TestRef_Kind(t *testing.T) {
	require.Equal(t, KindRef, Ref(0).Kind())
}

func TestRef_Type(t *testing.T) {
	typ := Ref(0).Type()
	require.Equal(t, TypeRef, typ)
	require.Equal(t, KindRef, typ.Kind())
	require.Equal(t, "ref", typ.String())
	require.True(t, typ.Cast(TypeI32))
	require.True(t, typ.Cast(TypeRef))
	require.True(t, typ.Equals(TypeRef))
	require.False(t, typ.Equals(TypeI32))
}

func TestRef_String(t *testing.T) {
	require.Equal(t, "7", Ref(7).String())
}
