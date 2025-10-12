package analysis

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestModulePass_Run(t *testing.T) {
	tests := []struct {
		program *program.Program
		module  *Module
	}{
		{
			program: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.NOP),
					}),
				},
			),
			module: &Module{
				EntryPoint: &Function{
					Function: types.NewFunction(
						types.NewFunctionSignature(),
						instr.New(instr.NOP),
					),
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
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.UNREACHABLE),
					}),
				},
			),
			module: &Module{
				EntryPoint: &Function{
					Function: types.NewFunction(
						types.NewFunctionSignature(),
						instr.New(instr.UNREACHABLE),
					),
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
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.RETURN),
					}),
				},
			),
			module: &Module{
				EntryPoint: &Function{
					Function: types.NewFunction(
						types.NewFunctionSignature(),
						instr.New(instr.RETURN),
					),
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
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.BR, 5),
						instr.New(instr.I32_CONST, 1),
						instr.New(instr.I32_CONST, 2),
					}),
				},
			),
			module: &Module{
				EntryPoint: &Function{
					Function: types.NewFunction(
						types.NewFunctionSignature(),
						instr.New(instr.BR, 5),
						instr.New(instr.I32_CONST, 1),
						instr.New(instr.I32_CONST, 2),
					),
					Blocks: []*BasicBlock{
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
			},
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.BR, 5),
						instr.New(instr.I32_CONST, 1),
						instr.New(instr.I32_CONST, 2),
					}),
				},
			),
			module: &Module{
				EntryPoint: &Function{
					Function: types.NewFunction(
						types.NewFunctionSignature(),
						instr.New(instr.BR, 5),
						instr.New(instr.I32_CONST, 1),
						instr.New(instr.I32_CONST, 2),
					),
					Blocks: []*BasicBlock{
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
			},
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.I32_CONST, 1),
						instr.New(instr.BR_IF, 5),
						instr.New(instr.I32_CONST, 2),
						instr.New(instr.I32_CONST, 3),
					}),
				},
			),
			module: &Module{
				EntryPoint: &Function{
					Function: types.NewFunction(
						types.NewFunctionSignature(),
						instr.New(instr.I32_CONST, 1),
						instr.New(instr.BR_IF, 5),
						instr.New(instr.I32_CONST, 2),
						instr.New(instr.I32_CONST, 3),
					),
					Blocks: []*BasicBlock{
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
			},
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.I32_CONST, 1),
						instr.New(instr.BR_TABLE, 1, 5, 0),
						instr.New(instr.I32_CONST, 2),
						instr.New(instr.I32_CONST, 3),
					}),
				},
			),
			module: &Module{
				EntryPoint: &Function{
					Function: types.NewFunction(
						types.NewFunctionSignature(),
						instr.New(instr.I32_CONST, 1),
						instr.New(instr.BR_TABLE, 1, 5, 0),
						instr.New(instr.I32_CONST, 2),
						instr.New(instr.I32_CONST, 3),
					),
					Blocks: []*BasicBlock{
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
			},
		},
	}

	for _, tt := range tests {
		m := pass.NewManager()
		_ = m.Register(NewModulePass())

		t.Run(tt.program.String(), func(t *testing.T) {
			err := m.Run(tt.program)
			require.NoError(t, err)

			var actual *Module
			err = m.Load(&actual)
			require.NoError(t, err)
			require.Equal(t, tt.module, actual)
		})
	}
}
