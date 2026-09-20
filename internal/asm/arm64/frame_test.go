package arm64_test

import (
	"testing"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/stretchr/testify/require"
)

func TestArch_Flow(t *testing.T) {
	var frame asm.Frame = arm64.New()
	tests := []struct {
		name string
		inst asm.Instruction
		want asm.Flow
	}{
		{"ADD", arm64.ADD(arm64.X0, arm64.X1, arm64.X2), asm.FlowNext},
		{"STR", arm64.STR(arm64.X0, arm64.X1, 0), asm.FlowNext},
		{"B label", arm64.BLabel(0), asm.FlowJump},
		{"B", arm64.B(8), asm.FlowJump},
		{"BR", arm64.BR(arm64.X0), asm.FlowEnd},
		{"BL", arm64.BL(8), asm.FlowCall},
		{"BLR", arm64.BLR(arm64.X0), asm.FlowCall},
		{"RET", arm64.RET(), asm.FlowEnd},
		{"BRK", arm64.BRK(0), asm.FlowEnd},
		{"CBZ", arm64.CBZLabel(arm64.X0, 0), asm.FlowBranch},
		{"CBNZ", arm64.CBNZLabel(arm64.X0, 0), asm.FlowBranch},
		{"TBZ", arm64.TBZ(arm64.X0, 1, 8), asm.FlowBranch},
		{"B.NE", arm64.BCondLabel(arm64.OpBNE, 0), asm.FlowBranch},
		{"B.GE", arm64.BGE(8), asm.FlowBranch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, frame.Flow(tt.inst))
		})
	}
}

func TestArch_Writes(t *testing.T) {
	var frame asm.Frame = arm64.New()
	tests := []struct {
		name string
		inst asm.Instruction
		want [4]bool
	}{
		{"ADD", arm64.ADD(arm64.X0, arm64.X1, arm64.X2), [4]bool{true, false, false, false}},
		{"MOVI", arm64.MOVI(arm64.X0, 1), [4]bool{true, false, false, false}},
		{"LDR", arm64.LDR(arm64.X0, arm64.X1, 0), [4]bool{true, false, false, false}},
		{"LDP", arm64.LDP(arm64.X0, arm64.X1, arm64.X2, 0), [4]bool{true, false, true, false}},
		{"STR", arm64.STR(arm64.X0, arm64.X1, 0), [4]bool{}},
		{"STRW", arm64.STRW(arm64.W0, arm64.X1, 0), [4]bool{}},
		{"STP", arm64.STP(arm64.X0, arm64.X1, arm64.X2, 0), [4]bool{}},
		{"CMP", arm64.CMP(arm64.X0, arm64.X1), [4]bool{}},
		{"CMPI", arm64.CMPI(arm64.X0, 1), [4]bool{}},
		{"TST", arm64.TST(arm64.X0, arm64.X1), [4]bool{}},
		{"FCMP", arm64.FCMP(arm64.D0, arm64.D1), [4]bool{}},
		{"CBZ", arm64.CBZLabel(arm64.X0, 0), [4]bool{}},
		{"BLR", arm64.BLR(arm64.X0), [4]bool{}},
		{"FMOV", arm64.FMOV(arm64.D0, arm64.X0), [4]bool{true, false, false, false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, frame.Writes(tt.inst))
		})
	}
}

func TestArch_Registers(t *testing.T) {
	var frame asm.Frame = arm64.New()
	t.Run("int", func(t *testing.T) {
		require.Equal(t, []asm.PReg{
			arm64.X0, arm64.X1, arm64.X2, arm64.X3, arm64.X4, arm64.X5, arm64.X6, arm64.X7,
			arm64.X8, arm64.X9, arm64.X10, arm64.X11, arm64.X12, arm64.X13, arm64.X14, arm64.X15,
			arm64.X19, arm64.X20, arm64.X21, arm64.X22, arm64.X23, arm64.X24, arm64.X25, arm64.X27,
		}, frame.Registers(asm.RegTypeInt))
	})
	t.Run("float", func(t *testing.T) {
		regs := frame.Registers(asm.RegTypeFloat)
		require.Len(t, regs, 32)
		require.Equal(t, arm64.D0, regs[0])
		require.Equal(t, arm64.D31, regs[31])
	})
}

func TestArch_Spill(t *testing.T) {
	var frame asm.Frame = arm64.New()
	tests := []struct {
		name string
		reg  asm.Reg
		slot int
		want asm.Instruction
	}{
		{"X", arm64.X3, 2, arm64.STR(arm64.X3, arm64.SP, 16)},
		{"W", arm64.W3, 1, arm64.STRW(arm64.W3, arm64.SP, 8)},
		{"D", arm64.D3, 0, arm64.STR(arm64.D3, arm64.SP, 0)},
		{"S", arm64.S3, 3, arm64.STR(arm64.S3, arm64.SP, 24)},
		{"virtual W", asm.NewVReg(9, asm.RegTypeInt, asm.Width32), 1, arm64.STRW(asm.NewVReg(9, asm.RegTypeInt, asm.Width32), arm64.SP, 8)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, frame.Spill(tt.reg, tt.slot))
		})
	}
}

func TestArch_Reload(t *testing.T) {
	var frame asm.Frame = arm64.New()
	tests := []struct {
		name string
		reg  asm.Reg
		slot int
		want asm.Instruction
	}{
		{"X", arm64.X3, 2, arm64.LDR(arm64.X3, arm64.SP, 16)},
		{"W", arm64.W3, 1, arm64.LDR(arm64.W3, arm64.SP, 8)},
		{"D", arm64.D3, 0, arm64.LDR(arm64.D3, arm64.SP, 0)},
		{"S", arm64.S3, 3, arm64.LDR(arm64.S3, arm64.SP, 24)},
		{"virtual D", asm.NewVReg(9, asm.RegTypeFloat, asm.Width64), 1, arm64.LDR(asm.NewVReg(9, asm.RegTypeFloat, asm.Width64), arm64.SP, 8)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, frame.Reload(tt.reg, tt.slot))
		})
	}
}
