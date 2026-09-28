package ssa_test

import (
	"math"
	"testing"

	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
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

func TestWord(t *testing.T) {
	for _, c := range []struct {
		name string
		box  types.Boxed
		want uint64
	}{
		{"i1", types.BoxI1(true), 1},
		{"i8", types.BoxI8(-2), 0xFFFFFFFE},
		{"i32", types.BoxI32(-7), 0xFFFFFFF9},
		{"i64", types.BoxI64(-3), 0xFFFFFFFFFFFFFFFD},
		{"f32", types.BoxF32(1.5), uint64(math.Float32bits(1.5))},
		{"f64", types.BoxF64(2.5), math.Float64bits(2.5)},
		{"ref", types.BoxRef(9), uint64(types.BoxRef(9))},
	} {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, ssa.Word(c.box))
		})
	}
}
