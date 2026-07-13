package instr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
