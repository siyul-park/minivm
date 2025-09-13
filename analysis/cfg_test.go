package analysis

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/stretchr/testify/require"
)

func TestBuildCFG(t *testing.T) {
	tests := []struct {
		code []byte
		cfg  *CFG
	}{
		{
			code: instr.Marshal([]instr.Instruction{
				instr.New(instr.NOP),
			}),
			cfg: &CFG{
				Blocks: []*BasicBlock{
					{
						Start: 0,
						End:   1,
						Succs: nil,
						Preds: nil,
					},
				},
			},
		},
		{
			code: instr.Marshal([]instr.Instruction{
				instr.New(instr.UNREACHABLE),
			}),
			cfg: &CFG{
				Blocks: []*BasicBlock{
					{
						Start: 0,
						End:   1,
						Succs: nil,
						Preds: nil,
					},
				},
			},
		},
		{
			code: instr.Marshal([]instr.Instruction{
				instr.New(instr.RETURN),
			}),
			cfg: &CFG{
				Blocks: []*BasicBlock{
					{
						Start: 0,
						End:   1,
						Succs: nil,
						Preds: nil,
					},
				},
			},
		},
		{
			code: instr.Marshal([]instr.Instruction{
				instr.New(instr.BR, 5),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 2),
			}),
			cfg: &CFG{
				Blocks: []*BasicBlock{
					{
						Start: 0,
						End:   5,
						Succs: []int{2},
						Preds: nil,
					},
					{
						Start: 5,
						End:   10,
						Succs: []int{2},
						Preds: nil,
					},
					{
						Start: 10,
						End:   15,
						Succs: nil,
						Preds: []int{0, 1},
					},
				},
			},
		},
		{
			code: instr.Marshal([]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.BR_IF, 5),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_CONST, 3),
			}),
			cfg: &CFG{
				Blocks: []*BasicBlock{
					{
						Start: 0,
						End:   10,
						Succs: []int{1, 2},
						Preds: nil,
					},
					{
						Start: 10,
						End:   15,
						Succs: []int{2},
						Preds: []int{0},
					},
					{
						Start: 15,
						End:   20,
						Succs: nil,
						Preds: []int{0, 1},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(instr.Disassemble(tt.code), func(t *testing.T) {
			cfg, err := BuildCFG(tt.code)
			require.NoError(t, err)
			require.Equal(t, tt.cfg, cfg)
		})
	}
}
