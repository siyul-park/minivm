package analysis

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
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
				nil,
			),
			module: &Module{
				Functions: []*Function{
					{
						Function: types.NewFunction(
							[]instr.Instruction{
								instr.New(instr.NOP),
							},
						),
						CFG: &CFG{
							Blocks: []*Block{
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
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.UNREACHABLE),
					}),
				},
				nil,
			),
			module: &Module{
				Functions: []*Function{
					{
						Function: types.NewFunction(
							[]instr.Instruction{
								instr.New(instr.UNREACHABLE),
							},
						),
						CFG: &CFG{
							Blocks: []*Block{
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
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.RETURN),
					}),
				},
				nil,
			),
			module: &Module{
				Functions: []*Function{
					{
						Function: types.NewFunction(
							[]instr.Instruction{
								instr.New(instr.RETURN),
							},
						),
						CFG: &CFG{
							Blocks: []*Block{
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
				nil,
			),
			module: &Module{
				Functions: []*Function{
					{
						Function: types.NewFunction(
							[]instr.Instruction{
								instr.New(instr.BR, 5),
								instr.New(instr.I32_CONST, 1),
								instr.New(instr.I32_CONST, 2),
							},
						),
						CFG: &CFG{
							Blocks: []*Block{
								{
									Offset: 0,
									Code: instr.Marshal([]instr.Instruction{
										instr.New(instr.BR, 5),
									}),
									Succs: []int{2},
									Preds: nil,
								},
								{
									Offset: 5,
									Code: instr.Marshal([]instr.Instruction{
										instr.New(instr.I32_CONST, 1),
									}),
									Succs: []int{2},
									Preds: nil,
								},

								{
									Offset: 10,
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
				nil,
			),
			module: &Module{
				Functions: []*Function{
					{
						Function: types.NewFunction(
							[]instr.Instruction{
								instr.New(instr.BR, 5),
								instr.New(instr.I32_CONST, 1),
								instr.New(instr.I32_CONST, 2),
							},
						),
						CFG: &CFG{
							Blocks: []*Block{
								{
									Offset: 0,
									Code: instr.Marshal([]instr.Instruction{
										instr.New(instr.BR, 5),
									}),
									Succs: []int{2},
									Preds: nil,
								},
								{
									Offset: 5,
									Code: instr.Marshal([]instr.Instruction{
										instr.New(instr.I32_CONST, 1),
									}),
									Succs: []int{2},
									Preds: nil,
								},

								{
									Offset: 10,
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
				nil,
			),
			module: &Module{
				Functions: []*Function{
					{
						Function: types.NewFunction(
							[]instr.Instruction{
								instr.New(instr.I32_CONST, 1),
								instr.New(instr.BR_IF, 5),
								instr.New(instr.I32_CONST, 2),
								instr.New(instr.I32_CONST, 3),
							},
						),
						CFG: &CFG{
							Blocks: []*Block{
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
									Offset: 10,
									Code: instr.Marshal([]instr.Instruction{
										instr.New(instr.I32_CONST, 2),
									}),
									Succs: []int{2},
									Preds: []int{0},
								},

								{
									Offset: 15,
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
		},
	}

	for _, tt := range tests {
		t.Run(tt.program.String(), func(t *testing.T) {
			b := NewModuleBuilder(tt.program)

			m, err := b.Build()
			require.NoError(t, err)
			require.Equal(t, tt.module, m)
		})
	}
}
