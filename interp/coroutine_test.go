package interp

import (
	"testing"

	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestCoroutine_Refs(t *testing.T) {
	co := &coroutine{
		ref:    3,
		image:  []types.Boxed{types.BoxI32(1), types.BoxRef(5)},
		upvals: []types.Boxed{types.BoxRef(7), types.BoxF64(2)},
		value:  types.BoxRef(9),
	}
	require.Equal(t, []types.Ref{1, 3, 5, 7, 9}, co.Refs([]types.Ref{1}))

	require.Equal(t, []types.Ref{1}, (&coroutine{value: types.BoxI32(1)}).Refs([]types.Ref{1}))
}
