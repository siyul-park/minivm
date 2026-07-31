package instr_test

import (
	"testing"

	instr "github.com/siyul-park/minivm/instr"
	"github.com/stretchr/testify/require"
)

func TestKind_String(t *testing.T) {
	tests := []struct {
		kind instr.Kind
		want string
	}{
		{kind: instr.KindI1, want: "i1"},
		{kind: instr.KindI8, want: "i8"},
		{kind: instr.KindI32, want: "i32"},
		{kind: instr.KindI64, want: "i64"},
		{kind: instr.KindF32, want: "f32"},
		{kind: instr.KindF64, want: "f64"},
		{kind: instr.KindRef, want: "ref"},
		{kind: instr.KindAny, want: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			require.Equal(t, tt.want, tt.kind.String())
		})
	}
}

func TestKind_IsNumeric(t *testing.T) {
	for _, kind := range []instr.Kind{instr.KindI1, instr.KindI8, instr.KindI32, instr.KindI64, instr.KindF32, instr.KindF64} {
		require.True(t, kind.IsNumeric(), kind.String())
	}
	for _, kind := range []instr.Kind{instr.KindRef, instr.KindAny} {
		require.False(t, kind.IsNumeric(), kind.String())
	}
}

func TestKind_Repr(t *testing.T) {
	tests := []struct {
		kind instr.Kind
		want instr.Kind
	}{
		{kind: instr.KindI1, want: instr.KindI32},
		{kind: instr.KindI8, want: instr.KindI32},
		{kind: instr.KindI32, want: instr.KindI32},
		{kind: instr.KindI64, want: instr.KindI64},
		{kind: instr.KindF32, want: instr.KindF32},
		{kind: instr.KindF64, want: instr.KindF64},
		{kind: instr.KindRef, want: instr.KindRef},
		{kind: instr.KindAny, want: instr.KindAny},
	}
	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			require.Equal(t, tt.want, tt.kind.Repr())
		})
	}
}

func TestKind_Size(t *testing.T) {
	tests := []struct {
		kind instr.Kind
		want int
	}{
		{kind: instr.KindI1, want: 1},
		{kind: instr.KindI8, want: 1},
		{kind: instr.KindI32, want: 4},
		{kind: instr.KindI64, want: 8},
		{kind: instr.KindF32, want: 4},
		{kind: instr.KindF64, want: 8},
		{kind: instr.KindRef, want: 4},
		{kind: instr.KindAny, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.kind.String(), func(t *testing.T) {
			require.Equal(t, tt.want, tt.kind.Size())
		})
	}
}
