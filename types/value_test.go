package types

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestZero(t *testing.T) {
	tests := []struct {
		kind Kind
		want Boxed
	}{
		{kind: KindI32, want: BoxI32(0)},
		{kind: KindI64, want: BoxI64(0)},
		{kind: KindF32, want: BoxF32(0)},
		{kind: KindF64, want: BoxF64(0)},
		{kind: KindRef, want: BoxedNull},
		{kind: Kind(255), want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			require.Equal(t, tt.want, Zero(tt.kind))
		})
	}
}

func TestIsNull(t *testing.T) {
	tests := []struct {
		val  Value
		want bool
	}{
		{Null, true},
		{BoxedNull, true},
		{Ref(1), false},
		{BoxRef(1), false},
		{I32(0), false},
		{TypedArray[int32]{0}, false},
		{nil, false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.want, IsNull(tt.val))
		})
	}
}

func TestKinds(t *testing.T) {
	require.Nil(t, Kinds(nil))
	require.Equal(t, []Kind{KindI32, KindRef, KindF64}, Kinds([]Type{TypeI32, TypeRef, TypeF64}))
}
