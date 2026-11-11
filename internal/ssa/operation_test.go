package ssa_test

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestOverflowsI64(t *testing.T) {
	require.True(t, ssa.OverflowsI64(instr.I64_ADD))
	require.True(t, ssa.OverflowsI64(instr.I64_DIV_U))
	require.False(t, ssa.OverflowsI64(instr.I64_AND))
}

func TestShape_Serves(t *testing.T) {
	i32 := ssa.Shape{Map: true, Kind: types.KindI32, Elem: types.KindRef}
	i64 := ssa.Shape{Map: true, Kind: types.KindI64, Elem: types.KindI64}
	tests := []struct {
		name   string
		shape  ssa.Shape
		code   instr.Opcode
		key    types.Kind
		value  types.Kind
		served bool
	}{
		{"len of any map of an integer key", i64, instr.MAP_LEN, 0, 0, true},
		{"get of a reference element", i32, instr.MAP_GET, types.KindI32, 0, true},
		{"lookup of a reference element", i32, instr.MAP_LOOKUP, types.KindI8, 0, true},
		{"get of an i64 element", i64, instr.MAP_GET, types.KindI64, 0, false},
		{"lookup of an i64 element", i64, instr.MAP_LOOKUP, types.KindI64, 0, false},
		{"set of a one-register value", i32, instr.MAP_SET, types.KindI32, types.KindF64, true},
		{"set of an i64 value", i32, instr.MAP_SET, types.KindI32, types.KindI64, false},
		{"delete of an i64 key", i64, instr.MAP_DELETE, types.KindI64, 0, true},
		{"i64 key held in a 32-bit lane", i64, instr.MAP_DELETE, types.KindI32, 0, false},
		{"i32 key held in a 64-bit word", i32, instr.MAP_DELETE, types.KindI64, 0, false},
		{"float key", ssa.Shape{Map: true, Kind: types.KindF64, Elem: types.KindI32}, instr.MAP_LEN, 0, 0, false},
		{"array shape", ssa.Shape{Kind: types.KindI32}, instr.MAP_LEN, 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.served, tt.shape.Serves(tt.code, tt.key, tt.value))
		})
	}
}

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
		{"slice", ssa.OpSlice, "slice"},
		{"bound", ssa.OpBound, "bound"},
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
