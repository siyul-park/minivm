package ssa_test

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestTypeOf(t *testing.T) {
	t.Run("mirrors every value kind", func(t *testing.T) {
		require.Equal(t, ssa.TypeI1, ssa.TypeOf(types.KindI1))
		require.Equal(t, ssa.TypeI8, ssa.TypeOf(types.KindI8))
		require.Equal(t, ssa.TypeI32, ssa.TypeOf(types.KindI32))
		require.Equal(t, ssa.TypeI64, ssa.TypeOf(types.KindI64))
		require.Equal(t, ssa.TypeF32, ssa.TypeOf(types.KindF32))
		require.Equal(t, ssa.TypeF64, ssa.TypeOf(types.KindF64))
		require.Equal(t, ssa.TypeRef, ssa.TypeOf(types.KindRef))
	})

	t.Run("rejects a kind with no representation", func(t *testing.T) {
		require.Equal(t, "invalid", ssa.TypeOf(instr.KindAny).String())
	})
}

func TestType_String(t *testing.T) {
	t.Run("names every type", func(t *testing.T) {
		require.Equal(t, "i1", ssa.TypeI1.String())
		require.Equal(t, "i8", ssa.TypeI8.String())
		require.Equal(t, "i32", ssa.TypeI32.String())
		require.Equal(t, "i64", ssa.TypeI64.String())
		require.Equal(t, "f32", ssa.TypeF32.String())
		require.Equal(t, "f64", ssa.TypeF64.String())
		require.Equal(t, "ref", ssa.TypeRef.String())
		require.Equal(t, "state", ssa.TypeState.String())
	})

	t.Run("names the zero type invalid", func(t *testing.T) {
		var zero ssa.Type
		require.Equal(t, "invalid", zero.String())
	})
}
