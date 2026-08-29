package arm64_test

import (
	"encoding/binary"
	"testing"

	"github.com/siyul-park/minivm/internal/asm"
	arm64 "github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/stretchr/testify/require"
)

func TestNewEncoder(t *testing.T) {
	require.NotNil(t, arm64.NewEncoder())
}

func TestEncoder_Encode(t *testing.T) {
	encoder := arm64.NewEncoder()

	goldens := []struct {
		name string
		inst asm.Instruction
		want uint32
	}{
		{"ADD X1,X2,X3", arm64.ADD(arm64.X1, arm64.X2, arm64.X3), 0x8B030041},
		{"ADD W1,W2,W3", arm64.ADD(arm64.W1, arm64.W2, arm64.W3), 0x0B030041},
		{"ADDS X1,X2,X3", arm64.ADDS(arm64.X1, arm64.X2, arm64.X3), 0xAB030041},
		{"SUB X1,X2,X3", arm64.SUB(arm64.X1, arm64.X2, arm64.X3), 0xCB030041},
		{"SUBS X1,X2,X3", arm64.SUBS(arm64.X1, arm64.X2, arm64.X3), 0xEB030041},
		{"MUL X1,X2,X3", arm64.MUL(arm64.X1, arm64.X2, arm64.X3), 0x9B037C41},
		{"MNEG X1,X2,X3", arm64.MNEG(arm64.X1, arm64.X2, arm64.X3), 0x9B03FC41},
		{"SDIV X1,X2,X3", arm64.SDIV(arm64.X1, arm64.X2, arm64.X3), 0x9AC30C41},
		{"UDIV X1,X2,X3", arm64.UDIV(arm64.X1, arm64.X2, arm64.X3), 0x9AC30841},
		{"ADC X1,X2,X3", arm64.ADC(arm64.X1, arm64.X2, arm64.X3), 0x9A030041},
		{"ADCS X1,X2,X3", arm64.ADCS(arm64.X1, arm64.X2, arm64.X3), 0xBA030041},
		{"SBC X1,X2,X3", arm64.SBC(arm64.X1, arm64.X2, arm64.X3), 0xDA030041},
		{"SBCS X1,X2,X3", arm64.SBCS(arm64.X1, arm64.X2, arm64.X3), 0xFA030041},
		{"AND X1,X2,X3", arm64.AND(arm64.X1, arm64.X2, arm64.X3), 0x8A030041},
		{"ANDS X1,X2,X3", arm64.ANDS(arm64.X1, arm64.X2, arm64.X3), 0xEA030041},
		{"ORR X1,X2,X3", arm64.ORR(arm64.X1, arm64.X2, arm64.X3), 0xAA030041},
		{"EOR X1,X2,X3", arm64.EOR(arm64.X1, arm64.X2, arm64.X3), 0xCA030041},
		{"BIC X1,X2,X3", arm64.BIC(arm64.X1, arm64.X2, arm64.X3), 0x8A230041},
		{"BICS X1,X2,X3", arm64.BICS(arm64.X1, arm64.X2, arm64.X3), 0xEA230041},
		{"EON X1,X2,X3", arm64.EON(arm64.X1, arm64.X2, arm64.X3), 0xCA230041},
		{"ORN X1,X2,X3", arm64.ORN(arm64.X1, arm64.X2, arm64.X3), 0xAA230041},
		{"LSL X1,X2,X3", arm64.LSL(arm64.X1, arm64.X2, arm64.X3), 0x9AC32041},
		{"LSR X1,X2,X3", arm64.LSR(arm64.X1, arm64.X2, arm64.X3), 0x9AC32441},
		{"ASR X1,X2,X3", arm64.ASR(arm64.X1, arm64.X2, arm64.X3), 0x9AC32841},
		{"ROR X1,X2,X3", arm64.ROR(arm64.X1, arm64.X2, arm64.X3), 0x9AC32C41},
		{"MADD X0,X1,X2,X3", arm64.MADD(arm64.X0, arm64.X1, arm64.X2, arm64.X3), 0x9B020C20},
		{"MSUB X0,X1,X2,X3", arm64.MSUB(arm64.X0, arm64.X1, arm64.X2, arm64.X3), 0x9B028C20},
		{"ADDI X1,X2,#42", arm64.ADDI(arm64.X1, arm64.X2, 42), 0x9100A841},
		{"ADDI W1,W2,#42", arm64.ADDI(arm64.W1, arm64.W2, 42), 0x1100A841},
		{"ADDSI X1,X2,#42", arm64.ADDSI(arm64.X1, arm64.X2, 42), 0xB100A841},
		{"SUBI X1,X2,#42", arm64.SUBI(arm64.X1, arm64.X2, 42), 0xD100A841},
		{"SUBSI X1,X2,#42", arm64.SUBSI(arm64.X1, arm64.X2, 42), 0xF100A841},
		{"ANDI X1,X2,#0xFF", arm64.ANDI(arm64.X1, arm64.X2, 0xFF), 0x92401C41},
		{"ANDSI X1,X2,#0xFF", arm64.ANDSI(arm64.X1, arm64.X2, 0xFF), 0xF2401C41},
		{"ORRI X1,X2,#0xFF", arm64.ORRI(arm64.X1, arm64.X2, 0xFF), 0xB2401C41},
		{"EORI X1,X2,#0xFF", arm64.EORI(arm64.X1, arm64.X2, 0xFF), 0xD2401C41},
		{"SBFX X1,X2,#0,#49", arm64.SBFX(arm64.X1, arm64.X2, 0, 49), 0x9340C041},
		{"SBFX X1,X2,#4,#10", arm64.SBFX(arm64.X1, arm64.X2, 4, 10), 0x93443441},
		{"SBFX W3,W4,#2,#8", arm64.SBFX(arm64.W3, arm64.W4, 2, 8), 0x13022483},
		{"CLZ X1,X2", arm64.CLZ(arm64.X1, arm64.X2), 0xDAC01041},
		{"CLZ W1,W2", arm64.CLZ(arm64.W1, arm64.W2), 0x5AC01041},
		{"RBIT X1,X2", arm64.RBIT(arm64.X1, arm64.X2), 0xDAC00041},
		{"REV16 X1,X2", arm64.REV16(arm64.X1, arm64.X2), 0xDAC00441},
		{"REV32 X1,X2", arm64.REV32(arm64.X1, arm64.X2), 0xDAC00841},
		{"CSEL X1,X2,X3,EQ", arm64.CSEL(arm64.X1, arm64.X2, arm64.X3, 0), 0x9A830041},
		{"CSINC X1,X2,X3,EQ", arm64.CSINC(arm64.X1, arm64.X2, arm64.X3, 0), 0x9A830441},
		{"CSINV X1,X2,X3,EQ", arm64.CSINV(arm64.X1, arm64.X2, arm64.X3, 0), 0xDA830041},
		{"CSNEG X1,X2,X3,EQ", arm64.CSNEG(arm64.X1, arm64.X2, arm64.X3, 0), 0xDA830441},
		{"CSET X1,EQ", arm64.CSET(arm64.X1, 0), 0x9A9F17E1},
		{"CSETM X1,EQ", arm64.CSETM(arm64.X1, 0), 0xDA9F13E1},
		{"MOVZ X1,#0x1234,LSL16", arm64.MOVZ(arm64.X1, 0x1234, 16), 0xD2A24681},
		{"MOVZ W1,#0x1234,LSL16", arm64.MOVZ(arm64.W1, 0x1234, 16), 0x52A24681},
		{"MOVK X1,#0x1234,LSL16", arm64.MOVK(arm64.X1, 0x1234, 16), 0xF2A24681},
		{"MOVN X1,#0x1234,LSL16", arm64.MOVN(arm64.X1, 0x1234, 16), 0x92A24681},
		{"LDR X1,[X2,#8]", arm64.LDR(arm64.X1, arm64.X2, 8), 0xF9400441},
		{"LDRB X1,[X2,#1]", arm64.LDRB(arm64.X1, arm64.X2, 1), 0x39400441},
		{"LDRSB X1,[X2,#1]", arm64.LDRSB(arm64.X1, arm64.X2, 1), 0x39800441},
		{"LDRH X1,[X2,#2]", arm64.LDRH(arm64.X1, arm64.X2, 2), 0x79400441},
		{"LDRSH X1,[X2,#2]", arm64.LDRSH(arm64.X1, arm64.X2, 2), 0x79800441},
		{"LDRSW X1,[X2,#4]", arm64.LDRSW(arm64.X1, arm64.X2, 4), 0xB9800441},
		{"STR X1,[X2,#8]", arm64.STR(arm64.X1, arm64.X2, 8), 0xF9000441},
		{"STRB X1,[X2,#1]", arm64.STRB(arm64.X1, arm64.X2, 1), 0x39000441},
		{"STRH X1,[X2,#2]", arm64.STRH(arm64.X1, arm64.X2, 2), 0x79000441},
		{"STRW X1,[X2,#4]", arm64.STRW(arm64.X1, arm64.X2, 4), 0xB9000441},
		{"FADD D1,D2,D3", arm64.FADD(arm64.D1, arm64.D2, arm64.D3), 0x1E632841},
		{"FADD S1,S2,S3", arm64.FADD(arm64.S1, arm64.S2, arm64.S3), 0x1E232841},
		{"FSUB D1,D2,D3", arm64.FSUB(arm64.D1, arm64.D2, arm64.D3), 0x1E633841},
		{"FMUL D1,D2,D3", arm64.FMUL(arm64.D1, arm64.D2, arm64.D3), 0x1E630841},
		{"FDIV D1,D2,D3", arm64.FDIV(arm64.D1, arm64.D2, arm64.D3), 0x1E631841},
		{"FMIN D1,D2,D3", arm64.FMIN(arm64.D1, arm64.D2, arm64.D3), 0x1E635841},
		{"FMIN S1,S2,S3", arm64.FMIN(arm64.S1, arm64.S2, arm64.S3), 0x1E235841},
		{"FMAX D1,D2,D3", arm64.FMAX(arm64.D1, arm64.D2, arm64.D3), 0x1E634841},
		{"FMAX S1,S2,S3", arm64.FMAX(arm64.S1, arm64.S2, arm64.S3), 0x1E234841},
		{"CNT D1,D2", arm64.CNT(arm64.D1, arm64.D2), 0x0E205841},
		{"ADDV D1,D2", arm64.ADDV(arm64.D1, arm64.D2), 0x0E31B841},
		{"FMADD D0,D1,D2,D3", arm64.FMADD(arm64.D0, arm64.D1, arm64.D2, arm64.D3), 0x1F420C20},
		{"FMSUB D0,D1,D2,D3", arm64.FMSUB(arm64.D0, arm64.D1, arm64.D2, arm64.D3), 0x1F428C20},
		{"FNMADD D0,D1,D2,D3", arm64.FNMADD(arm64.D0, arm64.D1, arm64.D2, arm64.D3), 0x1F620C20},
		{"FNMSUB D0,D1,D2,D3", arm64.FNMSUB(arm64.D0, arm64.D1, arm64.D2, arm64.D3), 0x1F628C20},
		{"SCVTF S1,W2", arm64.SCVTF(arm64.S1, arm64.W2), 0x1E220041},
		{"SCVTF S1,X2", arm64.SCVTF(arm64.S1, arm64.X2), 0x9E220041},
		{"SCVTF D1,W2", arm64.SCVTF(arm64.D1, arm64.W2), 0x1E620041},
		{"SCVTF D1,X2", arm64.SCVTF(arm64.D1, arm64.X2), 0x9E620041},
		{"UCVTF S1,W2", arm64.UCVTF(arm64.S1, arm64.W2), 0x1E230041},
		{"UCVTF S1,X2", arm64.UCVTF(arm64.S1, arm64.X2), 0x9E230041},
		{"UCVTF D1,W2", arm64.UCVTF(arm64.D1, arm64.W2), 0x1E630041},
		{"UCVTF D1,X2", arm64.UCVTF(arm64.D1, arm64.X2), 0x9E630041},
		{"FCVTZS W1,S2", arm64.FCVTZS(arm64.W1, arm64.S2), 0x1E380041},
		{"FCVTZS W1,D2", arm64.FCVTZS(arm64.W1, arm64.D2), 0x1E780041},
		{"FCVTZS X1,S2", arm64.FCVTZS(arm64.X1, arm64.S2), 0x9E380041},
		{"FCVTZS X1,D2", arm64.FCVTZS(arm64.X1, arm64.D2), 0x9E780041},
		{"FCVTZU W1,S2", arm64.FCVTZU(arm64.W1, arm64.S2), 0x1E390041},
		{"FCVTZU W1,D2", arm64.FCVTZU(arm64.W1, arm64.D2), 0x1E790041},
		{"FCVTZU X1,S2", arm64.FCVTZU(arm64.X1, arm64.S2), 0x9E390041},
		{"FCVTZU X1,D2", arm64.FCVTZU(arm64.X1, arm64.D2), 0x9E790041},
	}
	for _, tt := range goldens {
		t.Run(tt.name, func(t *testing.T) {
			got, err := encoder.Encode(tt.inst)
			require.NoError(t, err)
			require.Equal(t, tt.want, binary.LittleEndian.Uint32(got))
		})
	}

	t.Run("register offset load store scales slot index", func(t *testing.T) {
		got, err := encoder.Encode(arm64.LDRR(arm64.X3, arm64.X4, arm64.X5))
		require.NoError(t, err)
		require.Equal(t, []byte{0x83, 0x78, 0x65, 0xF8}, got)

		got, err = encoder.Encode(arm64.STRR(arm64.X3, arm64.X4, arm64.X5))
		require.NoError(t, err)
		require.Equal(t, []byte{0x83, 0x78, 0x25, 0xF8}, got)
	})

	invalid := []struct {
		name string
		inst asm.Instruction
		want error
	}{
		{
			"unsupported opcode",
			asm.Instruction{Op: 0xFFFF, Dst: asm.Physical(arm64.X1), Src1: asm.Physical(arm64.X2), Src2: asm.Physical(arm64.X3)},
			arm64.ErrUnsupportedOpcode,
		},
		{"mixed widths", arm64.ADD(arm64.X1, arm64.X2, arm64.W3), asm.ErrInvalidOperand},
		{
			"missing immediate",
			asm.Instruction{Op: uint16(arm64.OpADDI), Dst: asm.Physical(arm64.X1), Src1: asm.Physical(arm64.X2)},
			arm64.ErrMissingImmediate,
		},
		{"unencodable logical immediate", arm64.ANDI(arm64.X1, arm64.X2, 0), arm64.ErrMissingImmediate},
		{"int destination for SCVTF", arm64.SCVTF(arm64.X1, arm64.X2), asm.ErrInvalidOperand},
		{"float source for CLZ", arm64.CLZ(arm64.X1, arm64.D2), asm.ErrInvalidOperand},
		{"B offset unaligned", arm64.B(2), asm.ErrBranchOutOfRange},
		{"B offset exceeds imm26", arm64.B(1 << 27), asm.ErrBranchOutOfRange},
		{"BEQ offset exceeds imm19", arm64.BEQ(1 << 21), asm.ErrBranchOutOfRange},
		{"CBZ offset exceeds imm19", arm64.CBZ(arm64.X1, 1<<21), asm.ErrBranchOutOfRange},
		{"TBZ offset exceeds imm14", arm64.TBZ(arm64.X1, 3, 1<<17), asm.ErrBranchOutOfRange},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			_, err := encoder.Encode(tt.inst)
			require.ErrorIs(t, err, tt.want)
		})
	}
}
