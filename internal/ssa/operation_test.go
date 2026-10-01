package ssa_test

import (
	"testing"

	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/stretchr/testify/require"
)

func TestOp_String(t *testing.T) {
	for _, tt := range []struct {
		name string
		op   ssa.Op
		want string
	}{
		{"const", ssa.OpConst, "const"},
		{"exec", ssa.OpExec, "exec"},
		{"load", ssa.OpLoad, "load"},
		{"store", ssa.OpStore, "store"},
		{"guard kind", ssa.OpGuardKind, "guard.kind"},
		{"guard shape", ssa.OpGuardShape, "guard.shape"},
		{"guard bounds", ssa.OpGuardBounds, "guard.bounds"},
		{"guard value", ssa.OpGuardValue, "guard.value"},
		{"retain", ssa.OpRetain, "retain"},
		{"release", ssa.OpRelease, "release"},
		{"state", ssa.OpState, "state"},
		{"jump", ssa.OpJump, "jump"},
		{"branch", ssa.OpBranch, "br"},
		{"table", ssa.OpTable, "table"},
		{"return", ssa.OpReturn, "return"},
		{"complete", ssa.OpComplete, "complete"},
		{"exit", ssa.OpExit, "exit"},
		{"unknown operation", ssa.OpExit + 1, "invalid"},
	} {
		t.Run("names "+tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.op.String())
		})
	}
}

func TestSpace_String(t *testing.T) {
	for _, tt := range []struct {
		name  string
		space ssa.Space
		want  string
	}{
		{"local", ssa.SpaceLocal, "local"},
		{"global", ssa.SpaceGlobal, "global"},
		{"upval", ssa.SpaceUpval, "upval"},
		{"unknown space", ssa.SpaceUpval + 1, "invalid"},
	} {
		t.Run("names "+tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.space.String())
		})
	}
}
