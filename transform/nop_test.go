package transform

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/stretchr/testify/require"
)

func TestNOPEliminationPass_Run(t *testing.T) {
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
						instr.New(instr.BR, 5),
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
	}

	for _, tt := range tests {
		m := pass.NewManager()
		_ = m.Register(NewNOPEliminationPass())

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
