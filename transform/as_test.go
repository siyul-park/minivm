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

func TestNewAlgebraicPass(t *testing.T) {
	require.NotNil(t, NewAlgebraicPass())
}

func TestAlgebraicPass_Run(t *testing.T) {
	tests := []struct {
		program  *program.Program
		expected *program.Program
	}{
		{
			program: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 5),
				instr.New(instr.I32_CONST, 0),
				instr.New(instr.I32_ADD),
			}),
			expected: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 5),
				instr.New(instr.NOP), instr.New(instr.NOP), instr.New(instr.NOP),
				instr.New(instr.NOP), instr.New(instr.NOP), instr.New(instr.NOP),
			}),
		},
		{
			program: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 5),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_MUL),
			}),
			expected: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 5),
				instr.New(instr.NOP), instr.New(instr.NOP), instr.New(instr.NOP),
				instr.New(instr.NOP), instr.New(instr.NOP), instr.New(instr.NOP),
			}),
		},
		{
			program: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 5),
				instr.New(instr.I32_CONST, 0xFFFFFFFF),
				instr.New(instr.I32_AND),
			}),
			expected: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 5),
				instr.New(instr.NOP), instr.New(instr.NOP), instr.New(instr.NOP),
				instr.New(instr.NOP), instr.New(instr.NOP), instr.New(instr.NOP),
			}),
		},
		{
			program: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 5),
				instr.New(instr.I32_CONST, 8),
				instr.New(instr.I32_MUL),
			}),
			expected: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 5),
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.I32_SHL),
			}),
		},
		{
			program: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 16),
				instr.New(instr.I32_CONST, 4),
				instr.New(instr.I32_DIV_U),
			}),
			expected: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 16),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_SHR_U),
			}),
		},
		{
			program: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 16),
				instr.New(instr.I32_CONST, 4),
				instr.New(instr.I32_DIV_S),
			}),
			expected: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 16),
				instr.New(instr.I32_CONST, 4),
				instr.New(instr.I32_DIV_S),
			}),
		},
		{
			program: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 5),
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.I32_MUL),
			}),
			expected: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, 5),
				instr.New(instr.I32_CONST, 3),
				instr.New(instr.I32_MUL),
			}),
		},
		{
			program: program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, 5),
				instr.New(instr.I64_CONST, 0),
				instr.New(instr.I64_ADD),
			}),
			expected: program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, 5),
				instr.New(instr.NOP), instr.New(instr.NOP), instr.New(instr.NOP),
				instr.New(instr.NOP), instr.New(instr.NOP), instr.New(instr.NOP),
				instr.New(instr.NOP), instr.New(instr.NOP), instr.New(instr.NOP),
				instr.New(instr.NOP),
			}),
		},
		{
			program: program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, 5),
				instr.New(instr.I64_CONST, 4),
				instr.New(instr.I64_MUL),
			}),
			expected: program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, 5),
				instr.New(instr.I64_CONST, 2),
				instr.New(instr.I64_SHL),
			}),
		},
	}

	for _, tt := range tests {
		m := pass.NewManager()
		pass.Register[*types.Function, []*analysis.BasicBlock](m, analysis.NewBlocksAnalysis())

		t.Run(tt.program.String(), func(t *testing.T) {
			actual := tt.program
			_, err := NewAlgebraicPass().Run(m, actual)
			require.NoError(t, err)
			require.Equal(t, tt.expected, actual)
		})
	}

	t.Run("preserves execution", func(t *testing.T) {
		prog := program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 5),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.I32_ADD),
		})
		before := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		defer before.Close()
		require.NoError(t, before.Run(context.Background()))
		want, err := before.Pop()
		require.NoError(t, err)

		manager := pass.NewManager()
		pass.Register(manager, analysis.NewBlocksAnalysis())
		_, err = NewAlgebraicPass().Run(manager, prog)
		require.NoError(t, err)
		after := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		defer after.Close()
		require.NoError(t, after.Run(context.Background()))
		got, err := after.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
	})
}
