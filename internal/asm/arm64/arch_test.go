package arm64_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
)

func TestFrame_Flow(t *testing.T) {
	var frame asm.Frame = arm64.New()
	tests := []struct {
		name string
		inst asm.Instruction
		want asm.Flow
	}{
		{"ADD", arm64.ADD(arm64.X0, arm64.X1, arm64.X2), asm.FlowNext},
		{"STR", arm64.STR(arm64.X0, arm64.X1, 0), asm.FlowNext},
		{"USE", arm64.USE(arm64.X0), asm.FlowNext},
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

func TestFrame_Writes(t *testing.T) {
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
		{"USE", arm64.USE(arm64.X0), [4]bool{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, frame.Writes(tt.inst))
		})
	}
}

func TestFrame_Registers(t *testing.T) {
	var frame asm.Frame = arm64.New()
	require.Equal(t, []asm.PReg{
		arm64.X0, arm64.X1, arm64.X2, arm64.X3, arm64.X4, arm64.X5, arm64.X6, arm64.X7,
		arm64.X8, arm64.X9, arm64.X10, arm64.X11, arm64.X12, arm64.X13, arm64.X14, arm64.X15,
		arm64.X19, arm64.X20, arm64.X21, arm64.X22, arm64.X23, arm64.X24, arm64.X25, arm64.X27,
	}, frame.Registers(asm.RegTypeInt))
	require.Equal(t, []asm.PReg{
		arm64.D0, arm64.D1, arm64.D2, arm64.D3, arm64.D4, arm64.D5, arm64.D6, arm64.D7,
		arm64.D8, arm64.D9, arm64.D10, arm64.D11, arm64.D12, arm64.D13, arm64.D14, arm64.D15,
		arm64.D16, arm64.D17, arm64.D18, arm64.D19, arm64.D20, arm64.D21, arm64.D22, arm64.D23,
		arm64.D24, arm64.D25, arm64.D26, arm64.D27, arm64.D28, arm64.D29, arm64.D30, arm64.D31,
	}, frame.Registers(asm.RegTypeFloat))
	regs := frame.Registers(asm.RegTypeInt)
	regs[0] = arm64.X1
	require.Equal(t, arm64.X0, frame.Registers(asm.RegTypeInt)[0])
}

func TestFrame_Spill(t *testing.T) {
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

func TestFrame_Reload(t *testing.T) {
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

// TestRelaxer_Relax covers the structural contract of asm.Relaxer.Relax:
// in-range branches are left alone, out-of-range B.cond/CBZ/CBNZ branches
// are rewritten into an inverted skip branch plus an in-range unconditional
// B to the original label, TBZ/TBNZ are never relaxed, and a replacement B
// that cannot reach the target is rejected.
func TestRelaxer_Relax(t *testing.T) {
	relaxer := arm64.New()

	label := asm.Label(7)
	target := asm.LabelOperand{ID: label}
	skip := asm.Imm(8)

	t.Run("in-range branch is left alone", func(t *testing.T) {
		_, relaxed := relaxer.Relax(arm64.BCondLabel(arm64.OpBEQ, label), 1<<10)
		require.False(t, relaxed)

		_, relaxed = relaxer.Relax(arm64.CBZLabel(arm64.X1, label), 1<<10)
		require.False(t, relaxed)
	})

	t.Run("out-of-range B.cond inverts condition and preserves target", func(t *testing.T) {
		pairs := [][2]arm64.Op{
			{arm64.OpBEQ, arm64.OpBNE}, {arm64.OpBNE, arm64.OpBEQ},
			{arm64.OpBCS, arm64.OpBCC}, {arm64.OpBCC, arm64.OpBCS},
			{arm64.OpBMI, arm64.OpBPL}, {arm64.OpBPL, arm64.OpBMI},
			{arm64.OpBVS, arm64.OpBVC}, {arm64.OpBVC, arm64.OpBVS},
			{arm64.OpBHI, arm64.OpBLS}, {arm64.OpBLS, arm64.OpBHI},
			{arm64.OpBGE, arm64.OpBLT}, {arm64.OpBLT, arm64.OpBGE},
			{arm64.OpBGT, arm64.OpBLE}, {arm64.OpBLE, arm64.OpBGT},
		}
		for _, pair := range pairs {
			repl, relaxed := relaxer.Relax(arm64.BCondLabel(pair[0], label), 1<<20)
			require.True(t, relaxed)
			require.Len(t, repl, 2)
			require.Equal(t, uint16(pair[1]), repl[0].Op)
			require.Equal(t, skip, repl[0].Src2)
			require.Equal(t, uint16(arm64.OpB), repl[1].Op)
			require.Equal(t, target, repl[1].Src2)
		}
	})

	t.Run("out-of-range CBZ/CBNZ inverts comparison and preserves register", func(t *testing.T) {
		repl, relaxed := relaxer.Relax(arm64.CBZLabel(arm64.X3, label), 1<<20)
		require.True(t, relaxed)
		require.Len(t, repl, 2)
		require.Equal(t, uint16(arm64.OpCBNZ), repl[0].Op)
		require.Equal(t, asm.Physical(arm64.X3), repl[0].Src1)
		require.Equal(t, skip, repl[0].Src2)
		require.Equal(t, uint16(arm64.OpB), repl[1].Op)
		require.Equal(t, target, repl[1].Src2)

		repl, relaxed = relaxer.Relax(arm64.CBNZLabel(arm64.X3, label), -(1 << 21))
		require.True(t, relaxed)
		require.Equal(t, uint16(arm64.OpCBZ), repl[0].Op)
		require.Equal(t, asm.Physical(arm64.X3), repl[0].Src1)
		require.Equal(t, skip, repl[0].Src2)
	})

	t.Run("TBZ/TBNZ never carry a label operand and are never relaxed", func(t *testing.T) {
		_, relaxed := relaxer.Relax(arm64.TBZ(arm64.X1, 3, 1<<17), 1<<17)
		require.False(t, relaxed)

		_, relaxed = relaxer.Relax(arm64.TBNZ(arm64.X1, 3, 1<<17), 1<<17)
		require.False(t, relaxed)
	})

	t.Run("target beyond the B range is rejected", func(t *testing.T) {
		_, relaxed := relaxer.Relax(arm64.BCondLabel(arm64.OpBEQ, label), 1<<28)
		require.False(t, relaxed)
	})

	t.Run("replacement B observes exact directional imm26 boundaries", func(t *testing.T) {
		_, relaxed := relaxer.Relax(arm64.BCondLabel(arm64.OpBEQ, label), (1<<27)-4)
		require.True(t, relaxed)

		_, relaxed = relaxer.Relax(arm64.BCondLabel(arm64.OpBEQ, label), 1<<27)
		require.False(t, relaxed)

		_, relaxed = relaxer.Relax(arm64.BCondLabel(arm64.OpBEQ, label), -(1<<27)+4)
		require.True(t, relaxed)

		_, relaxed = relaxer.Relax(arm64.BCondLabel(arm64.OpBEQ, label), -(1 << 27))
		require.False(t, relaxed)
	})

	t.Run("non-branch instruction is never relaxed", func(t *testing.T) {
		_, relaxed := relaxer.Relax(arm64.ADD(arm64.X1, arm64.X2, arm64.X3), 1<<20)
		require.False(t, relaxed)
	})
}
