package optimize

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestOptimizer_Level(t *testing.T) {
	o := NewOptimizer(O0)
	require.Equal(t, O0, o.Level())
}

func TestOptimizer_Register(t *testing.T) {
	o := NewOptimizer(O0)
	err := o.Register(pass.New(func(_ *pass.Manager) (*program.Program, error) {
		return nil, nil
	}))
	require.NoError(t, err)
}

func TestOptimizer_Optimize(t *testing.T) {
	o := NewOptimizer(O1)
	prog := program.New(
		[]instr.Instruction{
			instr.New(instr.I32_CONST, 20),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		},
		program.WithConstants(
			types.NewFunction(
				types.NewFunctionSignature(
					types.WithParams(types.TypeI64),
					types.WithReturns(types.TypeI64),
				),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_LT_S),
				instr.New(instr.BR_IF, 26),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_SUB),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.I32_SUB),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
				instr.New(instr.I32_ADD),
				instr.New(instr.RETURN),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.RETURN),
			),
		),
	)

	prog, err := o.Optimize(prog)
	require.NoError(t, err)
}
