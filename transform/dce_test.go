package transform

import (
	"context"
	"testing"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestNewDCEPass(t *testing.T) {
	require.NotNil(t, NewDCEPass())
}

func TestDCEPass_Run(t *testing.T) {
	tests := []struct {
		program  *program.Program
		expected *program.Program
	}{
		{
			program: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.NOP),
					}),
				},
			),
			expected: program.New(nil),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.UNREACHABLE),
					}),
				},
			),
			expected: program.New(nil),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.BR, 6),
						instr.New(instr.I32_CONST, 1),
						instr.New(instr.NOP),
						instr.New(instr.I32_CONST, 2),
					}),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.BR, 0),
						instr.New(instr.I32_CONST, 2),
					}),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.BR_IF, 6),
						instr.New(instr.I32_CONST, 1),
						instr.New(instr.NOP),
						instr.New(instr.I32_CONST, 2),
					}),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.BR_IF, 5),
						instr.New(instr.I32_CONST, 1),
						instr.New(instr.I32_CONST, 2),
					}),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.BR_TABLE, 1, 6, 0),
						instr.New(instr.I32_CONST, 1),
						instr.New(instr.NOP),
						instr.New(instr.I32_CONST, 2),
					}),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.BR_TABLE, 1, 5, 0),
						instr.New(instr.I32_CONST, 1),
						instr.New(instr.I32_CONST, 2),
					}),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.NOP),
						instr.New(instr.I32_CONST, 1),
						instr.New(instr.BR, uint64(uint16(-9+1<<16))),
					}),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.I32_CONST, 1),
						instr.New(instr.BR, uint64(uint16(-8+1<<16))),
					}),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.NOP),
						instr.New(instr.I32_CONST, 0),
						instr.New(instr.BR_IF, uint64(uint16(-9+1<<16))),
					}),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.I32_CONST, 0),
						instr.New(instr.BR_IF, uint64(uint16(-8+1<<16))),
					}),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.NOP),
						instr.New(instr.I32_CONST, 0),
						instr.New(instr.BR_TABLE, 1, uint64(uint16(-11+1<<16)), 0),
						instr.New(instr.I32_CONST, 2),
					}),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.I32_CONST, 0),
						instr.New(instr.BR_TABLE, 1, uint64(uint16(-11+1<<16)), 0),
						instr.New(instr.I32_CONST, 2),
					}),
				},
			),
		},
	}

	for _, tt := range tests {
		m := pass.NewManager()
		pass.Register[*types.Function, []*analysis.BasicBlock](m, analysis.NewBlocksAnalysis())

		t.Run(tt.program.String(), func(t *testing.T) {
			actual := tt.program
			_, err := NewDCEPass().Run(m, actual)
			require.NoError(t, err)
			require.Equal(t, tt.expected, actual)
		})
	}

	// A branch to the past-the-end offset is a virtual exit only for top-level
	// code (program.Verify enforces this). A function constant that branches to
	// its own end is malformed, and DCE must reject it rather than silently
	// repairing the offset as if it were a legal virtual exit.
	t.Run("rejects branch to end inside a function", func(t *testing.T) {
		fn := &types.Function{Typ: &types.FunctionType{}, Code: instr.Marshal([]instr.Instruction{instr.New(instr.BR, 0)})}
		prog := program.New(nil, program.WithConstants(fn))

		m := pass.NewManager()
		pass.Register[*types.Function, []*analysis.BasicBlock](m, analysis.NewBlocksAnalysis())

		_, err := NewDCEPass().Run(m, prog)
		require.ErrorIs(t, err, analysis.ErrInvalidJump)
	})

	t.Run("preserves execution", func(t *testing.T) {
		builder := program.NewBuilder()
		live := builder.Label()
		builder.Br(live).Emit(instr.I32_CONST, 99).Bind(live).Emit(instr.I32_CONST, 42)
		prog, err := builder.Build()
		require.NoError(t, err)
		before := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		defer before.Close()
		require.NoError(t, before.Run(context.Background()))
		want, err := before.Pop()
		require.NoError(t, err)

		manager := pass.NewManager()
		pass.Register(manager, analysis.NewBlocksAnalysis())
		_, err = NewDCEPass().Run(manager, prog)
		require.NoError(t, err)
		after := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		defer after.Close()
		require.NoError(t, after.Run(context.Background()))
		got, err := after.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
	})
}
