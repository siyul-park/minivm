package analysis

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestBasicBlockPass_Run(t *testing.T) {
	tests := []struct {
		fn     *types.Function
		blocks []*BasicBlock
	}{
		{
			fn: types.NewFunction(
				types.NewFunctionSignature(),
				instr.New(instr.NOP),
			),
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
			fn: types.NewFunction(
				types.NewFunctionSignature(),
				instr.New(instr.UNREACHABLE),
			),
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
			fn: types.NewFunction(
				types.NewFunctionSignature(),
				instr.New(instr.RETURN),
			),
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
			fn: types.NewFunction(
				types.NewFunctionSignature(),
				instr.New(instr.BR, 5),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 2),
			),
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
			fn: types.NewFunction(
				types.NewFunctionSignature(),
				instr.New(instr.BR, 5),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 2),
			),
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
			fn: types.NewFunction(
				types.NewFunctionSignature(),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.BR_IF, 5),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 3),
			),
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
			fn: types.NewFunction(
				types.NewFunctionSignature(),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.BR_TABLE, 1, 5, 0),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 3),
			),
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
	}

	for _, tt := range tests {
		m := pass.NewManager()
		_ = m.Register(NewBasicBlocksPass())

		t.Run(tt.fn.String(), func(t *testing.T) {
			err := m.Run(tt.fn)
			require.NoError(t, err)

			var actual []*BasicBlock
			err = m.Load(&actual)
			require.NoError(t, err)
			require.Equal(t, tt.blocks, actual)
		})
	}
}
