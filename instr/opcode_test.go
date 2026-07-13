package instr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpcode_IsBranch(t *testing.T) {
	tests := []struct {
		name string
		op   Opcode
		want bool
	}{
		{name: "unconditional branch", op: BR, want: true},
		{name: "conditional branch", op: BR_IF, want: true},
		{name: "branch table", op: BR_TABLE, want: true},
		{name: "return", op: RETURN, want: false},
		{name: "unreachable", op: UNREACHABLE, want: false},
		{name: "numeric operation", op: I32_ADD, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.op.IsBranch())
		})
	}
}
