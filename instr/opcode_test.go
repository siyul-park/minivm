package instr_test

import (
	"testing"

	instr "github.com/siyul-park/minivm/instr"
	"github.com/stretchr/testify/require"
)

func TestOpcode_IsBranch(t *testing.T) {
	tests := []struct {
		op   instr.Opcode
		want bool
	}{
		{op: instr.BR, want: true},
		{op: instr.BR_IF, want: true},
		{op: instr.BR_TABLE, want: true},
		{op: instr.RETURN, want: false},
		{op: instr.UNREACHABLE, want: false},
		{op: instr.I32_ADD, want: false},
	}
	for _, tt := range tests {
		t.Run(instr.TypeOf(tt.op).Mnemonic, func(t *testing.T) {
			require.Equal(t, tt.want, tt.op.IsBranch())
		})
	}
}
