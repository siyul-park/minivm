package arm64

import (
	"encoding/binary"
	"testing"

	"github.com/siyul-park/minivm/asm"
	"github.com/stretchr/testify/require"
)

func TestNewEncoder(t *testing.T) {
	require.NotNil(t, NewEncoder())
}

func TestEncoder_Encode(t *testing.T) {
	encoder := NewEncoder()

	goldens := []struct {
		name string
		inst asm.Instruction
		want uint32
	}{
		{"ADD X1,X2,X3", ADD(X1, X2, X3), 0x8B030041},
		{"ADD W1,W2,W3", ADD(W1, W2, W3), 0x0B030041},
		{"ADDS X1,X2,X3", ADDS(X1, X2, X3), 0xAB030041},
		{"SUB X1,X2,X3", SUB(X1, X2, X3), 0xCB030041},
		{"SUBS X1,X2,X3", SUBS(X1, X2, X3), 0xEB030041},
		{"MUL X1,X2,X3", MUL(X1, X2, X3), 0x9B037C41},
		{"MNEG X1,X2,X3", MNEG(X1, X2, X3), 0x9B03FC41},
		{"SDIV X1,X2,X3", SDIV(X1, X2, X3), 0x9AC30C41},
		{"UDIV X1,X2,X3", UDIV(X1, X2, X3), 0x9AC30841},
		{"ADC X1,X2,X3", ADC(X1, X2, X3), 0x9A030041},
		{"ADCS X1,X2,X3", ADCS(X1, X2, X3), 0xBA030041},
		{"SBC X1,X2,X3", SBC(X1, X2, X3), 0xDA030041},
		{"SBCS X1,X2,X3", SBCS(X1, X2, X3), 0xFA030041},
		{"AND X1,X2,X3", AND(X1, X2, X3), 0x8A030041},
		{"ANDS X1,X2,X3", ANDS(X1, X2, X3), 0xEA030041},
		{"ORR X1,X2,X3", ORR(X1, X2, X3), 0xAA030041},
		{"EOR X1,X2,X3", EOR(X1, X2, X3), 0xCA030041},
		{"BIC X1,X2,X3", BIC(X1, X2, X3), 0x8A230041},
		{"BICS X1,X2,X3", BICS(X1, X2, X3), 0xEA230041},
		{"EON X1,X2,X3", EON(X1, X2, X3), 0xCA230041},
		{"ORN X1,X2,X3", ORN(X1, X2, X3), 0xAA230041},
		{"LSL X1,X2,X3", LSL(X1, X2, X3), 0x9AC32041},
		{"LSR X1,X2,X3", LSR(X1, X2, X3), 0x9AC32441},
		{"ASR X1,X2,X3", ASR(X1, X2, X3), 0x9AC32841},
		{"ROR X1,X2,X3", ROR(X1, X2, X3), 0x9AC32C41},
		{"MADD X0,X1,X2,X3", MADD(X0, X1, X2, X3), 0x9B020C20},
		{"MSUB X0,X1,X2,X3", MSUB(X0, X1, X2, X3), 0x9B028C20},
		{"ADDI X1,X2,#42", ADDI(X1, X2, 42), 0x9100A841},
		{"ADDI W1,W2,#42", ADDI(W1, W2, 42), 0x1100A841},
		{"ADDSI X1,X2,#42", ADDSI(X1, X2, 42), 0xB100A841},
		{"SUBI X1,X2,#42", SUBI(X1, X2, 42), 0xD100A841},
		{"SUBSI X1,X2,#42", SUBSI(X1, X2, 42), 0xF100A841},
		{"ANDI X1,X2,#0xFF", ANDI(X1, X2, 0xFF), 0x92401C41},
		{"ANDSI X1,X2,#0xFF", ANDSI(X1, X2, 0xFF), 0xF2401C41},
		{"ORRI X1,X2,#0xFF", ORRI(X1, X2, 0xFF), 0xB2401C41},
		{"EORI X1,X2,#0xFF", EORI(X1, X2, 0xFF), 0xD2401C41},
		{"SBFX X1,X2,#0,#49", SBFX(X1, X2, 0, 49), 0x9340C041},
		{"SBFX X1,X2,#4,#10", SBFX(X1, X2, 4, 10), 0x93443441},
		{"SBFX W3,W4,#2,#8", SBFX(W3, W4, 2, 8), 0x13022483},
		{"CLZ X1,X2", CLZ(X1, X2), 0xDAC01041},
		{"CLZ W1,W2", CLZ(W1, W2), 0x5AC01041},
		{"RBIT X1,X2", RBIT(X1, X2), 0xDAC00041},
		{"REV16 X1,X2", REV16(X1, X2), 0xDAC00441},
		{"REV32 X1,X2", REV32(X1, X2), 0xDAC00841},
		{"CSEL X1,X2,X3,EQ", CSEL(X1, X2, X3, 0), 0x9A830041},
		{"CSINC X1,X2,X3,EQ", CSINC(X1, X2, X3, 0), 0x9A830441},
		{"CSINV X1,X2,X3,EQ", CSINV(X1, X2, X3, 0), 0xDA830041},
		{"CSNEG X1,X2,X3,EQ", CSNEG(X1, X2, X3, 0), 0xDA830441},
		{"CSET X1,EQ", CSET(X1, 0), 0x9A9F17E1},
		{"CSETM X1,EQ", CSETM(X1, 0), 0xDA9F13E1},
		{"MOVZ X1,#0x1234,LSL16", MOVZ(X1, 0x1234, 16), 0xD2A24681},
		{"MOVZ W1,#0x1234,LSL16", MOVZ(W1, 0x1234, 16), 0x52A24681},
		{"MOVK X1,#0x1234,LSL16", MOVK(X1, 0x1234, 16), 0xF2A24681},
		{"MOVN X1,#0x1234,LSL16", MOVN(X1, 0x1234, 16), 0x92A24681},
		{"LDR X1,[X2,#8]", LDR(X1, X2, 8), 0xF9400441},
		{"LDRB X1,[X2,#1]", LDRB(X1, X2, 1), 0x39400441},
		{"LDRSB X1,[X2,#1]", LDRSB(X1, X2, 1), 0x39800441},
		{"LDRH X1,[X2,#2]", LDRH(X1, X2, 2), 0x79400441},
		{"LDRSH X1,[X2,#2]", LDRSH(X1, X2, 2), 0x79800441},
		{"LDRSW X1,[X2,#4]", LDRSW(X1, X2, 4), 0xB9800441},
		{"STR X1,[X2,#8]", STR(X1, X2, 8), 0xF9000441},
		{"STRB X1,[X2,#1]", STRB(X1, X2, 1), 0x39000441},
		{"STRH X1,[X2,#2]", STRH(X1, X2, 2), 0x79000441},
		{"STRW X1,[X2,#4]", STRW(X1, X2, 4), 0xB9000441},
		{"FADD D1,D2,D3", FADD(D1, D2, D3), 0x1E632841},
		{"FADD S1,S2,S3", FADD(S1, S2, S3), 0x1E232841},
		{"FSUB D1,D2,D3", FSUB(D1, D2, D3), 0x1E633841},
		{"FMUL D1,D2,D3", FMUL(D1, D2, D3), 0x1E630841},
		{"FDIV D1,D2,D3", FDIV(D1, D2, D3), 0x1E631841},
		{"FMIN D1,D2,D3", FMIN(D1, D2, D3), 0x1E635841},
		{"FMIN S1,S2,S3", FMIN(S1, S2, S3), 0x1E235841},
		{"FMAX D1,D2,D3", FMAX(D1, D2, D3), 0x1E634841},
		{"FMAX S1,S2,S3", FMAX(S1, S2, S3), 0x1E234841},
		{"CNT D1,D2", CNT(D1, D2), 0x0E205841},
		{"ADDV D1,D2", ADDV(D1, D2), 0x0E31B841},
		{"FMADD D0,D1,D2,D3", FMADD(D0, D1, D2, D3), 0x1F420C20},
		{"FMSUB D0,D1,D2,D3", FMSUB(D0, D1, D2, D3), 0x1F428C20},
		{"FNMADD D0,D1,D2,D3", FNMADD(D0, D1, D2, D3), 0x1F620C20},
		{"FNMSUB D0,D1,D2,D3", FNMSUB(D0, D1, D2, D3), 0x1F628C20},
		{"SCVTF S1,W2", SCVTF(S1, W2), 0x1E220041},
		{"SCVTF S1,X2", SCVTF(S1, X2), 0x9E220041},
		{"SCVTF D1,W2", SCVTF(D1, W2), 0x1E620041},
		{"SCVTF D1,X2", SCVTF(D1, X2), 0x9E620041},
		{"UCVTF S1,W2", UCVTF(S1, W2), 0x1E230041},
		{"UCVTF S1,X2", UCVTF(S1, X2), 0x9E230041},
		{"UCVTF D1,W2", UCVTF(D1, W2), 0x1E630041},
		{"UCVTF D1,X2", UCVTF(D1, X2), 0x9E630041},
		{"FCVTZS W1,S2", FCVTZS(W1, S2), 0x1E380041},
		{"FCVTZS W1,D2", FCVTZS(W1, D2), 0x1E780041},
		{"FCVTZS X1,S2", FCVTZS(X1, S2), 0x9E380041},
		{"FCVTZS X1,D2", FCVTZS(X1, D2), 0x9E780041},
		{"FCVTZU W1,S2", FCVTZU(W1, S2), 0x1E390041},
		{"FCVTZU W1,D2", FCVTZU(W1, D2), 0x1E790041},
		{"FCVTZU X1,S2", FCVTZU(X1, S2), 0x9E390041},
		{"FCVTZU X1,D2", FCVTZU(X1, D2), 0x9E790041},
	}
	for _, tt := range goldens {
		t.Run(tt.name, func(t *testing.T) {
			got, err := encoder.Encode(tt.inst)
			require.NoError(t, err)
			require.Equal(t, tt.want, binary.LittleEndian.Uint32(got))
		})
	}

	t.Run("register offset load store scales slot index", func(t *testing.T) {
		got, err := encoder.Encode(LDRR(X3, X4, X5))
		require.NoError(t, err)
		require.Equal(t, []byte{0x83, 0x78, 0x65, 0xF8}, got)

		got, err = encoder.Encode(STRR(X3, X4, X5))
		require.NoError(t, err)
		require.Equal(t, []byte{0x83, 0x78, 0x25, 0xF8}, got)
	})

	invalid := []struct {
		name string
		inst asm.Instruction
		want error
	}{
		{"unsupported opcode", newReg3(Op(0xFFFF), X1, X2, X3), ErrUnsupportedOpcode},
		{"mixed widths", ADD(X1, X2, W3), asm.ErrInvalidOperand},
		{"missing immediate", newReg2(OpADDI, X1, X2), ErrMissingImmediate},
		{"unencodable logical immediate", ANDI(X1, X2, 0), ErrMissingImmediate},
		{"int destination for SCVTF", SCVTF(X1, X2), asm.ErrInvalidOperand},
		{"float source for CLZ", CLZ(X1, D2), asm.ErrInvalidOperand},
		{"B offset unaligned", B(2), asm.ErrBranchOutOfRange},
		{"B offset exceeds imm26", B(1 << 27), asm.ErrBranchOutOfRange},
		{"BEQ offset exceeds imm19", BEQ(1 << 21), asm.ErrBranchOutOfRange},
		{"CBZ offset exceeds imm19", CBZ(X1, 1<<21), asm.ErrBranchOutOfRange},
		{"TBZ offset exceeds imm14", TBZ(X1, 3, 1<<17), asm.ErrBranchOutOfRange},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			_, err := encoder.Encode(tt.inst)
			require.ErrorIs(t, err, tt.want)
		})
	}
}
