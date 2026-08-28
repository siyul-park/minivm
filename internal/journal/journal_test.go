package journal_test

import (
	"testing"

	"github.com/siyul-park/minivm/internal/journal"
	"github.com/stretchr/testify/require"
)

// TestLayout asserts the two invariants that make the frame-journal layout
// safe to index by constant: every cell, record, and trap identity is
// distinct, and the frame record is exactly as wide as the stride the
// interpreter and native code both step by.
func TestLayout(t *testing.T) {
	t.Run("cell indices are distinct", func(t *testing.T) {
		cells := []journal.Cell{
			journal.CellStack, journal.CellGlobals, journal.CellBP, journal.CellSP,
			journal.CellEntry, journal.CellDepth, journal.CellCap, journal.CellTrap,
			journal.CellNextIP, journal.CellBudget, journal.CellActive, journal.CellRC,
			journal.CellUpvals, journal.CellHeap, journal.CellNatives, journal.CellExitID,
			journal.CellHead,
		}
		seen := make(map[journal.Cell]bool, len(cells))
		for _, c := range cells {
			require.False(t, seen[c], "duplicate cell index %d", c)
			seen[c] = true
		}
	})

	t.Run("record indices are distinct", func(t *testing.T) {
		records := []journal.Record{
			journal.RecordAddr, journal.RecordBP, journal.RecordIP, journal.RecordReturns,
		}
		seen := make(map[journal.Record]bool, len(records))
		for _, r := range records {
			require.False(t, seen[r], "duplicate record index %d", r)
			seen[r] = true
		}
	})

	t.Run("trap codes are distinct", func(t *testing.T) {
		traps := []journal.Trap{
			journal.TrapNone, journal.TrapFallback, journal.TrapOverflow, journal.TrapYield, journal.TrapBridge,
		}
		seen := make(map[journal.Trap]bool, len(traps))
		for _, tr := range traps {
			require.False(t, seen[tr], "duplicate trap code %d", tr)
			seen[tr] = true
		}
	})

	t.Run("stride matches the frame record width", func(t *testing.T) {
		require.Equal(t, int(journal.RecordReturns)+1, journal.Stride)
	})

	t.Run("shift matches stride in bytes", func(t *testing.T) {
		require.Equal(t, journal.Stride*8, 1<<journal.Shift)
	})
}

// TestLen asserts Len sizes a journal to hold the header plus n frame
// records, each Stride cells wide.
func TestLen(t *testing.T) {
	require.Equal(t, int(journal.CellHead), journal.Len(0))
	require.Equal(t, int(journal.CellHead)+journal.Stride, journal.Len(1))
	require.Equal(t, int(journal.CellHead)+journal.Stride*8, journal.Len(8))
}

// TestAt asserts At addresses frame records against the same layout Len
// sizes for: record 0 begins immediately after the header, and each
// following record starts Stride cells later.
func TestAt(t *testing.T) {
	t.Run("record 0 field 0 lands on the first cell after the header", func(t *testing.T) {
		require.Equal(t, journal.CellHead, journal.At(0, journal.RecordAddr))
	})

	t.Run("consecutive records are Stride apart", func(t *testing.T) {
		require.Equal(t, journal.Cell(journal.Stride), journal.At(1, journal.RecordAddr)-journal.At(0, journal.RecordAddr))
		require.Equal(t, journal.Cell(journal.Stride), journal.At(2, journal.RecordBP)-journal.At(1, journal.RecordBP))
	})

	t.Run("fields within a record follow record order", func(t *testing.T) {
		require.Equal(t, journal.At(0, journal.RecordAddr)+1, journal.At(0, journal.RecordBP))
		require.Equal(t, journal.At(0, journal.RecordBP)+1, journal.At(0, journal.RecordIP))
		require.Equal(t, journal.At(0, journal.RecordIP)+1, journal.At(0, journal.RecordReturns))
	})
}
