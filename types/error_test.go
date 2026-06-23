package types

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestError_Kind(t *testing.T) {
	require.Equal(t, KindRef, NewError("", BoxedNull).Kind())
}

func TestError_Type(t *testing.T) {
	require.Equal(t, TypeError, NewError("", BoxedNull).Type())
}

func TestError_String(t *testing.T) {
	require.Equal(t, `error("boom")`, NewError("boom", BoxedNull).String())
}

func TestError_Error(t *testing.T) {
	require.Equal(t, "boom", NewError("boom", BoxedNull).Error())
}

func TestError_Unwrap(t *testing.T) {
	sentinel := errors.New("cause")
	e := WrapError(sentinel)
	require.Equal(t, "cause", e.Error())
	require.ErrorIs(t, e, sentinel)
	require.Nil(t, WrapError(nil))
}

func TestError_Value(t *testing.T) {
	require.Equal(t, BoxI32(7), NewError("", BoxI32(7)).Value())
}

func TestError_Refs(t *testing.T) {
	require.Nil(t, NewError("", BoxI32(7)).Refs())
	require.Equal(t, []Ref{Ref(3)}, NewError("", BoxRef(3)).Refs())
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
