package transform

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestConstantDeduplicationPass_Run(t *testing.T) {
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
		{
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CONST_GET, 1),
				},
				program.WithConstants(
					types.TypedArray[float64]{1},
					types.TypedArray[float64]{1},
				),
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CONST_GET, 1),
				},
				program.WithConstants(
					types.TypedArray[float64]{1},
					types.TypedArray[float64]{1},
				),
			),
		},
	}

	for _, tt := range tests {
		m := pass.NewManager()

		t.Run(tt.program.String(), func(t *testing.T) {
			actual := tt.program
			_, err := NewConstantDeduplicationPass().Run(m, actual)
			require.NoError(t, err)
			require.Equal(t, tt.expected, actual)
		})
	}
}
