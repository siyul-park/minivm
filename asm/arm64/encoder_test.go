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
			instr:    ADD(W0, W1, W2),
			expected: []byte{0x20, 0x00, 0x02, 0x0B},
		},
		{
			instr:    ADDI(X0, X1, 5),
			expected: []byte{0x20, 0x14, 0x00, 0x91},
		},
		{
			instr:    SDIV(W0, W1, W2),
			expected: []byte{0x20, 0x0C, 0xC2, 0x1A},
		},
		{
			instr:    ASR(W0, W1, W2),
			expected: []byte{0x20, 0x28, 0xC2, 0x1A},
		},
		{
			instr:    ANDI(W0, W1, 0xFF),
			expected: []byte{0x20, 0x1C, 0x00, 0x12},
		},
		{
			instr:    LSLI(W0, W1, 1),
			expected: []byte{0x20, 0x78, 0x1F, 0x53},
		},
		{
			instr:    RORI(W0, W1, 1),
			expected: []byte{0x20, 0x04, 0x81, 0x13},
		},
		{
			instr:    CLZ(W0, W1),
			expected: []byte{0x20, 0x10, 0xC0, 0x5A},
		},
		{
			instr:    REV(W0, W1),
			expected: []byte{0x20, 0x08, 0xC0, 0x5A},
		},
		{
			instr:    TST(W1, W2),
			expected: []byte{0x3F, 0x00, 0x02, 0x6A},
		},
		{
			instr:    CMP(W1, W2),
			expected: []byte{0x3F, 0x00, 0x02, 0x6B},
		},
		{
			instr:    CMN(W1, W2),
			expected: []byte{0x3F, 0x00, 0x02, 0x2B},
		},
		{
			instr:    CSET(W0, 0),
			expected: []byte{0xE0, 0x17, 0x9F, 0x1A},
		},
		{
			instr:    CSINC(W0, W1, W2, 0),
			expected: []byte{0x20, 0x04, 0x82, 0x1A},
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
			instr:    MOVI(W0, 1),
			expected: []byte{0x20, 0x00, 0x80, 0x52},
		},
		{
			instr:    B(8),
			expected: []byte{0x02, 0x00, 0x00, 0x14},
		},
		{
			instr:    CBZ(X0, 8),
			expected: []byte{0x40, 0x00, 0x00, 0xB4},
		},
		{
			instr:    CBZ(W0, 8),
			expected: []byte{0x40, 0x00, 0x00, 0x34},
		},
		{
			instr:    FADD(S0, S1, S2),
			expected: []byte{0x20, 0x28, 0x22, 0x1E},
		},
		{
			instr:    SCVTF(D0, W1),
			expected: []byte{0x20, 0x00, 0x62, 0x1E},
		},
		{
			instr:    SCVTF(S0, X1),
			expected: []byte{0x20, 0x00, 0x22, 0x9E},
		},
		{
			instr:    FCVTZS(W0, S1),
			expected: []byte{0x20, 0x00, 0x38, 0x1E},
		},
		{
			instr:    FCVT(D0, S1),
			expected: []byte{0x20, 0xC0, 0x22, 0x1E},
		},
		{
			instr:    FABS(S0, S1),
			expected: []byte{0x20, 0xC0, 0x20, 0x1E},
		},
		{
			instr:    FMOV(S0, W1),
			expected: []byte{0x20, 0x00, 0x27, 0x1E},
		},
		{
			instr:    CCMP(X1, X2, 5, 0),
			expected: []byte{0x25, 0x00, 0x42, 0xFA},
		},
		{
			instr:    CCMPI(X1, 3, 5, 0),
			expected: []byte{0x25, 0x08, 0x43, 0xFA},
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

	t.Run("invalid mixed widths", func(t *testing.T) {
		_, err := encoder.Encode(ADD(W0, W1, X2))
		require.ErrorIs(t, err, asm.ErrInvalidOperand)
	})

	t.Run("invalid mixed float widths", func(t *testing.T) {
		_, err := encoder.Encode(FADD(S0, S1, D2))
		require.ErrorIs(t, err, asm.ErrInvalidOperand)
	})

	t.Run("invalid conversion types", func(t *testing.T) {
		_, err := encoder.Encode(FCVTZS(S0, W1))
		require.ErrorIs(t, err, asm.ErrInvalidOperand)
	})

	t.Run("invalid conversion widths", func(t *testing.T) {
		dst := asm.NewPReg(0, asm.RegTypeInt, asm.WidthUndefined)
		_, err := encoder.Encode(FCVTZS(dst, S1))
		require.ErrorIs(t, err, asm.ErrInvalidOperand)

		_, err = encoder.Encode(FCVTZU(dst, S1))
		require.ErrorIs(t, err, asm.ErrInvalidOperand)
	})

	t.Run("invalid float move types", func(t *testing.T) {
		_, err := encoder.Encode(FMOV(W0, W1))
		require.ErrorIs(t, err, asm.ErrInvalidOperand)
	})

	t.Run("invalid float move widths", func(t *testing.T) {
		dst := asm.NewPReg(0, asm.RegTypeFloat, asm.WidthUndefined)
		src := asm.NewPReg(1, asm.RegTypeFloat, asm.WidthUndefined)
		_, err := encoder.Encode(FMOV(dst, src))
		require.ErrorIs(t, err, asm.ErrInvalidOperand)
	})

	t.Run("invalid branch register type", func(t *testing.T) {
		_, err := encoder.Encode(CBZ(S0, 8))
		require.ErrorIs(t, err, asm.ErrInvalidOperand)
	})

	t.Run("invalid move shift width", func(t *testing.T) {
		_, err := encoder.Encode(MOVK(W0, 1, 32))
		require.ErrorIs(t, err, asm.ErrInvalidOperand)
	})
}
