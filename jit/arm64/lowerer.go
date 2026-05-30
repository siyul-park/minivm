// Package arm64 implements the JIT Lowerer for AArch64. Blank-import this
// package to register the backend with jit.
package arm64

import (
	"encoding/binary"
	"math"

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
	case instr.DROP:
		return l.lowerDrop(c)
	case instr.DUP:
		return l.lowerDup(c)
	case instr.I32_CONST:
		return l.lowerI32Const(c)
	case instr.I64_CONST:
		return l.lowerI64Const(c)
	case instr.F32_CONST:
		return l.lowerF32Const(c)
	case instr.F64_CONST:
		return l.lowerF64Const(c)
	}
	return false
}

// Exit emits the segment terminator. Three steps:
//
//  1. Spill the segment-local stack shadow back to i.stack[]. Each VReg
//     in c.Stack is stored at byte offset (sp + i) * 8 relative to the
//     stack base pointer (scratch slot 0).
//  2. Adjust sp (scratch slot 1) by len(c.Stack) so the adapter reads
//     the post-segment sp back.
//  3. Load nextIP into scratch slot 4 and RET.
func (Lowerer) Exit(c *jit.Context, nextIP int) {
	rStack := c.Scratch[scratchStack]
	rSP := c.Scratch[scratchSP]
	rNext := c.Scratch[scratchNext]

	if len(c.Stack) > 0 {
		// vBase = rStack + rSP*8  (byte address of i.stack[sp])
		vShift := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
		vBase := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)

		vSP := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
		_ = c.Assembler.Pin(vSP, rSP)
		vRStack := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
		_ = c.Assembler.Pin(vRStack, rStack)

		c.Assembler.Emit(arm64.LSLI(vShift, vSP, 3))
		c.Assembler.Emit(arm64.ADD(vBase, vRStack, vShift))

		for i, v := range c.Stack {
			c.Assembler.Emit(arm64.STR(v, vBase, int16(i*8)))
		}

		// sp_out = sp_in + len(c.Stack)
		vSPOut := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
		_ = c.Assembler.Pin(vSPOut, rSP)
		c.Assembler.Emit(arm64.ADDI(vSPOut, vSP, uint16(len(c.Stack))))
	}

	vNext := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = c.Assembler.Pin(vNext, rNext)
	c.Assembler.Emit(arm64.LDI(vNext, uint64(nextIP))...)
	c.Assembler.Emit(arm64.RET())
}

func (Lowerer) lowerNop(c *jit.Context) bool {
	c.IP += instrWidth(c.Code, c.IP)
	return true
}

func (Lowerer) lowerDrop(c *jit.Context) bool {
	if len(c.Stack) == 0 {
		return false
	}
	c.IP += instrWidth(c.Code, c.IP)
	c.Stack = c.Stack[:len(c.Stack)-1]
	return true
}

func (Lowerer) lowerDup(c *jit.Context) bool {
	if len(c.Stack) == 0 {
		return false
	}
	top := c.Stack[len(c.Stack)-1]
	dst := c.Assembler.Reg(top.Type(), top.Width())
	c.Assembler.Emit(arm64.MOV(dst, top))
	c.IP += instrWidth(c.Code, c.IP)
	c.Stack = append(c.Stack, dst)
	return true
}

func (l Lowerer) lowerI32Const(c *jit.Context) bool {
	width := instrWidth(c.Code, c.IP)
	val := int32(binary.LittleEndian.Uint32(c.Code[c.IP+1 : c.IP+width]))
	boxed := uint64(types.BoxI32(val))
	return l.pushImm(c, boxed, width)
}

func (l Lowerer) lowerI64Const(c *jit.Context) bool {
	width := instrWidth(c.Code, c.IP)
	val := int64(binary.LittleEndian.Uint64(c.Code[c.IP+1 : c.IP+width]))
	// Skip values that would heap-promote during interp boxing; segment
	// must produce an authentic Boxed without heap allocation.
	if !types.IsBoxable(val) {
		return false
	}
	boxed := uint64(types.BoxI64(val))
	return l.pushImm(c, boxed, width)
}

func (l Lowerer) lowerF32Const(c *jit.Context) bool {
	width := instrWidth(c.Code, c.IP)
	bits := binary.LittleEndian.Uint32(c.Code[c.IP+1 : c.IP+width])
	boxed := uint64(types.BoxF32(math.Float32frombits(bits)))
	return l.pushImm(c, boxed, width)
}

func (l Lowerer) lowerF64Const(c *jit.Context) bool {
	width := instrWidth(c.Code, c.IP)
	bits := binary.LittleEndian.Uint64(c.Code[c.IP+1 : c.IP+width])
	boxed := uint64(types.BoxF64(math.Float64frombits(bits)))
	return l.pushImm(c, boxed, width)
}

// pushImm loads boxed as a 64-bit immediate into a fresh VReg and tracks it
// on the segment-local stack shadow. width is the encoded byte length of
// the source opcode; the IP advances by that many bytes on success.
func (Lowerer) pushImm(c *jit.Context, boxed uint64, width int) bool {
	dst := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.LDI(dst, boxed)...)
	c.IP += width
	c.Stack = append(c.Stack, dst)
	return true
}

// instrWidth returns the encoded width in bytes of the opcode at code[ip].
func instrWidth(code []byte, ip int) int {
	return instr.Instruction(code[ip:]).Width()
}
