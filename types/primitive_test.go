package types_test

import (
	"testing"

	types "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestBool(t *testing.T) {
	require.Equal(t, types.True, types.Bool(true))
	require.Equal(t, types.False, types.Bool(false))
}

func TestI1_Kind(t *testing.T) {
	require.Equal(t, types.KindI1, types.I1(false).Kind())
}

func TestI1_Type(t *testing.T) {
	typ := types.I1(false).Type()
	require.Equal(t, types.TypeI1, typ)
	require.Equal(t, types.KindI1, typ.Kind())
	require.Equal(t, "i1", typ.String())
	require.True(t, typ.Cast(types.TypeI1))
	require.False(t, typ.Cast(types.TypeI32))
	require.True(t, typ.Equals(types.TypeI1))
	require.False(t, typ.Equals(types.TypeI32))
}

func TestI1_String(t *testing.T) {
	require.Equal(t, "false", types.I1(false).String())
	require.Equal(t, "true", types.I1(true).String())
}

func TestI8_Kind(t *testing.T) {
	require.Equal(t, types.KindI8, types.I8(0).Kind())
}

func TestI8_Type(t *testing.T) {
	typ := types.I8(0).Type()
	require.Equal(t, types.TypeI8, typ)
	require.Equal(t, types.KindI8, typ.Kind())
	require.Equal(t, "i8", typ.String())
	require.True(t, typ.Cast(types.TypeI8))
	require.False(t, typ.Cast(types.TypeI32))
	require.False(t, typ.Cast(types.TypeI1))
	require.True(t, typ.Equals(types.TypeI8))
	require.False(t, typ.Equals(types.TypeI32))
	require.False(t, typ.Equals(types.TypeI1))
}

func TestI8_String(t *testing.T) {
	require.Equal(t, "-8", types.I8(-8).String())
}

func TestI32_Kind(t *testing.T) {
	require.Equal(t, types.KindI32, types.I32(0).Kind())
}

func TestI32_Type(t *testing.T) {
	typ := types.I32(0).Type()
	require.Equal(t, types.TypeI32, typ)
	require.Equal(t, types.KindI32, typ.Kind())
	require.Equal(t, "i32", typ.String())
	require.True(t, typ.Cast(types.TypeI32))
	require.False(t, typ.Cast(types.TypeI64))
	require.False(t, typ.Cast(types.TypeI8))
	require.False(t, typ.Cast(types.TypeI1))
	require.True(t, typ.Equals(types.TypeI32))
	require.False(t, typ.Equals(types.TypeI64))
	require.False(t, typ.Equals(types.TypeI8))
}

func TestI32_String(t *testing.T) {
	require.Equal(t, "-32", types.I32(-32).String())
}

func TestI64_Kind(t *testing.T) {
	require.Equal(t, types.KindI64, types.I64(0).Kind())
}

func TestI64_Type(t *testing.T) {
	typ := types.I64(0).Type()
	require.Equal(t, types.TypeI64, typ)
	require.Equal(t, types.KindI64, typ.Kind())
	require.Equal(t, "i64", typ.String())
	require.True(t, typ.Cast(types.TypeI64))
	require.False(t, typ.Cast(types.TypeI32))
	require.True(t, typ.Equals(types.TypeI64))
	require.False(t, typ.Equals(types.TypeI32))
}

func TestI64_String(t *testing.T) {
	require.Equal(t, "-64", types.I64(-64).String())
}

func TestF32_Kind(t *testing.T) {
	require.Equal(t, types.KindF32, types.F32(0).Kind())
}

func TestF32_Type(t *testing.T) {
	typ := types.F32(0).Type()
	require.Equal(t, types.TypeF32, typ)
	require.Equal(t, types.KindF32, typ.Kind())
	require.Equal(t, "f32", typ.String())
	require.True(t, typ.Cast(types.TypeF32))
	require.False(t, typ.Cast(types.TypeF64))
	require.True(t, typ.Equals(types.TypeF32))
	require.False(t, typ.Equals(types.TypeF64))
}

func TestF32_String(t *testing.T) {
	require.Equal(t, "3.5", types.F32(3.5).String())
}

func TestF64_Kind(t *testing.T) {
	require.Equal(t, types.KindF64, types.F64(0).Kind())
}

func TestF64_Type(t *testing.T) {
	typ := types.F64(0).Type()
	require.Equal(t, types.TypeF64, typ)
	require.Equal(t, types.KindF64, typ.Kind())
	require.Equal(t, "f64", typ.String())
	require.True(t, typ.Cast(types.TypeF64))
	require.False(t, typ.Cast(types.TypeF32))
	require.True(t, typ.Equals(types.TypeF64))
	require.False(t, typ.Equals(types.TypeF32))
}

func TestF64_String(t *testing.T) {
	require.Equal(t, "6.25", types.F64(6.25).String())
}

func TestRef_Kind(t *testing.T) {
	require.Equal(t, types.KindRef, types.Ref(0).Kind())
}

func TestRef_Type(t *testing.T) {
	typ := types.Ref(0).Type()
	require.Equal(t, types.TypeAny, typ)
	require.Equal(t, types.KindRef, typ.Kind())
	require.Equal(t, "any", typ.String())
	require.True(t, typ.Cast(types.TypeI32))
	require.True(t, typ.Cast(types.TypeAny))
	require.True(t, typ.Equals(types.TypeAny))
	require.False(t, typ.Equals(types.TypeI32))
}

func TestRef_String(t *testing.T) {
	require.Equal(t, "7", types.Ref(7).String())
}
