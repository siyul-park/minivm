package instr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpcode_IsBranch(t *testing.T) {
	tests := []struct {
		op   Opcode
		want bool
	}{
		{op: BR, want: true},
		{op: BR_IF, want: true},
		{op: BR_TABLE, want: true},
		{op: RETURN, want: false},
		{op: UNREACHABLE, want: false},
		{op: I32_ADD, want: false},
	}
	for _, tt := range tests {
		t.Run(TypeOf(tt.op).Mnemonic, func(t *testing.T) {
			require.Equal(t, tt.want, tt.op.IsBranch())
		})
	}
}
