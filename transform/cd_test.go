package transform

import (
	"testing"

	"github.com/siyul-park/minivm/types"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/stretchr/testify/require"
)

func TestConstantDeduplicationPassPass_Run(t *testing.T) {
	tests := []struct {
		program  *program.Program
		expected *program.Program
	}{
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 1),
					instr.New(instr.CONST_GET, 2),
				},
				program.WithConstants(types.String("foo"), types.String("bar"), types.String("bar")),
			),
			expected: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.CONST_GET, 0),
						instr.New(instr.CONST_GET, 0),
					}),
				},
				program.WithConstants(types.String("bar")),
			),
		},
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.STRUCT_NEW_DEFAULT, 0),
					instr.New(instr.STRUCT_NEW_DEFAULT, 1),
				},
				program.WithTypes(
					types.NewStructType(types.NewStructField(types.TypeF64)),
					types.NewStructType(types.NewStructField(types.TypeF64)),
				),
			),
			expected: program.New(
				[]instr.Instruction{
					instr.Marshal([]instr.Instruction{
						instr.New(instr.STRUCT_NEW_DEFAULT, 0),
						instr.New(instr.STRUCT_NEW_DEFAULT, 0),
					}),
				},
				program.WithTypes(types.NewStructType(types.NewStructField(types.TypeF64))),
			),
		},
	}

	for _, tt := range tests {
		m := pass.NewManager()
		_ = m.Register(NewConstantDeduplicationPass())

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
