// Package arm64 implements the JIT Lowerer for AArch64. Blank-import this
// package to register the backend with jit.
package arm64

import (
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/jit"
	"github.com/siyul-park/minivm/types"
)

// Scratch slot assignments for every Phase A segment. The interp adapter
// fills slots 0..3 before the trampoline call and reads slots 1 and 4 back
// afterwards.
const (
	scratchStack  = 0 // X10 — base of i.stack[0]
	scratchSP     = 1 // X11 — current sp (in/out)
	scratchGlobal = 2 // X12 — base of i.globals[0]
	scratchConst  = 3 // X13 — base of i.constants[0]
	scratchNext   = 4 // X14 — segment's next IP (out)
)

// Lowerer is the AArch64 opcode emitter.
type Lowerer struct{}

var theArch = arm64.New()

// Arch returns the asm.Arch the compiler should use when targeting this
// backend.
func (Lowerer) Arch() asm.Arch { return theArch }

// Prologue is a no-op until whole-function Entry lowering lands.
func (Lowerer) Prologue(_ *jit.Context, _ *types.Function) {}

// Epilogue is a no-op until whole-function Entry lowering lands.
func (Lowerer) Epilogue(_ *jit.Context) {}

// Lower dispatches one opcode. Returns false (leaving Context untouched)
// for opcodes Phase A does not yet implement.
func (l Lowerer) Lower(c *jit.Context, op instr.Opcode) bool {
	switch op {
	case instr.NOP:
		return l.lowerNop(c)
	}
	return false
}

// Exit emits the segment terminator: load nextIP into the scratch slot the
// adapter reads (X14) and return from native.
func (Lowerer) Exit(c *jit.Context, nextIP int) {
	next := c.Scratch[scratchNext]
	vNext := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	if err := c.Assembler.Pin(vNext, next); err != nil {
		// Pin failure here means the scratch reg is mis-blocked; in that
		// case Build will fail with a clear error.
		return
	}
	c.Assembler.Emit(arm64.LDI(vNext, uint64(nextIP))...)
	c.Assembler.Emit(arm64.RET())
}

func (Lowerer) lowerNop(c *jit.Context) bool {
	c.IP += instrWidth(c.Code, c.IP)
	return true
}

// instrWidth returns the encoded width in bytes of the opcode at code[ip].
func instrWidth(code []byte, ip int) int {
	return instr.Instruction(code[ip:]).Width()
}
