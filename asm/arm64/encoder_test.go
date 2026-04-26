//go:build arm64

package arm64

import (
	"testing"

	"github.com/siyul-park/minivm/asm"
	"github.com/stretchr/testify/require"
)

func TestNewEncoder(t *testing.T) {
	encoder := NewEncoder()
	require.NotNil(t, encoder)
}

func TestEncoder_Encode(t *testing.T) {
	tests := []struct {
		instr    asm.Instruction
		expected []byte
	}{
		{
			instr:    ADD(X0, X1, X2),
			expected: []byte{0x20, 0x00, 0x02, 0x8B},
		},
		{
			instr:    ADDI(X0, X1, 5),
			expected: []byte{0x20, 0x14, 0x00, 0x91},
		},
		{
			instr:    MOV(X0, X1),
			expected: []byte{0xE0, 0x03, 0x01, 0xAA},
		},
		{
			instr:    MOVZ(X0, 0x1234, 16),
			expected: []byte{0x80, 0x46, 0xA2, 0xD2},
		},
		{
			instr:    B(8),
			expected: []byte{0x02, 0x00, 0x00, 0x14},
		},
		{
			instr:    CBZ(X0, 8),
			expected: []byte{0x40, 0x00, 0x00, 0xB4},
		},
	}

	encoder := NewEncoder()
	for _, tt := range tests {
		t.Run(tt.instr.String(), func(t *testing.T) {
			encoded, err := encoder.Encode(tt.instr)
			require.NoError(t, err)
			require.Equal(t, tt.expected, encoded)
		})
	}
}

func TestEncode_HelpsInterface(t *testing.T) {
	encoder := NewEncoder()
	insts := []asm.Instruction{
		ADD(X0, X1, X2),
		ADDI(X0, X1, 5),
	}

	bytes, err := asm.Encode(encoder, insts)
	require.NoError(t, err)
	require.Equal(t, []byte{0x20, 0x00, 0x02, 0x8B, 0x20, 0x14, 0x00, 0x91}, bytes)
}
