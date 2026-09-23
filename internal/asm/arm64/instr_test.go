package arm64_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/internal/asm"
	arm64 "github.com/siyul-park/minivm/internal/asm/arm64"
)

func TestLDI(t *testing.T) {
	tests := []struct {
		val  uint64
		want []asm.Instruction
	}{
		{
			val:  0,
			want: []asm.Instruction{arm64.MOVZ(arm64.X0, 0, 0)},
		},
		{
			val:  0x1234,
			want: []asm.Instruction{arm64.MOVZ(arm64.X0, 0x1234, 0)},
		},
		{
			val:  0x7FF6000000000000,
			want: []asm.Instruction{arm64.MOVZ(arm64.X0, 0x7FF6, 48)},
		},
		{
			val:  0x1234000056780000,
			want: []asm.Instruction{arm64.MOVZ(arm64.X0, 0x5678, 16), arm64.MOVK(arm64.X0, 0x1234, 48)},
		},
		{
			val:  0x12345678,
			want: []asm.Instruction{arm64.MOVZ(arm64.X0, 0x5678, 0), arm64.MOVK(arm64.X0, 0x1234, 16)},
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%#x", tt.val), func(t *testing.T) {
			require.Equal(t, tt.want, arm64.LDI(arm64.X0, tt.val))
		})
	}
}

func TestADD(t *testing.T) {
	inst := arm64.ADD(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpADD), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestADDI(t *testing.T) {
	inst := arm64.ADDI(arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpADDI), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestADDS(t *testing.T) {
	inst := arm64.ADDS(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpADDS), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestADDSI(t *testing.T) {
	inst := arm64.ADDSI(arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpADDSI), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestSUB(t *testing.T) {
	inst := arm64.SUB(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpSUB), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestSUBI(t *testing.T) {
	inst := arm64.SUBI(arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpSUBI), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestSUBS(t *testing.T) {
	inst := arm64.SUBS(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpSUBS), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestSUBSI(t *testing.T) {
	inst := arm64.SUBSI(arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpSUBSI), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestNEG(t *testing.T) {
	inst := arm64.NEG(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpNEG), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestNEGS(t *testing.T) {
	inst := arm64.NEGS(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpNEGS), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestMUL(t *testing.T) {
	inst := arm64.MUL(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpMUL), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestSDIV(t *testing.T) {
	inst := arm64.SDIV(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpSDIV), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestUDIV(t *testing.T) {
	inst := arm64.UDIV(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpUDIV), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestMADD(t *testing.T) {
	inst := arm64.MADD(arm64.X0, arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpMADD), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestMSUB(t *testing.T) {
	inst := arm64.MSUB(arm64.X0, arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpMSUB), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestMNEG(t *testing.T) {
	inst := arm64.MNEG(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpMNEG), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestADC(t *testing.T) {
	inst := arm64.ADC(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpADC), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestADCS(t *testing.T) {
	inst := arm64.ADCS(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpADCS), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestSBC(t *testing.T) {
	inst := arm64.SBC(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpSBC), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestSBCS(t *testing.T) {
	inst := arm64.SBCS(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpSBCS), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestAND(t *testing.T) {
	inst := arm64.AND(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpAND), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestANDI(t *testing.T) {
	inst := arm64.ANDI(arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpANDI), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestANDS(t *testing.T) {
	inst := arm64.ANDS(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpANDS), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestANDSI(t *testing.T) {
	inst := arm64.ANDSI(arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpANDSI), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestORR(t *testing.T) {
	inst := arm64.ORR(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpORR), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestORRI(t *testing.T) {
	inst := arm64.ORRI(arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpORRI), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestEOR(t *testing.T) {
	inst := arm64.EOR(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpEOR), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestEORI(t *testing.T) {
	inst := arm64.EORI(arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpEORI), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBIC(t *testing.T) {
	inst := arm64.BIC(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpBIC), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBICS(t *testing.T) {
	inst := arm64.BICS(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpBICS), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestEON(t *testing.T) {
	inst := arm64.EON(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpEON), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestORN(t *testing.T) {
	inst := arm64.ORN(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpORN), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestMVN(t *testing.T) {
	inst := arm64.MVN(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpMVN), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestTST(t *testing.T) {
	inst := arm64.TST(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpTST), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestTSTI(t *testing.T) {
	inst := arm64.TSTI(arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpTSTI), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestLSL(t *testing.T) {
	inst := arm64.LSL(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpLSL), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestLSR(t *testing.T) {
	inst := arm64.LSR(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpLSR), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestASR(t *testing.T) {
	inst := arm64.ASR(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpASR), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestROR(t *testing.T) {
	inst := arm64.ROR(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpROR), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestLSLI(t *testing.T) {
	inst := arm64.LSLI(arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpLSLI), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestLSRI(t *testing.T) {
	inst := arm64.LSRI(arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpLSRI), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestASRI(t *testing.T) {
	inst := arm64.ASRI(arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpASRI), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestRORI(t *testing.T) {
	inst := arm64.RORI(arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpRORI), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestSBFX(t *testing.T) {
	inst := arm64.SBFX(arm64.X0, arm64.X0, 1, 1)
	require.Equal(t, uint16(arm64.OpSBFX), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestCLZ(t *testing.T) {
	inst := arm64.CLZ(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpCLZ), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestRBIT(t *testing.T) {
	inst := arm64.RBIT(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpRBIT), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestREV(t *testing.T) {
	inst := arm64.REV(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpREV), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestREV16(t *testing.T) {
	inst := arm64.REV16(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpREV16), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestREV32(t *testing.T) {
	inst := arm64.REV32(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpREV32), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestSXTB(t *testing.T) {
	inst := arm64.SXTB(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpSXTB), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestSXTH(t *testing.T) {
	inst := arm64.SXTH(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpSXTH), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestSXTW(t *testing.T) {
	inst := arm64.SXTW(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpSXTW), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestUXTB(t *testing.T) {
	inst := arm64.UXTB(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpUXTB), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestUXTH(t *testing.T) {
	inst := arm64.UXTH(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpUXTH), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestUXTW(t *testing.T) {
	inst := arm64.UXTW(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpUXTW), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestMOV(t *testing.T) {
	inst := arm64.MOV(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpMOV), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestMOVW(t *testing.T) {
	inst := arm64.MOVW(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpMOVW), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestMOVI(t *testing.T) {
	inst := arm64.MOVI(arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpMOVI), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestMOVZ(t *testing.T) {
	inst := arm64.MOVZ(arm64.X0, 1, 1)
	require.Equal(t, uint16(arm64.OpMOVZ), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestMOVK(t *testing.T) {
	inst := arm64.MOVK(arm64.X0, 1, 1)
	require.Equal(t, uint16(arm64.OpMOVK), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestMOVN(t *testing.T) {
	inst := arm64.MOVN(arm64.X0, 1, 1)
	require.Equal(t, uint16(arm64.OpMOVN), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestCMP(t *testing.T) {
	inst := arm64.CMP(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpCMP), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestCMPI(t *testing.T) {
	inst := arm64.CMPI(arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpCMPI), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestCMN(t *testing.T) {
	inst := arm64.CMN(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpCMN), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestCMNI(t *testing.T) {
	inst := arm64.CMNI(arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpCMNI), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestCCMP(t *testing.T) {
	inst := arm64.CCMP(arm64.X0, arm64.X0, 1, 1)
	require.Equal(t, uint16(arm64.OpCCMP), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestCCMPI(t *testing.T) {
	inst := arm64.CCMPI(arm64.X0, 1, 1, 1)
	require.Equal(t, uint16(arm64.OpCCMPI), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestLDR(t *testing.T) {
	inst := arm64.LDR(arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpLDR), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestSTR(t *testing.T) {
	inst := arm64.STR(arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpSTR), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestLDRB(t *testing.T) {
	inst := arm64.LDRB(arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpLDRB), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestLDRSB(t *testing.T) {
	inst := arm64.LDRSB(arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpLDRSB), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestSTRB(t *testing.T) {
	inst := arm64.STRB(arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpSTRB), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestLDRH(t *testing.T) {
	inst := arm64.LDRH(arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpLDRH), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestLDRSH(t *testing.T) {
	inst := arm64.LDRSH(arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpLDRSH), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestSTRH(t *testing.T) {
	inst := arm64.STRH(arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpSTRH), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestLDRSW(t *testing.T) {
	inst := arm64.LDRSW(arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpLDRSW), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestSTRW(t *testing.T) {
	inst := arm64.STRW(arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpSTRW), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestLDRR(t *testing.T) {
	inst := arm64.LDRR(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpLDRR), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestSTRR(t *testing.T) {
	inst := arm64.STRR(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpSTRR), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestLDP(t *testing.T) {
	inst := arm64.LDP(arm64.X0, arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpLDP), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestSTP(t *testing.T) {
	inst := arm64.STP(arm64.X0, arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpSTP), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestSCVTF(t *testing.T) {
	inst := arm64.SCVTF(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpSCVTF), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestUCVTF(t *testing.T) {
	inst := arm64.UCVTF(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpUCVTF), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFCVTZS(t *testing.T) {
	inst := arm64.FCVTZS(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFCVTZS), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFCVTZU(t *testing.T) {
	inst := arm64.FCVTZU(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFCVTZU), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFCVT(t *testing.T) {
	inst := arm64.FCVT(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFCVT), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFADD(t *testing.T) {
	inst := arm64.FADD(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFADD), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFSUB(t *testing.T) {
	inst := arm64.FSUB(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFSUB), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFMUL(t *testing.T) {
	inst := arm64.FMUL(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFMUL), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFDIV(t *testing.T) {
	inst := arm64.FDIV(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFDIV), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFMIN(t *testing.T) {
	inst := arm64.FMIN(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFMIN), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFMAX(t *testing.T) {
	inst := arm64.FMAX(arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFMAX), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFMADD(t *testing.T) {
	inst := arm64.FMADD(arm64.X0, arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFMADD), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFMSUB(t *testing.T) {
	inst := arm64.FMSUB(arm64.X0, arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFMSUB), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFNMADD(t *testing.T) {
	inst := arm64.FNMADD(arm64.X0, arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFNMADD), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFNMSUB(t *testing.T) {
	inst := arm64.FNMSUB(arm64.X0, arm64.X0, arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFNMSUB), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFABS(t *testing.T) {
	inst := arm64.FABS(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFABS), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFNEG(t *testing.T) {
	inst := arm64.FNEG(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFNEG), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFSQRT(t *testing.T) {
	inst := arm64.FSQRT(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFSQRT), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFRINTN(t *testing.T) {
	inst := arm64.FRINTN(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFRINTN), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFRINTM(t *testing.T) {
	inst := arm64.FRINTM(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFRINTM), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFRINTP(t *testing.T) {
	inst := arm64.FRINTP(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFRINTP), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFRINTZ(t *testing.T) {
	inst := arm64.FRINTZ(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFRINTZ), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestCNT(t *testing.T) {
	inst := arm64.CNT(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpCNT), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestADDV(t *testing.T) {
	inst := arm64.ADDV(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpADDV), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFMOV(t *testing.T) {
	inst := arm64.FMOV(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFMOV), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFCMP(t *testing.T) {
	inst := arm64.FCMP(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFCMP), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFCMPE(t *testing.T) {
	inst := arm64.FCMPE(arm64.X0, arm64.X0)
	require.Equal(t, uint16(arm64.OpFCMPE), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestCSEL(t *testing.T) {
	inst := arm64.CSEL(arm64.X0, arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpCSEL), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestCSINC(t *testing.T) {
	inst := arm64.CSINC(arm64.X0, arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpCSINC), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestCSINV(t *testing.T) {
	inst := arm64.CSINV(arm64.X0, arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpCSINV), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestCSNEG(t *testing.T) {
	inst := arm64.CSNEG(arm64.X0, arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpCSNEG), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestCSET(t *testing.T) {
	inst := arm64.CSET(arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpCSET), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestCSETM(t *testing.T) {
	inst := arm64.CSETM(arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpCSETM), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestFCSEL(t *testing.T) {
	inst := arm64.FCSEL(arm64.X0, arm64.X0, arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpFCSEL), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestB(t *testing.T) {
	inst := arm64.B(1)
	require.Equal(t, uint16(arm64.OpB), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBL(t *testing.T) {
	inst := arm64.BL(1)
	require.Equal(t, uint16(arm64.OpBL), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBR(t *testing.T) {
	inst := arm64.BR(arm64.X0)
	require.Equal(t, uint16(arm64.OpBR), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBLR(t *testing.T) {
	inst := arm64.BLR(arm64.X0)
	require.Equal(t, uint16(arm64.OpBLR), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestEXIT(t *testing.T) {
	inst := arm64.EXIT(arm64.X0)
	require.Equal(t, uint16(arm64.OpEXIT), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestRET(t *testing.T) {
	inst := arm64.RET()
	require.Equal(t, uint16(arm64.OpRET), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBLabel(t *testing.T) {
	inst := arm64.BLabel(asm.Label(1))
	require.Equal(t, uint16(arm64.OpB), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBLLabel(t *testing.T) {
	inst := arm64.BLLabel(asm.Label(1))
	require.Equal(t, uint16(arm64.OpBL), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBCondLabel(t *testing.T) {
	inst := arm64.BCondLabel(arm64.OpBEQ, asm.Label(1))
	require.Equal(t, uint16(arm64.OpBEQ), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestCBZ(t *testing.T) {
	inst := arm64.CBZ(arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpCBZ), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestCBNZ(t *testing.T) {
	inst := arm64.CBNZ(arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpCBNZ), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestCBZLabel(t *testing.T) {
	inst := arm64.CBZLabel(arm64.X0, asm.Label(1))
	require.Equal(t, uint16(arm64.OpCBZ), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestCBNZLabel(t *testing.T) {
	inst := arm64.CBNZLabel(arm64.X0, asm.Label(1))
	require.Equal(t, uint16(arm64.OpCBNZ), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestTBZ(t *testing.T) {
	inst := arm64.TBZ(arm64.X0, 1, 1)
	require.Equal(t, uint16(arm64.OpTBZ), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestTBNZ(t *testing.T) {
	inst := arm64.TBNZ(arm64.X0, 1, 1)
	require.Equal(t, uint16(arm64.OpTBNZ), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBEQ(t *testing.T) {
	inst := arm64.BEQ(1)
	require.Equal(t, uint16(arm64.OpBEQ), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBNE(t *testing.T) {
	inst := arm64.BNE(1)
	require.Equal(t, uint16(arm64.OpBNE), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBLT(t *testing.T) {
	inst := arm64.BLT(1)
	require.Equal(t, uint16(arm64.OpBLT), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBGT(t *testing.T) {
	inst := arm64.BGT(1)
	require.Equal(t, uint16(arm64.OpBGT), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBLE(t *testing.T) {
	inst := arm64.BLE(1)
	require.Equal(t, uint16(arm64.OpBLE), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBGE(t *testing.T) {
	inst := arm64.BGE(1)
	require.Equal(t, uint16(arm64.OpBGE), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBMI(t *testing.T) {
	inst := arm64.BMI(1)
	require.Equal(t, uint16(arm64.OpBMI), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBPL(t *testing.T) {
	inst := arm64.BPL(1)
	require.Equal(t, uint16(arm64.OpBPL), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBVS(t *testing.T) {
	inst := arm64.BVS(1)
	require.Equal(t, uint16(arm64.OpBVS), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBVC(t *testing.T) {
	inst := arm64.BVC(1)
	require.Equal(t, uint16(arm64.OpBVC), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBHI(t *testing.T) {
	inst := arm64.BHI(1)
	require.Equal(t, uint16(arm64.OpBHI), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBLS(t *testing.T) {
	inst := arm64.BLS(1)
	require.Equal(t, uint16(arm64.OpBLS), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBCS(t *testing.T) {
	inst := arm64.BCS(1)
	require.Equal(t, uint16(arm64.OpBCS), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBCC(t *testing.T) {
	inst := arm64.BCC(1)
	require.Equal(t, uint16(arm64.OpBCC), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestNOP(t *testing.T) {
	inst := arm64.NOP()
	require.Equal(t, uint16(arm64.OpNOP), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestHLT(t *testing.T) {
	inst := arm64.HLT()
	require.Equal(t, uint16(arm64.OpHLT), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestUSE(t *testing.T) {
	inst := arm64.USE(arm64.X0)
	require.Equal(t, uint16(arm64.OpUSE), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestBRK(t *testing.T) {
	inst := arm64.BRK(1)
	require.Equal(t, uint16(arm64.OpBRK), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestSVC(t *testing.T) {
	inst := arm64.SVC(1)
	require.Equal(t, uint16(arm64.OpSVC), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestERET(t *testing.T) {
	inst := arm64.ERET()
	require.Equal(t, uint16(arm64.OpERET), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestMRS(t *testing.T) {
	inst := arm64.MRS(arm64.X0, 1)
	require.Equal(t, uint16(arm64.OpMRS), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestMSR(t *testing.T) {
	inst := arm64.MSR(1, arm64.X0)
	require.Equal(t, uint16(arm64.OpMSR), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestISB(t *testing.T) {
	inst := arm64.ISB()
	require.Equal(t, uint16(arm64.OpISB), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestDSB(t *testing.T) {
	inst := arm64.DSB()
	require.Equal(t, uint16(arm64.OpDSB), inst.Op)
	require.NotEmpty(t, inst.String())
}

func TestDMB(t *testing.T) {
	inst := arm64.DMB()
	require.Equal(t, uint16(arm64.OpDMB), inst.Op)
	require.NotEmpty(t, inst.String())
}
