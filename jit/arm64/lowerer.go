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
// afterwards. CONST_GET emits the constant inline at compile time, so the
// constants table never needs a base pointer at runtime.
const (
	scratchStack  = 0 // X10 — base of i.stack[0]
	scratchSP     = 1 // X11 — current sp (in/out)
	scratchGlobal = 2 // X12 — base of i.globals[0]
	scratchBP     = 3 // X13 — current frame's bp
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
	case instr.SWAP:
		return l.lowerSwap(c)
	case instr.I32_CONST:
		return l.lowerI32Const(c)
	case instr.I64_CONST:
		return l.lowerI64Const(c)
	case instr.F32_CONST:
		return l.lowerF32Const(c)
	case instr.F64_CONST:
		return l.lowerF64Const(c)
	case instr.CONST_GET:
		return l.lowerConstGet(c)
	case instr.GLOBAL_GET:
		return l.lowerGlobalGet(c)
	case instr.GLOBAL_SET:
		return l.lowerGlobalSet(c)
	case instr.LOCAL_GET:
		return l.lowerLocalGet(c)
	case instr.LOCAL_SET:
		return l.lowerLocalSet(c)
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

func (Lowerer) lowerSwap(c *jit.Context) bool {
	if len(c.Stack) < 2 {
		return false
	}
	last := len(c.Stack) - 1
	c.Stack[last], c.Stack[last-1] = c.Stack[last-1], c.Stack[last]
	c.IP += instrWidth(c.Code, c.IP)
	return true
}

func (l Lowerer) lowerConstGet(c *jit.Context) bool {
	width := instrWidth(c.Code, c.IP)
	idx := int(c.Code[c.IP+1])
	if idx >= len(c.Snap.Constants) {
		return false
	}
	v := c.Snap.Constants[idx]
	if v.Kind() == types.KindRef {
		// Ref constants need retain/release accounting the segment ABI
		// does not yet model.
		return false
	}
	return l.pushImm(c, uint64(v), width)
}

// lowerGlobalGet pushes globals[idx] onto the segment stack via a direct
// LDR from the globals base. Rejects when globals[idx] is a ref because
// Phase A does not yet model the runtime retain.
func (Lowerer) lowerGlobalGet(c *jit.Context) bool {
	width := instrWidth(c.Code, c.IP)
	idx := int(uint16(c.Code[c.IP+1]) | uint16(c.Code[c.IP+2])<<8)
	if idx >= len(c.Snap.Globals) {
		return false
	}
	if c.Snap.Globals[idx].Kind() == types.KindRef {
		return false
	}

	vGlobal := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	if err := c.Assembler.Pin(vGlobal, c.Scratch[scratchGlobal]); err != nil {
		return false
	}
	dst := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.LDR(dst, vGlobal, int16(idx*8)))
	c.IP += width
	c.Stack = append(c.Stack, dst)
	return true
}

// lowerGlobalSet pops the segment stack top and stores it to globals[idx].
// The same ref-handling restriction as lowerGlobalGet applies; in addition,
// SET overwriting a previously held ref would leak it, so a current ref in
// globals[idx] also rejects.
func (Lowerer) lowerGlobalSet(c *jit.Context) bool {
	if len(c.Stack) == 0 {
		return false
	}
	width := instrWidth(c.Code, c.IP)
	idx := int(uint16(c.Code[c.IP+1]) | uint16(c.Code[c.IP+2])<<8)
	if idx >= len(c.Snap.Globals) {
		return false
	}
	if c.Snap.Globals[idx].Kind() == types.KindRef {
		return false
	}

	src := c.Stack[len(c.Stack)-1]

	vGlobal := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	if err := c.Assembler.Pin(vGlobal, c.Scratch[scratchGlobal]); err != nil {
		return false
	}
	c.Assembler.Emit(arm64.STR(src, vGlobal, int16(idx*8)))

	c.IP += width
	c.Stack = c.Stack[:len(c.Stack)-1]
	return true
}

// lowerLocalGet pushes stack[bp+idx] (a previously stored local) onto the
// segment stack via LDR. Ref locals reject for the same reason GLOBAL_GET
// rejects ref globals.
func (l Lowerer) lowerLocalGet(c *jit.Context) bool {
	width := instrWidth(c.Code, c.IP)
	idx := int(c.Code[c.IP+1])
	if idx >= len(c.Snap.Locals) {
		return false
	}
	if c.Snap.Locals[idx] == types.KindRef {
		return false
	}

	addr, ok := l.localAddr(c, idx)
	if !ok {
		return false
	}
	dst := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.LDR(dst, addr, 0))
	c.IP += width
	c.Stack = append(c.Stack, dst)
	return true
}

// lowerLocalSet pops the segment stack top into stack[bp+idx].
func (l Lowerer) lowerLocalSet(c *jit.Context) bool {
	if len(c.Stack) == 0 {
		return false
	}
	width := instrWidth(c.Code, c.IP)
	idx := int(c.Code[c.IP+1])
	if idx >= len(c.Snap.Locals) {
		return false
	}
	if c.Snap.Locals[idx] == types.KindRef {
		return false
	}

	src := c.Stack[len(c.Stack)-1]
	addr, ok := l.localAddr(c, idx)
	if !ok {
		return false
	}
	c.Assembler.Emit(arm64.STR(src, addr, 0))

	c.IP += width
	c.Stack = c.Stack[:len(c.Stack)-1]
	return true
}

// localAddr returns a VReg whose value is the byte address of
// stack[bp+idx]. The arithmetic is: rStack + (rBP + idx) * 8. The
// final +idx*8 displacement is folded into the LDR/STR offset, so this
// helper materializes only rStack + rBP*8 into the VReg.
func (Lowerer) localAddr(c *jit.Context, idx int) (asm.VReg, bool) {
	vStack := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	if err := c.Assembler.Pin(vStack, c.Scratch[scratchStack]); err != nil {
		return asm.VReg{}, false
	}
	vBP := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	if err := c.Assembler.Pin(vBP, c.Scratch[scratchBP]); err != nil {
		return asm.VReg{}, false
	}

	vShift := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	vBase := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.LSLI(vShift, vBP, 3))
	c.Assembler.Emit(arm64.ADD(vBase, vStack, vShift))

	// Caller emits LDR/STR with a #idx*8 immediate displacement off vBase.
	if idx != 0 {
		offset := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
		c.Assembler.Emit(arm64.ADDI(offset, vBase, uint16(idx*8)))
		return offset, true
	}
	return vBase, true
}

// instrWidth returns the encoded width in bytes of the opcode at code[ip].
func instrWidth(code []byte, ip int) int {
	return instr.Instruction(code[ip:]).Width()
}
