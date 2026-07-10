package arm64

import (
	"testing"

	"github.com/siyul-park/minivm/asm"
	"github.com/stretchr/testify/require"
)

// TestArch_Relax covers the structural contract of Relax: in-range
// branches are left alone, out-of-range B.cond/CBZ/CBNZ branches are
// rewritten into an inverted skip branch plus an in-range unconditional B
// to the original label, TBZ/TBNZ (which never carry a LabelOperand in
// this codebase) are never relaxed, and a target so far away that even
// the replacement B would be out of range is rejected so encode falls
// back to ErrBranchOutOfRange.
func TestArch_Relax(t *testing.T) {
	a := arch{}
	lbl := asm.Label(7)

	t.Run("in-range branch is left alone", func(t *testing.T) {
		_, relaxed := a.Relax(BCondLabel(OpBEQ, lbl), 1<<10)
		require.False(t, relaxed)

		_, relaxed = a.Relax(CBZLabel(X1, lbl), 1<<10)
		require.False(t, relaxed)
	})

	t.Run("out-of-range B.cond inverts condition and preserves target", func(t *testing.T) {
		for op, inv := range invCond {
			repl, relaxed := a.Relax(BCondLabel(op, lbl), 1<<20)
			require.True(t, relaxed, op)
			require.Len(t, repl, 2, op)
			require.Equal(t, uint16(inv), repl[0].Op, op)
			require.Equal(t, asm.Imm(skipDisp), repl[0].Src2, op)
			require.Equal(t, uint16(OpB), repl[1].Op, op)
			require.Equal(t, asm.LabelOp(lbl), repl[1].Src2, op)
		}
	})

	t.Run("out-of-range CBZ/CBNZ inverts comparison and preserves register", func(t *testing.T) {
		repl, relaxed := a.Relax(CBZLabel(X3, lbl), 1<<20)
		require.True(t, relaxed)
		require.Len(t, repl, 2)
		require.Equal(t, uint16(OpCBNZ), repl[0].Op)
		require.Equal(t, asm.P(X3), repl[0].Src1)
		require.Equal(t, asm.Imm(skipDisp), repl[0].Src2)
		require.Equal(t, uint16(OpB), repl[1].Op)
		require.Equal(t, asm.LabelOp(lbl), repl[1].Src2)

		repl, relaxed = a.Relax(CBNZLabel(X3, lbl), -(1 << 21))
		require.True(t, relaxed)
		require.Equal(t, uint16(OpCBZ), repl[0].Op)
		require.Equal(t, asm.P(X3), repl[0].Src1)
	})

	t.Run("TBZ/TBNZ never carry a label operand and are never relaxed", func(t *testing.T) {
		_, relaxed := a.Relax(TBZ(X1, 3, 1<<17), 1<<17)
		require.False(t, relaxed)

		_, relaxed = a.Relax(TBNZ(X1, 3, 1<<17), 1<<17)
		require.False(t, relaxed)
	})

	t.Run("target beyond the B's own imm26 range is rejected", func(t *testing.T) {
		_, relaxed := a.Relax(BCondLabel(OpBEQ, lbl), 1<<28)
		require.False(t, relaxed)
	})

	t.Run("replacement B observes exact directional imm26 boundaries", func(t *testing.T) {
		_, relaxed := a.Relax(BCondLabel(OpBEQ, lbl), (1<<27)-4)
		require.True(t, relaxed)

		_, relaxed = a.Relax(BCondLabel(OpBEQ, lbl), 1<<27)
		require.False(t, relaxed)

		_, relaxed = a.Relax(BCondLabel(OpBEQ, lbl), -(1<<27)+4)
		require.True(t, relaxed)

		_, relaxed = a.Relax(BCondLabel(OpBEQ, lbl), -(1 << 27))
		require.False(t, relaxed)
	})

	t.Run("non-branch instruction is never relaxed", func(t *testing.T) {
		_, relaxed := a.Relax(ADD(X1, X2, X3), 1<<20)
		require.False(t, relaxed)
	})
}
