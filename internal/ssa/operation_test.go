package ssa_test

import (
	"testing"

	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/stretchr/testify/require"
)

func TestOp_String(t *testing.T) {
	t.Run("names every operation", func(t *testing.T) {
		names := map[ssa.Op]string{
			ssa.OpConst:       "const",
			ssa.OpExec:        "exec",
			ssa.OpLoad:        "load",
			ssa.OpStore:       "store",
			ssa.OpGuardKind:   "guard.kind",
			ssa.OpGuardShape:  "guard.shape",
			ssa.OpGuardBounds: "guard.bounds",
			ssa.OpGuardValue:  "guard.value",
			ssa.OpRetain:      "retain",
			ssa.OpRelease:     "release",
			ssa.OpBridge:      "bridge",
			ssa.OpState:       "state",
			ssa.OpJump:        "jump",
			ssa.OpBranch:      "br",
			ssa.OpTable:       "table",
			ssa.OpReturn:      "return",
			ssa.OpComplete:    "complete",
			ssa.OpExit:        "exit",
			ssa.OpSuspend:     "suspend",
		}
		seen := make(map[string]bool, len(names))
		for op, name := range names {
			require.Equal(t, name, op.String())
			require.False(t, seen[name], "duplicate operation name %q", name)
			seen[name] = true
		}
	})

	t.Run("names an unknown operation invalid", func(t *testing.T) {
		require.Equal(t, "invalid", (ssa.OpSuspend + 1).String())
	})
}

func TestSpace_String(t *testing.T) {
	t.Run("names every storage space", func(t *testing.T) {
		require.Equal(t, "local", ssa.SpaceLocal.String())
		require.Equal(t, "global", ssa.SpaceGlobal.String())
		require.Equal(t, "upval", ssa.SpaceUpval.String())
	})

	t.Run("names an unknown space invalid", func(t *testing.T) {
		require.Equal(t, "invalid", (ssa.SpaceUpval + 1).String())
	})
}
