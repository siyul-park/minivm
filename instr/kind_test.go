package instr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestKind_String(t *testing.T) {
	tests := []struct {
		name string
		kind Kind
		want string
	}{
		{name: "i1", kind: KindI1, want: "i1"},
		{name: "i8", kind: KindI8, want: "i8"},
		{name: "i32", kind: KindI32, want: "i32"},
		{name: "i64", kind: KindI64, want: "i64"},
		{name: "f32", kind: KindF32, want: "f32"},
		{name: "f64", kind: KindF64, want: "f64"},
		{name: "ref", kind: KindRef, want: "ref"},
		{name: "any", kind: KindAny, want: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
		name string
		kind Kind
		want Kind
	}{
		{name: "i1", kind: KindI1, want: KindI32},
		{name: "i8", kind: KindI8, want: KindI32},
		{name: "i32", kind: KindI32, want: KindI32},
		{name: "i64", kind: KindI64, want: KindI64},
		{name: "f32", kind: KindF32, want: KindF32},
		{name: "f64", kind: KindF64, want: KindF64},
		{name: "ref", kind: KindRef, want: KindRef},
		{name: "any", kind: KindAny, want: KindAny},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.kind.Repr())
		})
	}
}

func TestKind_Size(t *testing.T) {
	tests := []struct {
		name string
		kind Kind
		want int
	}{
		{name: "i1", kind: KindI1, want: 1},
		{name: "i8", kind: KindI8, want: 1},
		{name: "i32", kind: KindI32, want: 4},
		{name: "i64", kind: KindI64, want: 8},
		{name: "f32", kind: KindF32, want: 4},
		{name: "f64", kind: KindF64, want: 8},
		{name: "ref", kind: KindRef, want: 4},
		{name: "any", kind: KindAny, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.kind.Size())
		})
	}
}
