package jit

import (
	"errors"
	"fmt"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

var (
	ErrInvalidBytecode   = errors.New("jit: invalid bytecode")
	ErrUnsupportedOpcode = errors.New("jit: unsupported opcode")
	ErrCompilationFailed = errors.New("jit: compilation failed")
)

type InterpreterState struct {
	Stack []types.Boxed
	SP    *int
	BP    int
}

type CompiledFunction struct {
	Code       []byte
	NumParams  int
	NumReturns int
	execute    func([]int32) []int32
	mem        *ExecutableMemory
}

type Compiler struct {
	asm *Assembler
}

func NewCompiler() *Compiler {
	return &Compiler{
		asm: NewAssembler(),
	}
}

func (cf *CompiledFunction) Free() error {
	if cf.mem != nil {
		return cf.mem.Free()
	}
	return nil
}

func (c *CompiledFunction) Execute(state *InterpreterState) error {
	if *state.SP < c.NumParams {
		return ErrInvalidBytecode
	}

	params := make([]int32, c.NumParams)
	for i := 0; i < c.NumParams; i++ {
		params[i] = state.Stack[*state.SP-c.NumParams+i].I32()
	}

	*state.SP -= c.NumParams

	results := c.execute(params)

	if *state.SP+len(results) > len(state.Stack) {
		return ErrCompilationFailed
	}

	for _, r := range results {
		state.Stack[*state.SP] = types.BoxI32(r)
		*state.SP++
	}

	return nil
}

func (c *Compiler) Compile(code []byte) (*CompiledFunction, error) {
	c.asm.Reset()

	c.asm.PushReg(RBP)
	c.asm.MovRegToReg32(RBP, RSP)

	stackEffect, err := c.compileInstructions(code)
	if err != nil {
		return nil, err
	}

	c.asm.PopReg(RAX)
	c.asm.PopReg(RBP)
	c.asm.Ret()

	codeBytes := c.asm.Bytes()
	mem, err := AllocateExecutable(len(codeBytes))
	if err != nil {
		return nil, err
	}
	copy(mem.Data, codeBytes)

	fn := &CompiledFunction{
		Code:       codeBytes,
		NumParams:  0,
		NumReturns: stackEffect,
		mem:        mem,
	}

	fn.execute = func(params []int32) []int32 {
		result, err := mem.Execute()
		if err != nil {
			panic(err)
		}
		return []int32{int32(result)}
	}

	return fn, nil
}

func (c *Compiler) compileInstructions(code []byte) (int, error) {
	stackEffect := 0

	for i := 0; i < len(code); {
		op := instr.Opcode(code[i])

		switch op {
		case instr.I32_CONST:
			if i+4 >= len(code) {
				return 0, fmt.Errorf("%w: invalid I32_CONST at position %d", ErrInvalidBytecode, i)
			}
			val := int32(code[i+1]) | int32(code[i+2])<<8 | int32(code[i+3])<<16 | int32(code[i+4])<<24
			c.asm.MovImm32ToReg(RAX, val)
			c.asm.PushReg(RAX)
			stackEffect++
			i += 5

		case instr.I32_ADD:
			c.asm.PopReg(RCX)
			c.asm.PopReg(RAX)
			c.asm.AddRegToReg32(RAX, RCX)
			c.asm.PushReg(RAX)
			stackEffect--
			i++

		case instr.I32_SUB:
			c.asm.PopReg(RCX)
			c.asm.PopReg(RAX)
			c.asm.SubRegFromReg32(RAX, RCX)
			c.asm.PushReg(RAX)
			stackEffect--
			i++

		case instr.I32_MUL:
			c.asm.PopReg(RCX)
			c.asm.PopReg(RAX)
			c.asm.ImulRegReg32(RAX, RCX)
			c.asm.PushReg(RAX)
			stackEffect--
			i++

		case instr.I32_DIV_S:
			c.asm.PopReg(RCX)
			c.asm.PopReg(RAX)
			c.asm.Cdq()
			c.asm.Idiv32(RCX)
			c.asm.PushReg(RAX)
			stackEffect--
			i++

		case instr.RETURN:
			c.asm.PopReg(RAX)
			c.asm.PopReg(RBP)
			c.asm.Ret()
			return stackEffect, nil

		default:
			return 0, fmt.Errorf("%w: %v at position %d", ErrUnsupportedOpcode, op, i)
		}
	}

	return stackEffect, nil
}
