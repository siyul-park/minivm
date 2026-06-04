package types

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestZero(t *testing.T) {
	tests := []struct {
		name string
		kind Kind
		want Boxed
	}{
		{name: "i32", kind: KindI32, want: BoxI32(0)},
		{name: "i64", kind: KindI64, want: BoxI64(0)},
		{name: "f32", kind: KindF32, want: BoxF32(0)},
		{name: "f64", kind: KindF64, want: BoxF64(0)},
		{name: "ref", kind: KindRef, want: BoxedNull},
		{name: "unknown", kind: Kind(255), want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

func TestKind_String(t *testing.T) {
	tests := []struct {
		kind Kind
		str  string
	}{
		{KindI32, "i32"},
		{KindI64, "i64"},
		{KindF32, "f32"},
		{KindF64, "f64"},
		{KindRef, "ref"},
		{Kind(255), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.str, func(t *testing.T) {
			require.Equal(t, tt.str, tt.kind.String())
		})
	}
}

func TestKind_IsNumeric(t *testing.T) {
	tests := []struct {
		kind Kind
		want bool
	}{
		{KindI32, true},
		{KindI64, true},
		{KindF32, true},
		{KindF64, true},
		{KindRef, false},
		{Kind(255), false},
	}
	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			require.Equal(t, tt.want, tt.kind.IsNumeric())
		})
	}
}

func TestKind_Size(t *testing.T) {
	tests := []struct {
		kind Kind
		size int
	}{
		{KindI32, 4},
		{KindI64, 8},
		{KindF32, 4},
		{KindF64, 8},
		{KindRef, 4},
		{Kind(255), 0},
	}
	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			require.Equal(t, tt.size, tt.kind.Size())
		})
	}
}
