package analysis

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestValueNumberingAnalysis_Run(t *testing.T) {
	i32 := func(n int) []types.Type {
		ls := make([]types.Type, n)
		for i := range ls {
			ls[i] = types.TypeI32
		}
		return ls
	}

	t.Run("redundant reusing a local home", func(t *testing.T) {
		fn := types.NewFunctionBuilder(nil).WithLocals(i32(3)...).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_SET, 2),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
		).MustBuild()

		vn := run(t, fn)
		require.Equal(t, map[int]Redundancy{
			11: {Start: 7, End: 12, Kind: instr.KindI32, Home: 2, Def: 5},
		}, vn.Redundant)
	})

	t.Run("redundant without a home needs a fresh slot", func(t *testing.T) {
		fn := types.NewFunctionBuilder(nil).WithLocals(i32(2)...).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
		).MustBuild()

		vn := run(t, fn)
		require.Equal(t, map[int]Redundancy{
			9: {Start: 5, End: 10, Kind: instr.KindI32, Home: -1, Def: 5},
		}, vn.Redundant)
	})

	t.Run("commutative operands canonicalize", func(t *testing.T) {
		fn := types.NewFunctionBuilder(nil).WithLocals(i32(2)...).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_ADD),
		).MustBuild()

		vn := run(t, fn)
		require.Len(t, vn.Redundant, 1)
	})

	t.Run("non-commutative operands do not match", func(t *testing.T) {
		fn := types.NewFunctionBuilder(nil).WithLocals(i32(2)...).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_SUB),
		).MustBuild()

		vn := run(t, fn)
		require.Empty(t, vn.Redundant)
	})

	t.Run("local store invalidates the expression", func(t *testing.T) {
		fn := types.NewFunctionBuilder(nil).WithLocals(i32(2)...).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.DROP),
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
		).MustBuild()

		vn := run(t, fn)
		require.Empty(t, vn.Redundant)
	})

	t.Run("block boundary resets numbering", func(t *testing.T) {
		fn := types.NewFunctionBuilder(nil).WithLocals(i32(2)...).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.DROP),
			instr.New(instr.BR, 0),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
		).MustBuild()

		vn := run(t, fn)
		require.Empty(t, vn.Redundant)
	})

	t.Run("call ends block numbering", func(t *testing.T) {
		fn := types.NewFunctionBuilder(nil).WithLocals(i32(2)...).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.DROP),
			instr.New(instr.CALL),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
		).MustBuild()

		vn := run(t, fn)
		require.Empty(t, vn.Redundant)
	})
}

func run(t *testing.T, fn *types.Function) *ValueNumbering {
	t.Helper()
	m := pass.NewManager()
	pass.Register(m, NewBasicBlocksAnalysis())
	pass.Register(m, NewValueNumberingAnalysis())
	vn, err := pass.GetResult[*ValueNumbering](m, fn)
	require.NoError(t, err)
	return vn
}
