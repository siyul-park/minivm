package analysis_test

import (
	"testing"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestBlocks(t *testing.T) {
	fn := types.NewFunctionBuilder(nil).Append(instr.MustNew(instr.NOP)).MustBuild()
	got, err := analysis.Blocks(fn)
	require.NoError(t, err)
	require.Equal(t, []*analysis.BasicBlock{{Start: 0, End: 1}}, got)
}

func TestNewCFG(t *testing.T) {
	require.NotNil(t, analysis.NewCFG())
}

func TestCFG_Run(t *testing.T) {
	tests := []struct {
		fn     *types.Function
		blocks []*analysis.BasicBlock
		err    error
	}{
		{
			fn: types.NewFunctionBuilder(nil).Append(
				instr.MustNew(instr.NOP),
			).MustBuild(),
			blocks: []*analysis.BasicBlock{
				{
					Start: 0,
					End:   1,
					Succs: nil,
					Preds: nil,
				},
			},
		},
		{
			fn: types.NewFunctionBuilder(nil).Append(
				instr.MustNew(instr.UNREACHABLE),
			).MustBuild(),
			blocks: []*analysis.BasicBlock{
				{
					Start: 0,
					End:   1,
					Succs: nil,
					Preds: nil,
				},
			},
		},
		{
			fn: types.NewFunctionBuilder(nil).Append(
				instr.MustNew(instr.RETURN),
			).MustBuild(),
			blocks: []*analysis.BasicBlock{
				{
					Start: 0,
					End:   1,
					Succs: nil,
					Preds: nil,
				},
			},
		},
		{
			fn: types.NewFunctionBuilder(nil).Append(
				instr.MustNew(instr.RETURN_CALL),
				instr.MustNew(instr.I32_CONST, 1),
			).MustBuild(),
			blocks: []*analysis.BasicBlock{
				{Start: 0, End: 1},
				{Start: 1, End: 6},
			},
		},
		{
			fn: types.NewFunctionBuilder(nil).Append(
				instr.MustNew(instr.BR, 5),
				instr.MustNew(instr.I32_CONST, 1),
				instr.MustNew(instr.I32_CONST, 2),
			).MustBuild(),
			blocks: []*analysis.BasicBlock{
				{
					Start: 0,
					End:   3,
					Succs: []int{2},
					Preds: nil,
				},
				{
					Start: 3,
					End:   8,
					Succs: []int{2},
					Preds: nil,
				},
				{
					Start: 8,
					End:   13,
					Succs: nil,
					Preds: []int{0, 1},
				},
			},
		},
		{
			fn: types.NewFunctionBuilder(nil).Append(
				instr.MustNew(instr.I32_CONST, 1),
				instr.MustNew(instr.BR_IF, 5),
				instr.MustNew(instr.I32_CONST, 2),
				instr.MustNew(instr.I32_CONST, 3),
			).MustBuild(),
			blocks: []*analysis.BasicBlock{
				{
					Start: 0,
					End:   8,
					Succs: []int{1, 2},
					Preds: nil,
				},
				{
					Start: 8,
					End:   13,
					Succs: []int{2},
					Preds: []int{0},
				},

				{
					Start: 13,
					End:   18,
					Succs: nil,
					Preds: []int{0, 1},
				},
			},
		},
		{
			fn: types.NewFunctionBuilder(nil).Append(
				instr.MustNew(instr.I32_CONST, 1),
				instr.MustNew(instr.BR_TABLE, 1, 5, 0),
				instr.MustNew(instr.I32_CONST, 2),
				instr.MustNew(instr.I32_CONST, 3),
			).MustBuild(),
			blocks: []*analysis.BasicBlock{
				{
					Start: 0,
					End:   11,
					Succs: []int{1, 2},
					Preds: nil,
				},
				{
					Start: 11,
					End:   16,
					Succs: []int{2},
					Preds: []int{0},
				},
				{
					Start: 16,
					End:   21,
					Succs: nil,
					Preds: []int{0, 1},
				},
			},
		},
		{
			fn: types.NewFunctionBuilder(nil).Append(
				instr.MustNew(instr.NOP),
				instr.MustNew(instr.I32_CONST, 1),
				instr.MustNew(instr.BR, uint64(uint16(-9+1<<16))),
			).MustBuild(),
			blocks: []*analysis.BasicBlock{
				{
					Start: 0,
					End:   9,
					Succs: []int{0},
					Preds: []int{0},
				},
			},
		},
		{
			fn: types.NewFunctionBuilder(nil).Append(
				instr.MustNew(instr.NOP),
				instr.MustNew(instr.I32_CONST, 1),
				instr.MustNew(instr.BR_IF, uint64(uint16(-9+1<<16))),
				instr.MustNew(instr.I32_CONST, 2),
			).MustBuild(),
			blocks: []*analysis.BasicBlock{
				{
					Start: 0,
					End:   9,
					Succs: []int{0, 1},
					Preds: []int{0},
				},
				{
					Start: 9,
					End:   14,
					Succs: nil,
					Preds: []int{0},
				},
			},
		},
		{
			fn: types.NewFunctionBuilder(nil).Append(
				instr.MustNew(instr.I32_CONST, 0),
				instr.MustNew(instr.BR_TABLE, 1, uint64(uint16(-11+1<<16)), 0),
				instr.MustNew(instr.I32_CONST, 2),
			).MustBuild(),
			blocks: []*analysis.BasicBlock{
				{
					Start: 0,
					End:   11,
					Succs: []int{0, 1},
					Preds: []int{0},
				},
				{
					Start: 11,
					End:   16,
					Succs: nil,
					Preds: []int{0},
				},
			},
		},
		{
			fn: types.NewFunctionBuilder(nil).Append(
				instr.MustNew(instr.BR, 10),
			).MustBuild(),
			err: analysis.ErrInvalidJump,
		},
		{
			fn: types.NewFunctionBuilder(nil).Append(
				instr.MustNew(instr.BR_TABLE, 1, 0, 10),
			).MustBuild(),
			err: analysis.ErrInvalidJump,
		},
		{
			fn: types.NewFunctionBuilder(nil).Append(
				instr.MustNew(instr.I32_CONST, 1),
				instr.MustNew(instr.BR_TABLE, 2, 5, 5, 5),
				instr.MustNew(instr.I32_CONST, 2),
				instr.MustNew(instr.I32_CONST, 3),
			).MustBuild(),
			blocks: []*analysis.BasicBlock{
				{Start: 0, End: 18, Succs: []int{1}, Preds: nil},
				{Start: 18, End: 23, Succs: nil, Preds: []int{0}},
			},
		},
		{
			fn: types.NewFunctionBuilder(nil).Append(
				instr.MustNew(instr.I32_CONST, 1),
				instr.MustNew(instr.BR_IF, 0),
				instr.MustNew(instr.I32_CONST, 2),
				instr.MustNew(instr.I32_CONST, 3),
			).MustBuild(),
			blocks: []*analysis.BasicBlock{
				{Start: 0, End: 8, Succs: []int{1}, Preds: nil},
				{Start: 8, End: 18, Succs: nil, Preds: []int{0}},
			},
		},
		{
			fn: types.NewFunctionBuilder(nil).Append(
				instr.MustNew(instr.I32_CONST, 1),
				instr.MustNew(instr.BR_IF, 5),
				instr.MustNew(instr.I32_CONST, 2),
			).MustBuild(),
			blocks: []*analysis.BasicBlock{
				{Start: 0, End: 8, Succs: []int{1}, Preds: nil},
				{Start: 8, End: 13, Succs: nil, Preds: []int{0}},
			},
		},
		{
			fn: types.NewFunctionBuilder(nil).Append(
				instr.MustNew(instr.BR, 100),
			).MustBuild(),
			err: analysis.ErrInvalidJump,
		},
	}

	for _, tt := range tests {
		m := pass.NewManager()
		pass.Register[*types.Function, []*analysis.BasicBlock](m, analysis.NewCFG())

		t.Run(tt.fn.String(), func(t *testing.T) {
			actual, err := pass.GetResult[[]*analysis.BasicBlock](m, tt.fn)
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.blocks, actual)
		})
	}
}

func BenchmarkCFG_Run(b *testing.B) {
	b.Run("many_blocks", func(b *testing.B) {
		const n = 10000
		emit := make([]instr.Instruction, 0, n)
		for range n {
			emit = append(emit, instr.MustNew(instr.I32_CONST, 0))
		}
		fn := types.NewFunctionBuilder(nil).Append(emit...).MustBuild()

		m := pass.NewManager()
		blocksAnalysis := analysis.NewCFG()

		var err error
		b.ResetTimer()
		for b.Loop() {
			_, err = blocksAnalysis.Run(m, fn)
			if err != nil {
				break
			}
		}
		require.NoError(b, err)
	})
}
