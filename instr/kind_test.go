package instr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKind_String(t *testing.T) {
	tests := []struct {
		kind Kind
		want string
	}{
		{kind: KindI1, want: "i1"},
		{kind: KindI8, want: "i8"},
		{kind: KindI32, want: "i32"},
		{kind: KindI64, want: "i64"},
		{kind: KindF32, want: "f32"},
		{kind: KindF64, want: "f64"},
		{kind: KindRef, want: "ref"},
		{kind: KindAny, want: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			require.Equal(t, tt.want, tt.kind.String())
		})
	}
}

func TestKind_IsNumeric(t *testing.T) {
	for _, kind := range []Kind{KindI1, KindI8, KindI32, KindI64, KindF32, KindF64} {
		require.True(t, kind.IsNumeric(), kind.String())
	}
	for _, kind := range []Kind{KindRef, KindAny} {
		require.False(t, kind.IsNumeric(), kind.String())
	}
}

func TestKind_Repr(t *testing.T) {
	tests := []struct {
		kind Kind
		want Kind
	}{
		{kind: KindI1, want: KindI32},
		{kind: KindI8, want: KindI32},
		{kind: KindI32, want: KindI32},
		{kind: KindI64, want: KindI64},
		{kind: KindF32, want: KindF32},
		{kind: KindF64, want: KindF64},
		{kind: KindRef, want: KindRef},
		{kind: KindAny, want: KindAny},
	}
	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			require.Equal(t, tt.want, tt.kind.Repr())
		})
	}
}

func TestKind_Size(t *testing.T) {
	tests := []struct {
		kind Kind
		want int
	}{
		{kind: KindI1, want: 1},
		{kind: KindI8, want: 1},
		{kind: KindI32, want: 4},
		{kind: KindI64, want: 8},
		{kind: KindF32, want: 4},
		{kind: KindF64, want: 8},
		{kind: KindRef, want: 4},
		{kind: KindAny, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			require.Equal(t, tt.want, tt.kind.Size())
		})
	}
}
