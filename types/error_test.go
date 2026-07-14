package types

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewError(t *testing.T) {
	e := NewError(42, "boom", BoxRef(3))
	require.Equal(t, ErrorCode(42), e.Code())
	require.Equal(t, "boom", e.Error())
	require.Equal(t, BoxRef(3), e.Value())
}

func TestWrapError(t *testing.T) {
	sentinel := errors.New("cause")
	e := WrapError(ErrorCodeUserBase, sentinel)
	require.Equal(t, ErrorCodeUserBase, e.Code())
	require.ErrorIs(t, e, sentinel)
	require.Nil(t, WrapError(ErrorCodeNone, nil))
}

func TestError_Error(t *testing.T) {
	require.Equal(t, "boom", NewError(ErrorCodeNone, "boom", BoxedNull).Error())
}

func TestError_Unwrap(t *testing.T) {
	sentinel := errors.New("cause")
	e := WrapError(ErrorCodeNone, sentinel)
	require.ErrorIs(t, e.Unwrap(), sentinel)
}

func TestError_Value(t *testing.T) {
	require.Equal(t, BoxI32(7), NewError(ErrorCodeNone, "", BoxI32(7)).Value())
}

func TestError_Code(t *testing.T) {
	require.Equal(t, ErrorCodeNone, NewError(ErrorCodeNone, "", BoxedNull).Code())
	require.Equal(t, ErrorCode(42), NewError(42, "", BoxedNull).Code())

	sentinel := errors.New("cause")
	e := WrapError(ErrorCodeUserBase, sentinel)
	require.Equal(t, ErrorCodeUserBase, e.Code())
	require.ErrorIs(t, e, sentinel)
}

func TestError_Kind(t *testing.T) {
	require.Equal(t, KindRef, NewError(ErrorCodeNone, "", BoxedNull).Kind())
}

func TestError_Type(t *testing.T) {
	require.Equal(t, TypeError, NewError(ErrorCodeNone, "", BoxedNull).Type())
}

func TestError_String(t *testing.T) {
	require.Equal(t, `error("boom")`, NewError(ErrorCodeNone, "boom", BoxedNull).String())
}

func TestError_Refs(t *testing.T) {
	require.Equal(t, []Ref{9}, NewError(ErrorCodeNone, "", BoxI32(7)).Refs([]Ref{9}))
	require.Equal(t, []Ref{Ref(9), Ref(3)}, NewError(ErrorCodeNone, "", BoxRef(3)).Refs([]Ref{9}))
}

func TestErrorType_Kind(t *testing.T) {
	require.Equal(t, KindRef, TypeError.Kind())
}

func TestErrorType_String(t *testing.T) {
	require.Equal(t, "error", TypeError.String())
}

func TestErrorType_Cast(t *testing.T) {
	require.True(t, TypeError.Cast(TypeError))
	require.False(t, TypeError.Cast(TypeI32))
}

func TestErrorType_Equals(t *testing.T) {
	require.True(t, TypeError.Equals(TypeError))
	require.False(t, TypeError.Equals(TypeString))
}
