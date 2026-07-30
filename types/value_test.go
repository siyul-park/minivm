package types_test

import (
	"fmt"
	"testing"

	types "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestZero(t *testing.T) {
	tests := []struct {
		kind types.Kind
		want types.Boxed
	}{
		{kind: types.KindI32, want: types.BoxI32(0)},
		{kind: types.KindI64, want: types.BoxI64(0)},
		{kind: types.KindF32, want: types.BoxF32(0)},
		{kind: types.KindF64, want: types.BoxF64(0)},
		{kind: types.KindRef, want: types.BoxedNull},
		{kind: types.Kind(255), want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			require.Equal(t, tt.want, types.Zero(tt.kind))
		})
	}
}

func TestIsNull(t *testing.T) {
	tests := []struct {
		val  types.Value
		want bool
	}{
		{types.Null, true},
		{types.BoxedNull, true},
		{types.Ref(1), false},
		{types.BoxRef(1), false},
		{types.I32(0), false},
		{types.TypedArray[int32]{0}, false},
		{nil, false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.want, types.IsNull(tt.val))
		})
	}
}

func TestKinds(t *testing.T) {
	require.Nil(t, types.Kinds(nil))
	require.Equal(t, []types.Kind{types.KindI32, types.KindRef, types.KindF64}, types.Kinds([]types.Type{types.TypeI32, types.TypeRef, types.TypeF64}))
}
