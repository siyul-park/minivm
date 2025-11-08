package transform

import (
	"math"
	"testing"

	"github.com/siyul-park/minivm/types"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/stretchr/testify/require"
)

func TestConstantFoldingPass_Run(t *testing.T) {
	tests := []struct {
		program  *program.Program
		expected *program.Program
	}{
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_ADD),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 3),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 5),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_SUB),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 2),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 4),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_MUL),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 12),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 10),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_DIV_S),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 5),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 10),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_DIV_U),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 3),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 10),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_REM_S),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 10),
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_REM_U),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_SHL),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 2),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_SHR_S),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_SHR_U),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_XOR),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 0),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_AND),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_OR),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_EQZ),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 0),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_EQ),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_NE),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_LT_S),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 1),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_LT_U),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_GT_S),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_GT_U),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_LE_S),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_LE_U),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_GE_S),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 3),
					instr.New(instr.I32_CONST, 2),
					instr.New(instr.I32_GE_U),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 42),
					instr.New(instr.I32_TO_F32_S),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I32_CONST, 42),
					instr.New(instr.I32_TO_F32_U),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_ADD),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I64_CONST, 3),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 5),
					instr.New(instr.I64_CONST, 3),
					instr.New(instr.I64_SUB),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I64_CONST, 2),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 10),
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_DIV_S),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I64_CONST, 5),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 10),
					instr.New(instr.I64_CONST, 3),
					instr.New(instr.I64_DIV_U),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I64_CONST, 3),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 10),
					instr.New(instr.I64_CONST, 3),
					instr.New(instr.I64_REM_S),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I64_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 10),
					instr.New(instr.I64_CONST, 3),
					instr.New(instr.I64_REM_U),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I64_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_SHL),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I64_CONST, 2),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_SHR_S),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I64_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_SHR_U),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I64_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_EQZ),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 0),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_EQ),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_NE),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_LT_S),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 1),
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_LT_U),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 3),
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_GT_S),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 3),
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_GT_U),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_LE_S),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_LE_U),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 3),
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_GE_S),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 3),
					instr.New(instr.I64_CONST, 2),
					instr.New(instr.I64_GE_U),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 42),
					instr.New(instr.I64_TO_I32),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 42),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 42),
					instr.New(instr.I64_TO_F32_S),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 42),
					instr.New(instr.I64_TO_F32_U),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 42),
					instr.New(instr.I64_TO_F64_S),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.F64_CONST, math.Float64bits(42)),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.I64_CONST, 42),
					instr.New(instr.I64_TO_F64_U),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.F64_CONST, math.Float64bits(42)),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(1.5))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.5))),
					instr.New(instr.F32_ADD),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(4.0))),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(5.5))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_SUB),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(3.5))),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(4.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(3.0))),
					instr.New(instr.F32_MUL),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(12.0))),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(10.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_DIV),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(5.0))),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
					instr.New(instr.F32_EQ),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
					instr.New(instr.F32_NE),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(1.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_LT),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(3.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_GT),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_LE),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(3.0))),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(2.0))),
					instr.New(instr.F32_GE),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
					instr.New(instr.F32_TO_I32_S),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 42),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
					instr.New(instr.F32_TO_I32_U),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 42),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(1.5)),
					instr.New(instr.F64_CONST, math.Float64bits(2.5)),
					instr.New(instr.F64_ADD),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.F64_CONST, math.Float64bits(4.0)),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(5.5)),
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_SUB),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.F64_CONST, math.Float64bits(3.5)),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(4.0)),
					instr.New(instr.F64_CONST, math.Float64bits(3.0)),
					instr.New(instr.F64_MUL),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.F64_CONST, math.Float64bits(12.0)),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(10.0)),
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_DIV),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.F64_CONST, math.Float64bits(5.0)),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(1.0)),
					instr.New(instr.F64_CONST, math.Float64bits(1.0)),
					instr.New(instr.F64_EQ),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_CONST, math.Float64bits(1.0)),
					instr.New(instr.F64_NE),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(1.0)),
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_LT),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(3.0)),
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_GT),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_LE),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(3.0)),
					instr.New(instr.F64_CONST, math.Float64bits(2.0)),
					instr.New(instr.F64_GE),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(42)),
					instr.New(instr.F64_TO_I32_S),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 42),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(42)),
					instr.New(instr.F64_TO_I32_U),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 42),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(42)),
					instr.New(instr.F64_TO_I64_S),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.I64_CONST, 42),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(42)),
					instr.New(instr.F64_TO_I64_U),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.I64_CONST, 42),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.F64_CONST, math.Float64bits(42)),
					instr.New(instr.F64_TO_F32),
				},
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.F32_CONST, uint64(math.Float32bits(42))),
				},
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.STRING_ENCODE_UTF32),
					instr.New(instr.STRING_NEW_UTF32),
				},
				program.WithConstants(types.String("foo")),
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.CONST_GET, 2),
				},
				program.WithConstants(types.String("foo"), types.I32Array("foo"), types.String("foo")),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CONST_GET, 1),
					instr.New(instr.STRING_CONCAT),
				},
				program.WithConstants(types.String("foo"), types.String("bar")),
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.CONST_GET, 2),
				},
				program.WithConstants(types.String("foo"), types.String("bar"), types.String("foobar")),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.STRING_EQ),
				},
				program.WithConstants(types.String("foo")),
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
				program.WithConstants(types.String("foo")),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.STRING_NE),
				},
				program.WithConstants(types.String("foo")),
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 0),
				},
				program.WithConstants(types.String("foo")),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CONST_GET, 1),
					instr.New(instr.STRING_LT),
				},
				program.WithConstants(types.String("foo"), types.String("bar")),
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 0),
				},
				program.WithConstants(types.String("foo"), types.String("bar")),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CONST_GET, 1),
					instr.New(instr.STRING_GT),
				},
				program.WithConstants(types.String("foo"), types.String("bar")),
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
				program.WithConstants(types.String("foo"), types.String("bar")),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CONST_GET, 1),
					instr.New(instr.STRING_LE),
				},
				program.WithConstants(types.String("foo"), types.String("bar")),
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 0),
				},
				program.WithConstants(types.String("foo"), types.String("bar")),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CONST_GET, 1),
					instr.New(instr.STRING_GE),
				},
				program.WithConstants(types.String("foo"), types.String("bar")),
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.NOP),
					instr.New(instr.I32_CONST, 1),
				},
				program.WithConstants(types.String("foo"), types.String("bar")),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.STRING_ENCODE_UTF32),
				},
				program.WithConstants(types.String("foo")),
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.NOP),
					instr.New(instr.CONST_GET, 1),
				},
				program.WithConstants(types.String("foo"), types.I32Array("foo")),
			),
		},
	}

	for _, tt := range tests {
		m := pass.NewManager()
		_ = m.Register(analysis.NewBasicBlocksPass())
		_ = m.Register(NewConstantFoldingPass())

		t.Run(tt.program.String(), func(t *testing.T) {
			err := m.Run(tt.program)
			require.NoError(t, err)

			var actual *program.Program
			err = m.Load(&actual)
			require.NoError(t, err)
			require.Equal(t, tt.expected, actual)
		})
	}
}
