package types_test

import (
	"errors"
	"testing"

	types "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestNewError(t *testing.T) {
	e := types.NewError(42, "boom", types.BoxRef(3))
	require.Equal(t, types.ErrorCode(42), e.Code())
	require.Equal(t, "boom", e.Error())
	require.Equal(t, types.BoxRef(3), e.Value())
}

func TestWrapError(t *testing.T) {
	sentinel := errors.New("cause")
	e := types.WrapError(types.ErrorCodeUserBase, sentinel)
	require.Equal(t, types.ErrorCodeUserBase, e.Code())
	require.ErrorIs(t, e, sentinel)
	require.Nil(t, types.WrapError(types.ErrorCodeNone, nil))
}

func TestError_Error(t *testing.T) {
	require.Equal(t, "boom", types.NewError(types.ErrorCodeNone, "boom", types.BoxedNull).Error())
}

func TestError_Unwrap(t *testing.T) {
	sentinel := errors.New("cause")
	e := types.WrapError(types.ErrorCodeNone, sentinel)
	require.ErrorIs(t, e.Unwrap(), sentinel)
}

func TestError_Value(t *testing.T) {
	require.Equal(t, types.BoxI32(7), types.NewError(types.ErrorCodeNone, "", types.BoxI32(7)).Value())
}

func TestError_Code(t *testing.T) {
	require.Equal(t, types.ErrorCodeNone, types.NewError(types.ErrorCodeNone, "", types.BoxedNull).Code())
	require.Equal(t, types.ErrorCode(42), types.NewError(42, "", types.BoxedNull).Code())

	sentinel := errors.New("cause")
	e := types.WrapError(types.ErrorCodeUserBase, sentinel)
	require.Equal(t, types.ErrorCodeUserBase, e.Code())
	require.ErrorIs(t, e, sentinel)
}

func TestError_Kind(t *testing.T) {
	require.Equal(t, types.KindRef, types.NewError(types.ErrorCodeNone, "", types.BoxedNull).Kind())
}

func TestError_Type(t *testing.T) {
	typ := types.NewError(types.ErrorCodeNone, "", types.BoxedNull).Type()
	require.Equal(t, types.TypeError, typ)
	require.Equal(t, types.KindRef, typ.Kind())
	require.Equal(t, "error", typ.String())
	require.True(t, typ.Cast(types.TypeError))
	require.False(t, typ.Cast(types.TypeI32))
	require.True(t, typ.Equals(types.TypeError))
	require.False(t, typ.Equals(types.TypeString))
}

func TestError_String(t *testing.T) {
	require.Equal(t, `error("boom")`, types.NewError(types.ErrorCodeNone, "boom", types.BoxedNull).String())
}

func TestError_Refs(t *testing.T) {
	require.Equal(t, []types.Ref{9}, types.NewError(types.ErrorCodeNone, "", types.BoxI32(7)).Refs([]types.Ref{9}))
	require.Equal(t, []types.Ref{types.Ref(9), types.Ref(3)}, types.NewError(types.ErrorCodeNone, "", types.BoxRef(3)).Refs([]types.Ref{9}))
}
