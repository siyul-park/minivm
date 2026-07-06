package analysis

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestGlobalValueNumberingAnalysis_Run(t *testing.T) {
	i32t := &types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeI32}, Returns: []types.Type{types.TypeI32}}

	t.Run("within-block redundancy is captured like local CSE", func(t *testing.T) {
		fn := types.NewFunctionBuilder(i32t).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
		).MustBuild()

		m := pass.NewManager()
		pass.Register(m, NewBasicBlocksAnalysis())
		pass.Register(m, NewGlobalValueNumberingAnalysis())
		gvn, err := pass.GetResult[*GlobalValueNumbering](m, fn)
		require.NoError(t, err)
		require.Len(t, gvn.Redundant, 1)
		r := gvn.Redundant[9]
		require.Equal(t, 5, r.Start)
		require.Equal(t, 10, r.End)
		require.Equal(t, instr.KindI32, r.Kind)
		require.Equal(t, -1, r.Home)
		require.Equal(t, []int{5}, gvn.Defs[r.Def])
	})

	t.Run("redundancy at a control-flow merge", func(t *testing.T) {
		fb := types.NewFunctionBuilder(i32t)
		then, merge := fb.Label(), fb.Label()
		fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_GT_S))
		fb.BrIf(then)
		fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.DROP))
		fb.Br(merge)
		fb.Bind(then)
		fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.DROP))
		fb.Bind(merge)
		fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN))
		fn := fb.MustBuild()

		m := pass.NewManager()
		pass.Register(m, NewBasicBlocksAnalysis())
		pass.Register(m, NewGlobalValueNumberingAnalysis())
		gvn, err := pass.GetResult[*GlobalValueNumbering](m, fn)
		require.NoError(t, err)
		require.Len(t, gvn.Redundant, 1)
		var r Redundancy
		for _, v := range gvn.Redundant {
			r = v
		}
		require.Equal(t, -1, r.Home)
		require.Len(t, gvn.Defs[r.Def], 2, "captured at both arms")
	})

	t.Run("loop-invariant recomputation is redundant", func(t *testing.T) {
		fb := types.NewFunctionBuilder(i32t)
		top := fb.Label()
		fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.DROP))
		fb.Bind(top)
		fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.DROP))
		fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_GT_S))
		fb.BrIf(top)
		fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN))
		fn := fb.MustBuild()

		m := pass.NewManager()
		pass.Register(m, NewBasicBlocksAnalysis())
		pass.Register(m, NewGlobalValueNumberingAnalysis())
		gvn, err := pass.GetResult[*GlobalValueNumbering](m, fn)
		require.NoError(t, err)
		require.Len(t, gvn.Redundant, 1)
		var r Redundancy
		for _, v := range gvn.Redundant {
			r = v
		}
		require.Equal(t, -1, r.Home)
		require.Len(t, gvn.Defs[r.Def], 1, "captured once in the preheader")
	})

	t.Run("value defined on only one arm is not redundant at the merge", func(t *testing.T) {
		fb := types.NewFunctionBuilder(i32t)
		then, merge := fb.Label(), fb.Label()
		fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_GT_S))
		fb.BrIf(then)
		fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.DROP))
		fb.Br(merge)
		fb.Bind(then)
		fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.DROP))
		fb.Bind(merge)
		fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN))
		fn := fb.MustBuild()

		m := pass.NewManager()
		pass.Register(m, NewBasicBlocksAnalysis())
		pass.Register(m, NewGlobalValueNumberingAnalysis())
		gvn, err := pass.GetResult[*GlobalValueNumbering](m, fn)
		require.NoError(t, err)
		require.Empty(t, gvn.Redundant)
	})

	t.Run("reassigned local is opaque across blocks", func(t *testing.T) {
		fb := types.NewFunctionBuilder(i32t).WithLocals(types.TypeI32)
		merge := fb.Label()
		fb.Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.LOCAL_SET, 2))
		fb.Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.DROP))
		fb.Br(merge)
		fb.Bind(merge)
		fb.Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN))
		fn := fb.MustBuild()

		m := pass.NewManager()
		pass.Register(m, NewBasicBlocksAnalysis())
		pass.Register(m, NewGlobalValueNumberingAnalysis())
		gvn, err := pass.GetResult[*GlobalValueNumbering](m, fn)
		require.NoError(t, err)
		require.Empty(t, gvn.Redundant, "slot 2 is reassigned, so its value has no stable cross-block identity")
	})

	t.Run("within-block redundancy reuses a live local home", func(t *testing.T) {
		fn := types.NewFunctionBuilder(i32t).WithLocals(types.TypeI32).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_SET, 2),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
		).MustBuild()

		m := pass.NewManager()
		pass.Register(m, NewBasicBlocksAnalysis())
		pass.Register(m, NewGlobalValueNumberingAnalysis())
		gvn, err := pass.GetResult[*GlobalValueNumbering](m, fn)
		require.NoError(t, err)
		require.Len(t, gvn.Redundant, 1)
		for _, r := range gvn.Redundant {
			require.Equal(t, 2, r.Home, "slot 2 still holds the value")
		}
	})

	t.Run("commutative operands canonicalize", func(t *testing.T) {
		fn := types.NewFunctionBuilder(i32t).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
		).MustBuild()

		m := pass.NewManager()
		pass.Register(m, NewBasicBlocksAnalysis())
		pass.Register(m, NewGlobalValueNumberingAnalysis())
		gvn, err := pass.GetResult[*GlobalValueNumbering](m, fn)
		require.NoError(t, err)
		require.Len(t, gvn.Redundant, 1)
	})

	t.Run("non-commutative operands do not match", func(t *testing.T) {
		fn := types.NewFunctionBuilder(i32t).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_SUB),
			instr.New(instr.RETURN),
		).MustBuild()

		m := pass.NewManager()
		pass.Register(m, NewBasicBlocksAnalysis())
		pass.Register(m, NewGlobalValueNumberingAnalysis())
		gvn, err := pass.GetResult[*GlobalValueNumbering](m, fn)
		require.NoError(t, err)
		require.Empty(t, gvn.Redundant)
	})

	t.Run("a store invalidates the expression", func(t *testing.T) {
		fn := types.NewFunctionBuilder(i32t).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.DROP),
			instr.New(instr.I32_CONST, 7),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
		).MustBuild()

		m := pass.NewManager()
		pass.Register(m, NewBasicBlocksAnalysis())
		pass.Register(m, NewGlobalValueNumberingAnalysis())
		gvn, err := pass.GetResult[*GlobalValueNumbering](m, fn)
		require.NoError(t, err)
		require.Empty(t, gvn.Redundant)
	})

	t.Run("a call ends numbering", func(t *testing.T) {
		fn := types.NewFunctionBuilder(i32t).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.DROP),
			instr.New(instr.CALL),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
		).MustBuild()

		m := pass.NewManager()
		pass.Register(m, NewBasicBlocksAnalysis())
		pass.Register(m, NewGlobalValueNumberingAnalysis())
		gvn, err := pass.GetResult[*GlobalValueNumbering](m, fn)
		require.NoError(t, err)
		require.Empty(t, gvn.Redundant)
	})
}
