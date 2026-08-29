package arm64_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/internal/asm"
	arm64 "github.com/siyul-park/minivm/internal/asm/arm64"
)

// TestArch_Relax covers the structural contract of asm.Relaxer.Relax:
// in-range branches are left alone, out-of-range B.cond/CBZ/CBNZ branches
// are rewritten into an inverted skip branch plus an in-range unconditional
// B to the original label, TBZ/TBNZ (which never carry a LabelOperand in
// this codebase) are never relaxed, and a target so far away that even the
// replacement B would be out of range is rejected so Build falls back to
// ErrBranchOutOfRange.
func TestArch_Relax(t *testing.T) {
	relaxer, ok := arm64.New().(asm.Relaxer)
	require.True(t, ok)

	label := asm.Label(7)
	target := asm.LabelOperand{ID: label}
	// The inverted branch clears the single unconditional B that follows it;
	// branch displacements are relative to the branch itself, so that is +8.
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
			require.True(t, relaxed, pair[0])
			require.Len(t, repl, 2, pair[0])
			require.Equal(t, uint16(pair[1]), repl[0].Op, pair[0])
			require.Equal(t, skip, repl[0].Src2, pair[0])
			require.Equal(t, uint16(arm64.OpB), repl[1].Op, pair[0])
			require.Equal(t, target, repl[1].Src2, pair[0])
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
	})

	t.Run("TBZ/TBNZ never carry a label operand and are never relaxed", func(t *testing.T) {
		_, relaxed := relaxer.Relax(arm64.TBZ(arm64.X1, 3, 1<<17), 1<<17)
		require.False(t, relaxed)

		_, relaxed = relaxer.Relax(arm64.TBNZ(arm64.X1, 3, 1<<17), 1<<17)
		require.False(t, relaxed)
	})

	t.Run("target beyond the B's own imm26 range is rejected", func(t *testing.T) {
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
