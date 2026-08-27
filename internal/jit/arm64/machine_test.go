package arm64_test

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	asmarm64 "github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/arm64"
	"github.com/siyul-park/minivm/types"

	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	machine := arm64.New()
	require.NotNil(t, machine)

	t.Run("lowers a straight-line plan", func(t *testing.T) {
		fn := &types.Function{
			Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Code: instr.Marshal([]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_ADD),
				instr.New(instr.RETURN),
			}),
		}
		input := &jit.Input{Address: 1, Function: fn}
		plans, err := jit.StaticPlan(input)
		require.NoError(t, err)
		require.Len(t, plans, 1)

		assembler := asm.New(asmarm64.New())
		_, ok := machine.Lower(assembler, input, plans[0], false)
		require.True(t, ok)

		code, err := assembler.Build()
		require.NoError(t, err)
		require.NotEmpty(t, code)
	})

	t.Run("rejects an empty plan without emitting", func(t *testing.T) {
		fn := &types.Function{Typ: &types.FunctionType{}}
		assembler := asm.New(asmarm64.New())
		_, ok := machine.Lower(assembler, &jit.Input{Address: 1, Function: fn}, jit.Plan{}, false)
		require.False(t, ok, "an unlowerable plan must report failure rather than emit")
	})
}
