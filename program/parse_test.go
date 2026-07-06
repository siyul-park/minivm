package program

import (
	"strings"
	"testing"

	"github.com/siyul-park/minivm/internal/textparse"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		prog *Program
	}{
		{
			New(nil),
		},
		{
			New([]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_ADD),
			}),
		},
		{
			New(
				[]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)},
				WithConstants(
					types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
						Emit(instr.New(instr.I32_CONST, 42), instr.New(instr.RETURN)).
						MustBuild(),
				),
			),
		},
		{
			New(
				[]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)},
				WithConstants(
					types.NewFunctionBuilder(&types.FunctionType{
						Params:  []types.Type{types.TypeI32},
						Returns: []types.Type{types.TypeI64},
					}).
						WithLocals(types.TypeI32).
						Emit(instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN)).
						MustBuild(),
				),
			),
		},
		{
			New(
				nil,
				WithTypes(types.NewArrayType(types.TypeI32), types.NewStructType(
					types.NewStructField(types.TypeI32),
					types.NewStructField(types.TypeF64),
				)),
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.prog.String(), func(t *testing.T) {
			parsed, err := Parse(strings.NewReader(tt.prog.String()))
			require.NoError(t, err)
			require.Equal(t, tt.prog.String(), parsed.String())
		})
	}

	t.Run("long code line", func(t *testing.T) {
		parsed, err := Parse(strings.NewReader("i32.const" + strings.Repeat(" ", 70_000) + "1\n"))
		require.NoError(t, err)
		require.Equal(t, New([]instr.Instruction{instr.New(instr.I32_CONST, 1)}).String(), parsed.String())
	})

	t.Run("oversized code line", func(t *testing.T) {
		_, err := Parse(strings.NewReader("i32.const " + strings.Repeat(" ", textparse.MaxLineBytes) + "1\n"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "exceeds maximum allowed size")
	})
}
