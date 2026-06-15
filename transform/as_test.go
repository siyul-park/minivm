package transform

import (
	"testing"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestAlgebraicSimplificationPass_Run(t *testing.T) {
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
		pass.Register[*types.Function, []*analysis.BasicBlock](m, analysis.NewBasicBlocksAnalysis())

		t.Run(tt.program.String(), func(t *testing.T) {
			actual := tt.program
			_, err := NewAlgebraicSimplificationPass().Run(m, actual)
			require.NoError(t, err)
			require.Equal(t, tt.expected, actual)
		})
	}
}
