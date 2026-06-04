package analysis

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestBasicBlocksPass_Run(t *testing.T) {
	tests := []struct {
		name   string
		fn     *types.Function
		blocks []*BasicBlock
		err    error
	}{
		{
			fn: types.NewFunctionBuilder(nil).Emit(
				instr.New(instr.NOP),
			).Build(),
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
			).Build(),
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
			).Build(),
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
			).Build(),
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
			).Build(),
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
			).Build(),
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
			).Build(),
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
			).Build(),
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
			).Build(),
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
			).Build(),
			err: ErrInvalidJump,
		},
		{
			name: "invalid br_table target",
			fn: types.NewFunctionBuilder(nil).Emit(
				instr.New(instr.BR_TABLE, 1, 0, 10),
			).Build(),
			err: ErrInvalidJump,
		},
	}

	for _, tt := range tests {
		m := pass.NewManager()
		_ = m.Register(NewBasicBlocksPass())

		name := tt.name
		if name == "" {
			name = tt.fn.String()
		}
		t.Run(name, func(t *testing.T) {
			err := m.Run(tt.fn)
			require.NoError(t, err)

			var actual []*BasicBlock
			err = m.Load(&actual)
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.blocks, actual)
		})
	}
}
