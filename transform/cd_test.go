package transform

import (
	"context"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestNewDedupPass(t *testing.T) {
	require.NotNil(t, NewDedupPass())
}

func TestDedupPass_Run(t *testing.T) {
	tests := []struct {
		name     string
		program  *program.Program
		expected *program.Program
	}{
		{
			name: "comparable constants",
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
			name: "equivalent types",
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
			name: "all type operands",
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.REF_TEST, 0),
					instr.New(instr.REF_CAST, 1),
					instr.New(instr.MAP_NEW_DEFAULT, 2),
				},
				program.WithTypes(types.TypeString, types.TypeError, types.NewMapType(types.TypeI32, types.TypeI32)),
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.REF_TEST, 0),
					instr.New(instr.REF_CAST, 1),
					instr.New(instr.MAP_NEW_DEFAULT, 2),
				},
				program.WithTypes(types.TypeString, types.TypeError, types.NewMapType(types.TypeI32, types.TypeI32)),
			),
		},
		{
			name: "uncomparable constants",
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
		{
			name: "nil constants",
			program: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CONST_GET, 1),
				},
				program.WithConstants([]types.Value{nil, nil}...),
			),
			expected: program.New(
				[]instr.Instruction{
					instr.New(instr.CONST_GET, 0),
					instr.New(instr.CONST_GET, 0),
				},
				program.WithConstants([]types.Value{nil}...),
			),
		},
	}

	for _, tt := range tests {
		m := pass.NewManager()

		t.Run(tt.name, func(t *testing.T) {
			actual := tt.program
			_, err := NewDedupPass().Run(m, actual)
			require.NoError(t, err)
			require.Equal(t, tt.expected, actual)
		})
	}

	t.Run("preserves execution", func(t *testing.T) {
		prog := program.New(
			[]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CONST_GET, 1),
				instr.New(instr.I32_ADD),
			},
			program.WithConstants(types.I32(21), types.I32(21)),
		)
		before := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		defer before.Close()
		require.NoError(t, before.Run(context.Background()))
		want, err := before.Pop()
		require.NoError(t, err)

		_, err = NewDedupPass().Run(pass.NewManager(), prog)
		require.NoError(t, err)
		after := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		defer after.Close()
		require.NoError(t, after.Run(context.Background()))
		got, err := after.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
	})
}
