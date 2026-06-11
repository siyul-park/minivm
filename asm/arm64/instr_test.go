package arm64

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/asm"
)

func TestLDI(t *testing.T) {
	tests := []struct {
		name string
		val  uint64
		want []asm.Instruction
	}{
		{
			name: "zero",
			val:  0,
			want: []asm.Instruction{MOVZ(X0, 0, 0)},
		},
		{
			name: "low halfword",
			val:  0x1234,
			want: []asm.Instruction{MOVZ(X0, 0x1234, 0)},
		},
		{
			name: "single high halfword",
			val:  0x7FF6000000000000,
			want: []asm.Instruction{MOVZ(X0, 0x7FF6, 48)},
		},
		{
			name: "skips middle zero halfwords",
			val:  0x1234000056780000,
			want: []asm.Instruction{
				MOVZ(X0, 0x5678, 16),
				MOVK(X0, 0x1234, 48),
			},
		},
		{
			name: "keeps nonzero halfwords",
			val:  0x12345678,
			want: []asm.Instruction{
				MOVZ(X0, 0x5678, 0),
				MOVK(X0, 0x1234, 16),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, LDI(X0, tt.val))
		})
	}
}
