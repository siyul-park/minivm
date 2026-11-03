package instr_test

import (
	"math"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/stretchr/testify/require"
)

func TestMustNew(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		require.Equal(t, instr.Instruction{byte(instr.LOCAL_GET), 3}, instr.MustNew(instr.LOCAL_GET, 3))
	})
	t.Run("invalid", func(t *testing.T) {
		require.Panics(t, func() { instr.MustNew(instr.LOCAL_GET) })
	})
}

func TestNew(t *testing.T) {
	negative := func(v int64) uint64 { return uint64(v) }
	tests := []struct {
		name     string
		op       instr.Opcode
		operands []uint64
		want     []byte
		wantErr  error
	}{
		{name: "static", op: instr.NOP, want: []byte{byte(instr.NOP)}},
		{name: "dynamic", op: instr.BR_TABLE, operands: []uint64{2, 0, 1, 0}, want: []byte{byte(instr.BR_TABLE), 2, 0, 0, 1, 0, 0, 0}},
		{name: "empty dynamic", op: instr.BR_TABLE, operands: []uint64{0, 3}, want: []byte{byte(instr.BR_TABLE), 0, 3, 0}},
		{name: "unknown opcode", op: instr.Opcode(0xFF), wantErr: instr.ErrUnknownOpcode},
		{name: "missing operand", op: instr.LOCAL_GET, wantErr: instr.ErrOperandCount},
		{name: "extra operand", op: instr.NOP, operands: []uint64{1}, wantErr: instr.ErrOperandCount},
		{name: "extra operand after dynamic", op: instr.BR_TABLE, operands: []uint64{0, 3, 0}, wantErr: instr.ErrOperandCount},
		{name: "dynamic count exceeds operands", op: instr.BR_TABLE, operands: []uint64{2, 0, 1}, wantErr: instr.ErrOperandCount},
		{name: "dynamic count below operands", op: instr.BR_TABLE, operands: []uint64{1, 0, 1, 0}, wantErr: instr.ErrOperandCount},
		{name: "dynamic count above 255", op: instr.BR_TABLE, operands: append([]uint64{256}, make([]uint64, 257)...), wantErr: instr.ErrOperandRange},
		{name: "dynamic count at 255", op: instr.BR_TABLE, operands: append([]uint64{255}, make([]uint64, 256)...), want: append([]byte{byte(instr.BR_TABLE), 255}, make([]byte, 512)...)},
		{name: "dynamic operand range", op: instr.BR_TABLE, operands: []uint64{1, 0x10000, 0}, wantErr: instr.ErrOperandRange},
		{name: "width 1 maximum", op: instr.LOCAL_GET, operands: []uint64{255}, want: []byte{byte(instr.LOCAL_GET), 255}},
		{name: "width 1 above maximum", op: instr.LOCAL_GET, operands: []uint64{256}, wantErr: instr.ErrOperandRange},
		{name: "width 1 negative fits", op: instr.LOCAL_GET, operands: []uint64{negative(-128)}, want: []byte{byte(instr.LOCAL_GET), 0x80}},
		{name: "width 1 negative below", op: instr.LOCAL_GET, operands: []uint64{negative(-129)}, wantErr: instr.ErrOperandRange},
		{name: "width 2 maximum", op: instr.BR, operands: []uint64{65535}, want: []byte{byte(instr.BR), 0xFF, 0xFF}},
		{name: "width 2 above maximum", op: instr.BR, operands: []uint64{65536}, wantErr: instr.ErrOperandRange},
		{name: "width 2 negative fits", op: instr.BR, operands: []uint64{negative(-5)}, want: []byte{byte(instr.BR), 0xFB, 0xFF}},
		{name: "width 2 negative below", op: instr.BR, operands: []uint64{negative(-32769)}, wantErr: instr.ErrOperandRange},
		{name: "width 4 maximum", op: instr.I32_CONST, operands: []uint64{math.MaxUint32}, want: []byte{byte(instr.I32_CONST), 0xFF, 0xFF, 0xFF, 0xFF}},
		{name: "width 4 above maximum", op: instr.I32_CONST, operands: []uint64{math.MaxUint32 + 1}, wantErr: instr.ErrOperandRange},
		{name: "width 4 negative fits", op: instr.I32_CONST, operands: []uint64{negative(math.MinInt32)}, want: []byte{byte(instr.I32_CONST), 0, 0, 0, 0x80}},
		{name: "width 4 negative below", op: instr.I32_CONST, operands: []uint64{negative(math.MinInt32 - 1)}, wantErr: instr.ErrOperandRange},
		{name: "width 8 any", op: instr.I64_CONST, operands: []uint64{math.MaxUint64}, want: []byte{byte(instr.I64_CONST), 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instruction, err := instr.New(tt.op, tt.operands...)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Nil(t, instruction)
				return
			}
			require.NoError(t, err)
			require.Equal(t, instr.Instruction(tt.want), instruction)
			require.Equal(t, tt.op, instruction.Opcode())
			require.Equal(t, len(tt.want), instruction.Width())
		})
	}
}

func TestInstruction_SetOperand(t *testing.T) {
	t.Run("static", func(t *testing.T) {
		instruction := instr.MustNew(instr.I32_CONST, 1)
		instruction.SetOperand(0, 42)
		require.Equal(t, []uint64{42}, instruction.Operands())
	})
	t.Run("narrow", func(t *testing.T) {
		instruction := instr.MustNew(instr.LOCAL_GET, 1)
		instruction.SetOperand(0, 7)
		require.Equal(t, []uint64{7}, instruction.Operands())
	})
	t.Run("wide", func(t *testing.T) {
		instruction := instr.MustNew(instr.I64_CONST, 1)
		instruction.SetOperand(0, 0x0102030405060708)
		require.Equal(t, []uint64{0x0102030405060708}, instruction.Operands())
	})
	t.Run("dynamic", func(t *testing.T) {
		instruction := instr.MustNew(instr.BR_TABLE, 2, 0, 1, 0)
		instruction.SetOperand(1, 5)
		require.Equal(t, []uint64{2, 5, 1, 0}, instruction.Operands())
	})
	t.Run("dynamic count", func(t *testing.T) {
		instruction := instr.MustNew(instr.BR_TABLE, 2, 0, 1, 0)
		instruction.SetOperand(0, 1)
		require.Equal(t, []uint64{1, 0, 1}, instruction.Operands())
	})
	t.Run("out of range", func(t *testing.T) {
		instruction := instr.MustNew(instr.BR, 5)
		instruction.SetOperand(1, 9)
		require.Equal(t, []uint64{5}, instruction.Operands())
	})
}

func TestInstruction_Operand(t *testing.T) {
	t.Run("in range", func(t *testing.T) {
		instruction := instr.MustNew(instr.BR, 5)
		require.Equal(t, uint64(5), instruction.Operand(0))
	})
	t.Run("out of range", func(t *testing.T) {
		instruction := instr.MustNew(instr.BR, 5)
		require.Zero(t, instruction.Operand(1))
	})
}

func TestInstruction_Width(t *testing.T) {
	t.Run("static", func(t *testing.T) {
		instruction := instr.MustNew(instr.NOP)
		require.Equal(t, 1, instruction.Width())
	})
	t.Run("dynamic", func(t *testing.T) {
		instruction := instr.MustNew(instr.BR_TABLE, 2, 0, 1, 0)
		require.Equal(t, 8, instruction.Width())
	})
}

func TestInstruction_Operands(t *testing.T) {
	t.Run("static", func(t *testing.T) {
		instruction := instr.MustNew(instr.NOP)
		require.Empty(t, instruction.Operands())
	})
	t.Run("dynamic", func(t *testing.T) {
		instruction := instr.MustNew(instr.BR_TABLE, 2, 0, 1, 0)
		require.Equal(t, []uint64{2, 0, 1, 0}, instruction.Operands())
	})
}

func TestInstruction_Opcode(t *testing.T) {
	instruction := instr.MustNew(instr.NOP)
	require.Equal(t, instr.NOP, instruction.Opcode())
}

func TestInstruction_Type(t *testing.T) {
	instruction := instr.MustNew(instr.I32_CONST, 42)
	require.Equal(t, instr.Type{
		Mnemonic: "i32.const",
		Widths:   []int{4},
		Push:     []instr.Kind{instr.KindI32},
	}, instruction.Type())
}

func TestInstruction_String(t *testing.T) {
	t.Run("static", func(t *testing.T) {
		instruction := instr.MustNew(instr.NOP)
		require.Equal(t, "nop", instruction.String())
	})
	t.Run("dynamic", func(t *testing.T) {
		instruction := instr.MustNew(instr.BR_TABLE, 2, 0, 1, 0)
		require.Equal(t, "br_table 0x02 0x0000 0x0001 0x0000", instruction.String())
	})
}
