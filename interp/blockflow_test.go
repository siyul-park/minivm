package interp

import (
	"testing"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestBlockHeights(t *testing.T) {
	t.Run("straight-line function", func(t *testing.T) {
		fn := types.NewFunctionBuilder(nil).Emit(
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.NOP),
		).MustBuild()

		m := pass.NewManager()
		pass.Register[*types.Function, []*analysis.BasicBlock](m, analysis.NewBasicBlocksAnalysis())
		blocks, err := pass.GetResult[[]*analysis.BasicBlock](m, fn)
		require.NoError(t, err)

		heights, ok := blockHeights(fn, blocks, nil, nil)
		require.True(t, ok)
		require.Equal(t, []int{0}, heights)
	})

	t.Run("diamond with agreeing merge heights", func(t *testing.T) {
		b := types.NewFunctionBuilder(nil)
		lElse := b.Label()
		lEnd := b.Label()
		b.Emit(instr.New(instr.I32_CONST, 1))
		b.BrIf(lElse)
		b.Emit(instr.New(instr.I32_CONST, 2))
		b.Br(lEnd)
		b.Bind(lElse)
		b.Emit(instr.New(instr.I32_CONST, 3))
		b.Bind(lEnd)
		b.Emit(instr.New(instr.DROP))
		fn := b.MustBuild()

		m := pass.NewManager()
		pass.Register[*types.Function, []*analysis.BasicBlock](m, analysis.NewBasicBlocksAnalysis())
		blocks, err := pass.GetResult[[]*analysis.BasicBlock](m, fn)
		require.NoError(t, err)
		require.Len(t, blocks, 4)

		heights, ok := blockHeights(fn, blocks, nil, nil)
		require.True(t, ok)
		require.Equal(t, []int{0, 0, 0, 1}, heights)
	})

	t.Run("nested loops with backward branches", func(t *testing.T) {
		b := types.NewFunctionBuilder(nil)
		lOuter := b.Label()
		lInner := b.Label()
		b.Bind(lOuter)
		b.Emit(instr.New(instr.NOP))
		b.Bind(lInner)
		b.Emit(instr.New(instr.I32_CONST, 1))
		b.BrIf(lInner)
		b.Emit(instr.New(instr.I32_CONST, 2))
		b.BrIf(lOuter)
		b.Emit(instr.New(instr.RETURN))
		fn := b.MustBuild()

		m := pass.NewManager()
		pass.Register[*types.Function, []*analysis.BasicBlock](m, analysis.NewBasicBlocksAnalysis())
		blocks, err := pass.GetResult[[]*analysis.BasicBlock](m, fn)
		require.NoError(t, err)

		heights, ok := blockHeights(fn, blocks, nil, nil)
		require.True(t, ok)
		require.Len(t, heights, len(blocks))
		for _, h := range heights {
			require.Equal(t, 0, h)
		}
	})

	t.Run("branch to virtual exit", func(t *testing.T) {
		b := types.NewFunctionBuilder(nil)
		lEnd := b.Label()
		b.Emit(instr.New(instr.I32_CONST, 1))
		b.BrIf(lEnd)
		b.Emit(instr.New(instr.I32_CONST, 2))
		b.Bind(lEnd)
		fn := b.MustBuild()

		m := pass.NewManager()
		pass.Register[*types.Function, []*analysis.BasicBlock](m, analysis.NewBasicBlocksAnalysis())
		blocks, err := pass.GetResult[[]*analysis.BasicBlock](m, fn)
		require.NoError(t, err)
		require.Len(t, blocks, 2)

		heights, ok := blockHeights(fn, blocks, nil, nil)
		require.True(t, ok)
		require.Equal(t, []int{0, 0}, heights)
	})

	t.Run("map.new is indeterminate", func(t *testing.T) {
		fn := types.NewFunctionBuilder(nil).Emit(
			instr.New(instr.MAP_NEW, 0),
		).MustBuild()

		m := pass.NewManager()
		pass.Register[*types.Function, []*analysis.BasicBlock](m, analysis.NewBasicBlocksAnalysis())
		blocks, err := pass.GetResult[[]*analysis.BasicBlock](m, fn)
		require.NoError(t, err)

		_, ok := blockHeights(fn, blocks, nil, nil)
		require.False(t, ok)
	})

	t.Run("closure.new is indeterminate", func(t *testing.T) {
		fn := types.NewFunctionBuilder(nil).Emit(
			instr.New(instr.CLOSURE_NEW),
		).MustBuild()

		m := pass.NewManager()
		pass.Register[*types.Function, []*analysis.BasicBlock](m, analysis.NewBasicBlocksAnalysis())
		blocks, err := pass.GetResult[[]*analysis.BasicBlock](m, fn)
		require.NoError(t, err)

		_, ok := blockHeights(fn, blocks, nil, nil)
		require.False(t, ok)
	})

	t.Run("struct.new is indeterminate", func(t *testing.T) {
		fn := types.NewFunctionBuilder(nil).Emit(
			instr.New(instr.STRUCT_NEW, 0),
		).MustBuild()

		m := pass.NewManager()
		pass.Register[*types.Function, []*analysis.BasicBlock](m, analysis.NewBasicBlocksAnalysis())
		blocks, err := pass.GetResult[[]*analysis.BasicBlock](m, fn)
		require.NoError(t, err)

		_, ok := blockHeights(fn, blocks, nil, nil)
		require.False(t, ok)
	})

	t.Run("call on an unresolvable callee is indeterminate", func(t *testing.T) {
		fn := types.NewFunctionBuilder(nil).Emit(
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.CALL),
		).MustBuild()

		m := pass.NewManager()
		pass.Register[*types.Function, []*analysis.BasicBlock](m, analysis.NewBasicBlocksAnalysis())
		blocks, err := pass.GetResult[[]*analysis.BasicBlock](m, fn)
		require.NoError(t, err)

		_, ok := blockHeights(fn, blocks, nil, nil)
		require.False(t, ok)
	})

	t.Run("call on a const-get function constant is determinate", func(t *testing.T) {
		callee := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
			instr.New(instr.I32_CONST, 42),
			instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{instr.New(instr.NOP)}, program.WithConstants(callee))
		i := New(prog)

		b := types.NewFunctionBuilder(nil)
		lElse := b.Label()
		lEnd := b.Label()
		b.Emit(instr.New(instr.CONST_GET, 0))
		b.Emit(instr.New(instr.CALL))
		b.Emit(instr.New(instr.I32_CONST, 5))
		b.BrIf(lElse)
		b.Br(lEnd)
		b.Bind(lElse)
		b.Bind(lEnd)
		b.Emit(instr.New(instr.DROP))
		fn := b.MustBuild()

		m := pass.NewManager()
		pass.Register[*types.Function, []*analysis.BasicBlock](m, analysis.NewBasicBlocksAnalysis())
		blocks, err := pass.GetResult[[]*analysis.BasicBlock](m, fn)
		require.NoError(t, err)

		heights, ok := blockHeights(fn, blocks, i.constants, i.heap)
		require.True(t, ok)
		require.Equal(t, 0, heights[0])
		require.Equal(t, 1, heights[len(heights)-1])
	})

	t.Run("handlers are rejected", func(t *testing.T) {
		fn := &types.Function{
			Typ:      &types.FunctionType{},
			Code:     instr.Marshal([]instr.Instruction{instr.New(instr.NOP)}),
			Handlers: []instr.Handler{{Start: 0, End: 1, Catch: 1, Depth: 0}},
		}

		m := pass.NewManager()
		pass.Register[*types.Function, []*analysis.BasicBlock](m, analysis.NewBasicBlocksAnalysis())
		blocks, err := pass.GetResult[[]*analysis.BasicBlock](m, fn)
		require.NoError(t, err)

		_, ok := blockHeights(fn, blocks, nil, nil)
		require.False(t, ok)
	})

	t.Run("unreachable block is rejected", func(t *testing.T) {
		fn := types.NewFunctionBuilder(nil).Emit(
			instr.New(instr.RETURN),
			instr.New(instr.I32_CONST, 1),
		).MustBuild()

		m := pass.NewManager()
		pass.Register[*types.Function, []*analysis.BasicBlock](m, analysis.NewBasicBlocksAnalysis())
		blocks, err := pass.GetResult[[]*analysis.BasicBlock](m, fn)
		require.NoError(t, err)
		require.Len(t, blocks, 2)
		require.Empty(t, blocks[1].Preds)

		_, ok := blockHeights(fn, blocks, nil, nil)
		require.False(t, ok)
	})
}
