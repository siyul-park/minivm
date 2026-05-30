package jit

import (
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// Context is the per-block state the Lowerer reads and mutates while
// emitting native code. Callers in jit/ set up Context once per block;
// architectures see it as a read-mostly bag of pointers.
//
// A Lowerer must return false from Lower without mutating any field in
// Context when an opcode is unsupported.
type Context struct {
	Assembler *asm.Assembler
	Stack     []asm.VReg
	Params    []asm.VReg
	Facts     map[int]types.Kind
	Code      []byte
	IP        int
	End       int
	Slots     *Slots
	Layout    Layout
}

// Lowerer is the arch-specific opcode emitter. Prologue/Epilogue are
// explicit roles for whole-function compilation; Lower dispatches a single
// bytecode opcode and reports whether it was handled.
type Lowerer interface {
	Prologue(c *Context, fn *types.Function)
	Epilogue(c *Context)
	Lower(c *Context, op instr.Opcode) bool
}
