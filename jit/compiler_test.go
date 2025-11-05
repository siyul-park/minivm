package jit

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

func TestCompiler_BasicArithmetic(t *testing.T) {
	instrs := []instr.Instruction{
		instr.New(instr.I32_CONST, uint64(int32(10))),
		instr.New(instr.I32_CONST, uint64(int32(5))),
		instr.New(instr.I32_ADD),
	}
	p := program.New(instrs)

	compiler := NewCompiler()
	fn, err := compiler.Compile(p.Code)
	require.NoError(t, err)
	defer fn.Free()

	state := &InterpreterState{
		Stack: make([]types.Boxed, 1024),
		SP:    new(int),
		BP:    0,
	}

	err = fn.Execute(state)
	require.NoError(t, err)
	require.Equal(t, 1, *state.SP)

	result := state.Stack[0].I32()
	require.Equal(t, int32(15), result)
}

func TestCompiler_ComplexExpression(t *testing.T) {
	instrs := []instr.Instruction{
		instr.New(instr.I32_CONST, uint64(int32(10))),
		instr.New(instr.I32_CONST, uint64(int32(5))),
		instr.New(instr.I32_ADD),
		instr.New(instr.I32_CONST, uint64(int32(3))),
		instr.New(instr.I32_MUL),
	}
	p := program.New(instrs)

	compiler := NewCompiler()
	fn, err := compiler.Compile(p.Code)
	require.NoError(t, err)
	defer fn.Free()

	state := &InterpreterState{
		Stack: make([]types.Boxed, 1024),
		SP:    new(int),
		BP:    0,
	}

	err = fn.Execute(state)
	require.NoError(t, err)

	result := state.Stack[0].I32()
	require.Equal(t, int32(45), result)
}

func TestCompiler_WithExistingStack(t *testing.T) {
	instrs := []instr.Instruction{
		instr.New(instr.I32_CONST, uint64(int32(7))),
		instr.New(instr.I32_CONST, uint64(int32(3))),
		instr.New(instr.I32_SUB),
	}
	p := program.New(instrs)

	compiler := NewCompiler()
	fn, err := compiler.Compile(p.Code)
	require.NoError(t, err)
	defer fn.Free()

	state := &InterpreterState{
		Stack: make([]types.Boxed, 1024),
		SP:    new(int),
		BP:    0,
	}

	state.Stack[0] = types.BoxI32(100)
	*state.SP = 1

	err = fn.Execute(state)
	require.NoError(t, err)
	require.Equal(t, 2, *state.SP)
	require.Equal(t, int32(100), state.Stack[0].I32())

	result := state.Stack[1].I32()
	require.Equal(t, int32(4), result)
}

func TestCompiler_Division(t *testing.T) {
	instrs := []instr.Instruction{
		instr.New(instr.I32_CONST, uint64(int32(84))),
		instr.New(instr.I32_CONST, uint64(int32(2))),
		instr.New(instr.I32_DIV_S),
	}
	p := program.New(instrs)

	compiler := NewCompiler()
	fn, err := compiler.Compile(p.Code)
	require.NoError(t, err)
	defer fn.Free()

	state := &InterpreterState{
		Stack: make([]types.Boxed, 1024),
		SP:    new(int),
		BP:    0,
	}

	err = fn.Execute(state)
	require.NoError(t, err)

	result := state.Stack[0].I32()
	require.Equal(t, int32(42), result)
}

func TestCompiler_MultipleExecutions(t *testing.T) {
	instrs := []instr.Instruction{
		instr.New(instr.I32_CONST, uint64(int32(5))),
		instr.New(instr.I32_CONST, uint64(int32(3))),
		instr.New(instr.I32_MUL),
	}
	p := program.New(instrs)

	compiler := NewCompiler()
	fn, err := compiler.Compile(p.Code)
	require.NoError(t, err)
	defer fn.Free()

	for i := 0; i < 3; i++ {
		state := &InterpreterState{
			Stack: make([]types.Boxed, 1024),
			SP:    new(int),
			BP:    0,
		}

		err = fn.Execute(state)
		require.NoError(t, err)

		result := state.Stack[0].I32()
		require.Equal(t, int32(15), result)
	}
}

func TestCompiler_ErrorHandling(t *testing.T) {
	tests := []struct {
		name      string
		code      []byte
		expectErr error
	}{
		{
			name:      "invalid I32_CONST",
			code:      []byte{byte(instr.I32_CONST)},
			expectErr: ErrInvalidBytecode,
		},
		{
			name:      "unsupported opcode",
			code:      []byte{byte(instr.NOP)},
			expectErr: ErrUnsupportedOpcode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiler := NewCompiler()
			_, err := compiler.Compile(tt.code)
			require.Error(t, err)
		})
	}
}
