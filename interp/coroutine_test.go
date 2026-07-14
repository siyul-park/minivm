package interp

import (
	"testing"

	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestCoroutine_Kind(t *testing.T) {
	require.Equal(t, types.KindRef, (&Coroutine{}).Kind())
}

func TestCoroutine_Type(t *testing.T) {
	typ := &types.FunctionType{Returns: []types.Type{types.TypeI32}}
	require.Equal(t, typ, (&Coroutine{typ: typ}).Type())
	require.Equal(t, &types.FunctionType{}, (&Coroutine{}).Type())
}

func TestCoroutine_String(t *testing.T) {
	require.Equal(t, "coroutine", (&Coroutine{}).String())
}

func TestCoroutine_Refs(t *testing.T) {
	co := &Coroutine{
		ref:    3,
		image:  []types.Boxed{types.BoxI32(1), types.BoxRef(5)},
		upvals: []types.Boxed{types.BoxRef(7), types.BoxF64(2)},
		value:  types.BoxRef(9),
	}
	require.Equal(t, []types.Ref{1, 3, 5, 7, 9}, co.Refs([]types.Ref{1}))

	require.Equal(t, []types.Ref{1}, (&Coroutine{value: types.BoxI32(1)}).Refs([]types.Ref{1}))
}
