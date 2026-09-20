package ssa_test

import (
	"testing"

	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/stretchr/testify/require"
)

func TestOp_String(t *testing.T) {
	t.Run("names const", func(t *testing.T) {
		require.Equal(t, "const", ssa.OpConst.String())
	})

	t.Run("names exec", func(t *testing.T) {
		require.Equal(t, "exec", ssa.OpExec.String())
	})

	t.Run("names load", func(t *testing.T) {
		require.Equal(t, "load", ssa.OpLoad.String())
	})

	t.Run("names store", func(t *testing.T) {
		require.Equal(t, "store", ssa.OpStore.String())
	})

	t.Run("names guard kind", func(t *testing.T) {
		require.Equal(t, "guard.kind", ssa.OpGuardKind.String())
	})

	t.Run("names guard shape", func(t *testing.T) {
		require.Equal(t, "guard.shape", ssa.OpGuardShape.String())
	})

	t.Run("names guard bounds", func(t *testing.T) {
		require.Equal(t, "guard.bounds", ssa.OpGuardBounds.String())
	})

	t.Run("names guard value", func(t *testing.T) {
		require.Equal(t, "guard.value", ssa.OpGuardValue.String())
	})

	t.Run("names retain", func(t *testing.T) {
		require.Equal(t, "retain", ssa.OpRetain.String())
	})

	t.Run("names release", func(t *testing.T) {
		require.Equal(t, "release", ssa.OpRelease.String())
	})

	t.Run("names state", func(t *testing.T) {
		require.Equal(t, "state", ssa.OpState.String())
	})

	t.Run("names jump", func(t *testing.T) {
		require.Equal(t, "jump", ssa.OpJump.String())
	})

	t.Run("names branch", func(t *testing.T) {
		require.Equal(t, "br", ssa.OpBranch.String())
	})

	t.Run("names table", func(t *testing.T) {
		require.Equal(t, "table", ssa.OpTable.String())
	})

	t.Run("names return", func(t *testing.T) {
		require.Equal(t, "return", ssa.OpReturn.String())
	})

	t.Run("names complete", func(t *testing.T) {
		require.Equal(t, "complete", ssa.OpComplete.String())
	})

	t.Run("names exit", func(t *testing.T) {
		require.Equal(t, "exit", ssa.OpExit.String())
	})

	t.Run("names suspend", func(t *testing.T) {
		require.Equal(t, "suspend", ssa.OpSuspend.String())
	})

	t.Run("names an unknown operation invalid", func(t *testing.T) {
		require.Equal(t, "invalid", (ssa.OpSuspend + 1).String())
	})
}

func TestSpace_String(t *testing.T) {
	t.Run("names local", func(t *testing.T) {
		require.Equal(t, "local", ssa.SpaceLocal.String())
	})

	t.Run("names global", func(t *testing.T) {
		require.Equal(t, "global", ssa.SpaceGlobal.String())
	})

	t.Run("names upval", func(t *testing.T) {
		require.Equal(t, "upval", ssa.SpaceUpval.String())
	})

	t.Run("names an unknown space invalid", func(t *testing.T) {
		require.Equal(t, "invalid", (ssa.SpaceUpval + 1).String())
	})
}
