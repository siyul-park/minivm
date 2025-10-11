package analysis

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestModuleBuilder_Build(t *testing.T) {
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
				Functions: []*Function{
					{
						Function: types.NewFunction(
							types.NewFunctionSignature(),
							instr.New(instr.NOP),
						),
						Blocks: []*BasicBlock{
							{
								Offset: 0,
								Code: instr.Marshal([]instr.Instruction{
									instr.New(instr.NOP),
								}),
								Succs: nil,
								Preds: nil,
							},
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
				Functions: []*Function{
					{
						Function: types.NewFunction(
							types.NewFunctionSignature(),
							instr.New(instr.UNREACHABLE),
						),
						Blocks: []*BasicBlock{
							{
								Offset: 0,
								Code: instr.Marshal([]instr.Instruction{
									instr.New(instr.UNREACHABLE),
								}),
								Succs: nil,
								Preds: nil,
							},
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
				Functions: []*Function{
					{
						Function: types.NewFunction(
							types.NewFunctionSignature(),
							instr.New(instr.RETURN),
						),
						Blocks: []*BasicBlock{
							{
								Offset: 0,
								Code: instr.Marshal([]instr.Instruction{
									instr.New(instr.RETURN),
								}),
								Succs: nil,
								Preds: nil,
							},
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
				Functions: []*Function{
					{
						Function: types.NewFunction(
							types.NewFunctionSignature(),
							instr.New(instr.BR, 5),
							instr.New(instr.I32_CONST, 1),
							instr.New(instr.I32_CONST, 2),
						),
						Blocks: []*BasicBlock{
							{
								Offset: 0,
								Code: instr.Marshal([]instr.Instruction{
									instr.New(instr.BR, 5),
								}),
								Succs: []int{2},
								Preds: nil,
							},
							{
								Offset: 3,
								Code: instr.Marshal([]instr.Instruction{
									instr.New(instr.I32_CONST, 1),
								}),
								Succs: []int{2},
								Preds: nil,
							},
							{
								Offset: 8,
								Code: instr.Marshal([]instr.Instruction{
									instr.New(instr.I32_CONST, 2),
								}),
								Succs: nil,
								Preds: []int{0, 1},
							},
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
				Functions: []*Function{
					{
						Function: types.NewFunction(
							types.NewFunctionSignature(),
							instr.New(instr.BR, 5),
							instr.New(instr.I32_CONST, 1),
							instr.New(instr.I32_CONST, 2),
						),
						Blocks: []*BasicBlock{
							{
								Offset: 0,
								Code: instr.Marshal([]instr.Instruction{
									instr.New(instr.BR, 5),
								}),
								Succs: []int{2},
								Preds: nil,
							},
							{
								Offset: 3,
								Code: instr.Marshal([]instr.Instruction{
									instr.New(instr.I32_CONST, 1),
								}),
								Succs: []int{2},
								Preds: nil,
							},
							{
								Offset: 8,
								Code: instr.Marshal([]instr.Instruction{
									instr.New(instr.I32_CONST, 2),
								}),
								Succs: nil,
								Preds: []int{0, 1},
							},
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
				Functions: []*Function{
					{
						Function: types.NewFunction(
							types.NewFunctionSignature(),
							instr.New(instr.I32_CONST, 1),
							instr.New(instr.BR_IF, 5),
							instr.New(instr.I32_CONST, 2),
							instr.New(instr.I32_CONST, 3),
						),
						Blocks: []*BasicBlock{
							{
								Offset: 0,
								Code: instr.Marshal([]instr.Instruction{
									instr.New(instr.I32_CONST, 1),
									instr.New(instr.BR_IF, 5),
								}),
								Succs: []int{1, 2},
								Preds: nil,
							},
							{
								Offset: 8,
								Code: instr.Marshal([]instr.Instruction{
									instr.New(instr.I32_CONST, 2),
								}),
								Succs: []int{2},
								Preds: []int{0},
							},

							{
								Offset: 13,
								Code: instr.Marshal([]instr.Instruction{
									instr.New(instr.I32_CONST, 3),
								}),
								Succs: nil,
								Preds: []int{0, 1},
							},
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
