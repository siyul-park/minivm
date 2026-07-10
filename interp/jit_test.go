package interp

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/stretchr/testify/require"
)

// TestSpillSafe covers the spill-disable policy centralized in spillSafe: a
// trace tree may only use ordinary register spilling when neither its root
// nor any branch it can inline as a continuation ends in a terminal
// ARRAY_SET/STRUCT_SET heap mutation. Before this was centralized, emitRoot
// checked only the root trace's last op, so a learned pending continuation
// that itself ended in a mutation escaped the check.
func TestSpillSafe(t *testing.T) {
	t.Run("root ends in a plain op", func(t *testing.T) {
		tree := &tree{root: &trace{ops: []step{{op: instr.I32_ADD}}}}
		require.True(t, spillSafe(tree))
	})

	t.Run("root itself ends in a mutation", func(t *testing.T) {
		tree := &tree{root: &trace{ops: []step{{op: instr.I32_ADD}, {op: instr.ARRAY_SET}}}}
		require.False(t, spillSafe(tree))
	})

	t.Run("root is plain but a pending branch ends in a mutation", func(t *testing.T) {
		tree := &tree{
			root: &trace{ops: []step{{op: instr.I32_ADD}}},
			branches: map[int]*trace{
				0: {ops: []step{{op: instr.I32_CONST}, {op: instr.STRUCT_SET}}},
			},
		}
		require.False(t, spillSafe(tree))
	})

	t.Run("root and every branch are plain", func(t *testing.T) {
		tree := &tree{
			root: &trace{ops: []step{{op: instr.I32_ADD}}},
			branches: map[int]*trace{
				0: {ops: []step{{op: instr.I32_CONST}}},
				1: nil,
			},
		}
		require.True(t, spillSafe(tree))
	})
}
