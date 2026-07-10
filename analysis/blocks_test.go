package analysis

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestBasicBlocksAnalysis_Run(t *testing.T) {
	tests := []struct {
		name   string
		fn     *types.Function
		blocks []*BasicBlock
		err    error
	}{
		{
			fn: types.NewFunctionBuilder(nil).Emit(
				instr.New(instr.NOP),
			).MustBuild(),
			blocks: []*BasicBlock{
				{
					Start: 0,
					End:   1,
					Succs: nil,
					Preds: nil,
				},
			},
		},
		{
			fn: types.NewFunctionBuilder(nil).Emit(
				instr.New(instr.UNREACHABLE),
			).MustBuild(),
			blocks: []*BasicBlock{
				{
					Start: 0,
					End:   1,
					Succs: nil,
					Preds: nil,
				},
			},
		},
		{
			fn: types.NewFunctionBuilder(nil).Emit(
				instr.New(instr.RETURN),
			).MustBuild(),
			blocks: []*BasicBlock{
				{
					Start: 0,
					End:   1,
					Succs: nil,
					Preds: nil,
				},
			},
		},
		{
			fn: types.NewFunctionBuilder(nil).Emit(
				instr.New(instr.BR, 5),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 2),
			).MustBuild(),
			blocks: []*BasicBlock{
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
			fn: types.NewFunctionBuilder(nil).Emit(
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.BR_IF, 5),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 3),
			).MustBuild(),
			blocks: []*BasicBlock{
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
			fn: types.NewFunctionBuilder(nil).Emit(
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.BR_TABLE, 1, 5, 0),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 3),
			).MustBuild(),
			blocks: []*BasicBlock{
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
			fn: types.NewFunctionBuilder(nil).Emit(
				instr.New(instr.NOP),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.BR, uint64(uint16(-9+1<<16))),
			).MustBuild(),
			blocks: []*BasicBlock{
				{
					Start: 0,
					End:   9,
					Succs: []int{0},
					Preds: []int{0},
				},
			},
		},
		{
			fn: types.NewFunctionBuilder(nil).Emit(
				instr.New(instr.NOP),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.BR_IF, uint64(uint16(-9+1<<16))),
				instr.New(instr.I32_CONST, 2),
			).MustBuild(),
			blocks: []*BasicBlock{
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
			fn: types.NewFunctionBuilder(nil).Emit(
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.BR_TABLE, 1, uint64(uint16(-11+1<<16)), 0),
				instr.New(instr.I32_CONST, 2),
			).MustBuild(),
			blocks: []*BasicBlock{
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
			name: "invalid br target",
			fn: types.NewFunctionBuilder(nil).Emit(
				instr.New(instr.BR, 10),
			).MustBuild(),
			err: ErrInvalidJump,
		},
		{
			name: "invalid br_table target",
			fn: types.NewFunctionBuilder(nil).Emit(
				instr.New(instr.BR_TABLE, 1, 0, 10),
			).MustBuild(),
			err: ErrInvalidJump,
		},
		{
			name: "br_table repeated targets are deduplicated",
			fn: types.NewFunctionBuilder(nil).Emit(
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.BR_TABLE, 2, 5, 5, 5),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 3),
			).MustBuild(),
			blocks: []*BasicBlock{
				{Start: 0, End: 18, Succs: []int{1}, Preds: nil},
				{Start: 18, End: 23, Succs: nil, Preds: []int{0}},
			},
		},
		{
			name: "br_if duplicate edge is deduplicated",
			fn: types.NewFunctionBuilder(nil).Emit(
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.BR_IF, 0),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 3),
			).MustBuild(),
			blocks: []*BasicBlock{
				{Start: 0, End: 8, Succs: []int{1}, Preds: nil},
				{Start: 8, End: 18, Succs: nil, Preds: []int{0}},
			},
		},
		{
			name: "branch to virtual exit",
			fn: types.NewFunctionBuilder(nil).Emit(
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.BR_IF, 5),
				instr.New(instr.I32_CONST, 2),
			).MustBuild(),
			blocks: []*BasicBlock{
				{Start: 0, End: 8, Succs: []int{1}, Preds: nil},
				{Start: 8, End: 13, Succs: nil, Preds: []int{0}},
			},
		},
		{
			name: "invalid br target after map lookup",
			fn: types.NewFunctionBuilder(nil).Emit(
				instr.New(instr.BR, 100),
			).MustBuild(),
			err: ErrInvalidJump,
		},
	}

	for _, tt := range tests {
		m := pass.NewManager()
		pass.Register[*types.Function, []*BasicBlock](m, NewBasicBlocksAnalysis())

		name := tt.name
		if name == "" {
			name = tt.fn.String()
		}
		t.Run(name, func(t *testing.T) {
			actual, err := pass.GetResult[[]*BasicBlock](m, tt.fn)
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.blocks, actual)
		})
	}
}

func BenchmarkBasicBlocksAnalysis_Run(b *testing.B) {
	b.Run("many_blocks", func(b *testing.B) {
		const n = 10000
		emit := make([]instr.Instruction, 0, n)
		for range n {
			emit = append(emit, instr.New(instr.I32_CONST, 0))
		}
		fn := types.NewFunctionBuilder(nil).Emit(emit...).MustBuild()

		m := pass.NewManager()
		analysis := NewBasicBlocksAnalysis()

		b.ResetTimer()
		for b.Loop() {
			if _, err := analysis.Run(m, fn); err != nil {
				b.Fatal(err)
			}
		}
	})
}
