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

// Lowerer is the AArch64 opcode emitter.
type Lowerer struct{}

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

var (
	_       jit.Lowerer = Lowerer{}
	theArch             = arm64.New()
)

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
	case instr.I32_ADD:
		return l.lowerI32Add(c)
	case instr.I32_SUB:
		return l.lowerI32Sub(c)
	case instr.I32_MUL:
		return l.lowerI32Mul(c)
	case instr.I32_AND:
		return l.lowerI32And(c)
	case instr.I32_OR:
		return l.lowerI32Or(c)
	case instr.I32_XOR:
		return l.lowerI32Xor(c)
	case instr.I32_EQZ:
		return l.lowerI32Eqz(c)
	case instr.I32_SHL:
		return l.lowerI32Shl(c)
	case instr.I32_SHR_S:
		return l.lowerI32ShrS(c)
	case instr.I32_SHR_U:
		return l.lowerI32ShrU(c)
	case instr.I32_EQ:
		return l.lowerI32Cmp(c, nil, arm64.CondEQ)
	case instr.I32_NE:
		return l.lowerI32Cmp(c, nil, arm64.CondNE)
	case instr.I32_LT_S:
		return l.lowerI32Cmp(c, signExtendI32, arm64.CondLT)
	case instr.I32_LE_S:
		return l.lowerI32Cmp(c, signExtendI32, arm64.CondLE)
	case instr.I32_GT_S:
		return l.lowerI32Cmp(c, signExtendI32, arm64.CondGT)
	case instr.I32_GE_S:
		return l.lowerI32Cmp(c, signExtendI32, arm64.CondGE)
	case instr.I32_LT_U:
		return l.lowerI32Cmp(c, zeroExtendI32, arm64.CondCC)
	case instr.I32_LE_U:
		return l.lowerI32Cmp(c, zeroExtendI32, arm64.CondLS)
	case instr.I32_GT_U:
		return l.lowerI32Cmp(c, zeroExtendI32, arm64.CondHI)
	case instr.I32_GE_U:
		return l.lowerI32Cmp(c, zeroExtendI32, arm64.CondCS)
	case instr.I64_ADD:
		return l.lowerI64BinOp(c, arm64.ADD)
	case instr.I64_SUB:
		return l.lowerI64BinOp(c, arm64.SUB)
	case instr.I64_MUL:
		return l.lowerI64BinOp(c, arm64.MUL)
	case instr.I64_EQ:
		return l.lowerI64Cmp(c, nil, arm64.CondEQ)
	case instr.I64_NE:
		return l.lowerI64Cmp(c, nil, arm64.CondNE)
	case instr.I64_EQZ:
		return l.lowerI64Eqz(c)
	case instr.I64_LT_S:
		return l.lowerI64Cmp(c, signExtendI64, arm64.CondLT)
	case instr.I64_LE_S:
		return l.lowerI64Cmp(c, signExtendI64, arm64.CondLE)
	case instr.I64_GT_S:
		return l.lowerI64Cmp(c, signExtendI64, arm64.CondGT)
	case instr.I64_GE_S:
		return l.lowerI64Cmp(c, signExtendI64, arm64.CondGE)
	case instr.I64_LT_U:
		return l.lowerI64Cmp(c, zeroExtendI64, arm64.CondCC)
	case instr.I64_LE_U:
		return l.lowerI64Cmp(c, zeroExtendI64, arm64.CondLS)
	case instr.I64_GT_U:
		return l.lowerI64Cmp(c, zeroExtendI64, arm64.CondHI)
	case instr.I64_GE_U:
		return l.lowerI64Cmp(c, zeroExtendI64, arm64.CondCS)
	case instr.I64_SHL:
		return l.lowerI64Shift(c, arm64.LSL, zeroExtendI64)
	case instr.I64_SHR_S:
		return l.lowerI64Shift(c, arm64.ASR, signExtendI64)
	case instr.I64_SHR_U:
		return l.lowerI64Shift(c, arm64.LSR, zeroExtendI64)
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

// boxedI32Tag is the upper 32 bits common to every NaN-boxed i32. ORing
// the raw 32-bit value into a register pre-cleared above bit 31 produces
// a valid Boxed.
const boxedI32Tag = uint64(0x7FF6_0000_0000_0000)

// i32LoMask isolates the 32-bit value lane of a NaN-boxed i32.
const i32LoMask = uint64(0xFFFFFFFF)

// boxedI64Tag covers bits 49..62 of every NaN-boxed i64 — KindI64 (2) in
// bits 49..51 and the 0x7FF exponent fill in bits 52..62.
const boxedI64Tag = uint64(0x7FF4_0000_0000_0000)

// i64ValMask isolates the 49-bit signed value lane of a NaN-boxed i64.
// Inputs to types.IsBoxable must already fit within this range.
const i64ValMask = uint64(0x0001_FFFF_FFFF_FFFF)

// i64SignShift is the left-then-right shift count used to sign-extend
// bit 48 of an i64 value lane into all bits above it.
const i64SignShift = uint8(15)

func (l Lowerer) lowerI32Add(c *jit.Context) bool {
	return l.lowerI32BinOp(c, arm64.ADD)
}

func (l Lowerer) lowerI32Sub(c *jit.Context) bool {
	return l.lowerI32BinOp(c, arm64.SUB)
}

func (l Lowerer) lowerI32Mul(c *jit.Context) bool {
	return l.lowerI32BinOp(c, arm64.MUL)
}

// lowerI32BinOp lowers an i32 binary arithmetic opcode whose result can
// land in any bit pattern (ADD, SUB, MUL). The lowered sequence runs the
// op on the boxed inputs in 64-bit registers, then re-masks and re-tags
// the result so it lands as a fresh boxed i32 on the segment stack.
func (l Lowerer) lowerI32BinOp(c *jit.Context, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	if len(c.Stack) < 2 {
		return false
	}
	b := c.Stack[len(c.Stack)-1]
	a := c.Stack[len(c.Stack)-2]

	raw := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(op(raw, a, b))

	boxed := l.boxI32(c, raw)
	c.Stack = append(c.Stack[:len(c.Stack)-2], boxed)
	c.IP += instrWidth(c.Code, c.IP)
	return true
}

// lowerI32And and lowerI32Or take the fast path: ANDing or ORing two
// boxed i32 values preserves the tag bits because both operands share
// the same tag pattern (tag&tag == tag, tag|tag == tag). No re-box step
// is required.
func (l Lowerer) lowerI32And(c *jit.Context) bool {
	return l.lowerI32Logical(c, arm64.AND)
}

func (l Lowerer) lowerI32Or(c *jit.Context) bool {
	return l.lowerI32Logical(c, arm64.ORR)
}

func (Lowerer) lowerI32Logical(c *jit.Context, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	if len(c.Stack) < 2 {
		return false
	}
	b := c.Stack[len(c.Stack)-1]
	a := c.Stack[len(c.Stack)-2]
	dst := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(op(dst, a, b))
	c.Stack = append(c.Stack[:len(c.Stack)-2], dst)
	c.IP += instrWidth(c.Code, c.IP)
	return true
}

// lowerI32Xor needs an explicit re-tag: XORing two same-tagged inputs
// cancels the tag bits in the upper half, so we OR the tag back in.
func (l Lowerer) lowerI32Xor(c *jit.Context) bool {
	if len(c.Stack) < 2 {
		return false
	}
	b := c.Stack[len(c.Stack)-1]
	a := c.Stack[len(c.Stack)-2]

	xord := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.EOR(xord, a, b))

	tag := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.LDI(tag, boxedI32Tag)...)
	boxed := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ORR(boxed, xord, tag))

	c.Stack = append(c.Stack[:len(c.Stack)-2], boxed)
	c.IP += instrWidth(c.Code, c.IP)
	return true
}

// lowerI32Eqz pops one boxed i32, compares its low 32 bits to zero, and
// pushes a boxed i32 1 (equal) or 0 (not equal).
func (l Lowerer) lowerI32Eqz(c *jit.Context) bool {
	if len(c.Stack) == 0 {
		return false
	}
	a := c.Stack[len(c.Stack)-1]

	lo := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ANDI(lo, a, i32LoMask))
	c.Assembler.Emit(arm64.CMPI(lo, 0))

	flag := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.CSET(flag, arm64.CondEQ))

	boxed := l.boxI32(c, flag)
	c.Stack[len(c.Stack)-1] = boxed
	c.IP += instrWidth(c.Code, c.IP)
	return true
}

// lowerI32Shl lowers a logical left shift on boxed i32 inputs. The shift
// count is masked to 5 bits before LSL because ARM64 register-shifts
// read more bits than i32 shift semantics allow.
func (l Lowerer) lowerI32Shl(c *jit.Context) bool {
	return l.lowerI32Shift(c, arm64.LSL, zeroExtendI32)
}

// lowerI32ShrS lowers an arithmetic right shift; the value lane must be
// sign-extended so the high bits carry the correct fill.
func (l Lowerer) lowerI32ShrS(c *jit.Context) bool {
	return l.lowerI32Shift(c, arm64.ASR, signExtendI32)
}

// lowerI32ShrU lowers a logical right shift; zero-extending the value
// lane drops any tag bits before the shift.
func (l Lowerer) lowerI32ShrU(c *jit.Context) bool {
	return l.lowerI32Shift(c, arm64.LSR, zeroExtendI32)
}

func (l Lowerer) lowerI32Shift(
	c *jit.Context,
	op func(dst, src1, src2 asm.Reg) asm.Instruction,
	prep func(*jit.Context, asm.VReg) asm.VReg,
) bool {
	if len(c.Stack) < 2 {
		return false
	}
	b := c.Stack[len(c.Stack)-1]
	a := c.Stack[len(c.Stack)-2]

	shift := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ANDI(shift, b, 0x1F))

	val := prep(c, a)
	raw := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(op(raw, val, shift))

	boxed := l.boxI32(c, raw)
	c.Stack = append(c.Stack[:len(c.Stack)-2], boxed)
	c.IP += instrWidth(c.Code, c.IP)
	return true
}

// lowerI32Cmp pops two boxed i32 values, optionally preps each (sign- or
// zero-extending to 64 bits for signed/unsigned compares), runs CMP on
// the prepared operands, and pushes a boxed 0/1 from the chosen
// condition code. prep is nil for EQ/NE because the boxed tag is
// identical across both operands, so a raw 64-bit compare is correct.
func (l Lowerer) lowerI32Cmp(
	c *jit.Context,
	prep func(*jit.Context, asm.VReg) asm.VReg,
	cond uint8,
) bool {
	if len(c.Stack) < 2 {
		return false
	}
	b := c.Stack[len(c.Stack)-1]
	a := c.Stack[len(c.Stack)-2]

	if prep != nil {
		a = prep(c, a)
		b = prep(c, b)
	}
	c.Assembler.Emit(arm64.CMP(a, b))

	flag := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.CSET(flag, cond))

	boxed := l.boxI32(c, flag)
	c.Stack = append(c.Stack[:len(c.Stack)-2], boxed)
	c.IP += instrWidth(c.Code, c.IP)
	return true
}

// signExtendI32 sign-extends the low 32 bits of v into a fresh 64-bit
// vreg so signed 64-bit compares and arithmetic produce correct results.
func signExtendI32(c *jit.Context, v asm.VReg) asm.VReg {
	out := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.SXTW(out, v))
	return out
}

// zeroExtendI32 masks v down to its low 32 bits in a fresh 64-bit vreg,
// dropping the tag bits so the result can feed into shifts or unsigned
// 64-bit compares without contamination.
func zeroExtendI32(c *jit.Context, v asm.VReg) asm.VReg {
	out := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ANDI(out, v, i32LoMask))
	return out
}

// signExtendI64 sign-extends bit 48 of v's value lane into bits 49..63.
// LSL by 15 pushes bit 48 to bit 63; ASR by 15 then drags the sign back
// down so the full 64-bit register holds the i64 in two's complement.
func signExtendI64(c *jit.Context, v asm.VReg) asm.VReg {
	tmp := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.LSLI(tmp, v, i64SignShift))
	out := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ASRI(out, tmp, i64SignShift))
	return out
}

// zeroExtendI64 masks v down to its 49-bit value lane in a fresh 64-bit
// vreg, dropping the tag bits so the result can feed into shifts or
// unsigned 64-bit compares without contamination.
func zeroExtendI64(c *jit.Context, v asm.VReg) asm.VReg {
	out := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ANDI(out, v, i64ValMask))
	return out
}

// lowerI64BinOp lowers an i64 binary arithmetic opcode (ADD / SUB / MUL).
// Inputs are sign-extended to 64 bits, the op runs on the extended
// values, and the result is re-masked to the 49-bit value lane and
// re-tagged. Results that overflow the boxable range silently wrap;
// full Boxable-vs-heap promotion lands in a later phase.
func (l Lowerer) lowerI64BinOp(
	c *jit.Context,
	op func(dst, src1, src2 asm.Reg) asm.Instruction,
) bool {
	if len(c.Stack) < 2 {
		return false
	}
	b := c.Stack[len(c.Stack)-1]
	a := c.Stack[len(c.Stack)-2]

	av := signExtendI64(c, a)
	bv := signExtendI64(c, b)
	raw := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(op(raw, av, bv))

	boxed := l.boxI64(c, raw)
	c.Stack = append(c.Stack[:len(c.Stack)-2], boxed)
	c.IP += instrWidth(c.Code, c.IP)
	return true
}

// lowerI64Cmp pops two boxed i64 inputs, optionally preps each (sign- or
// zero-extending to 64 bits for signed/unsigned compares), runs CMP, and
// pushes a boxed 0/1 from the chosen condition. prep is nil for EQ/NE
// because matching tags make a 64-bit compare sufficient.
func (l Lowerer) lowerI64Cmp(
	c *jit.Context,
	prep func(*jit.Context, asm.VReg) asm.VReg,
	cond uint8,
) bool {
	if len(c.Stack) < 2 {
		return false
	}
	b := c.Stack[len(c.Stack)-1]
	a := c.Stack[len(c.Stack)-2]

	if prep != nil {
		a = prep(c, a)
		b = prep(c, b)
	}
	c.Assembler.Emit(arm64.CMP(a, b))

	flag := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.CSET(flag, cond))

	boxed := l.boxI32(c, flag)
	c.Stack = append(c.Stack[:len(c.Stack)-2], boxed)
	c.IP += instrWidth(c.Code, c.IP)
	return true
}

// lowerI64Eqz pops one boxed i64, masks off the tag, compares the value
// lane to zero, and pushes the boxed 0/1 result (as a boxed i32 per the
// WebAssembly EQZ semantics).
func (l Lowerer) lowerI64Eqz(c *jit.Context) bool {
	if len(c.Stack) == 0 {
		return false
	}
	a := c.Stack[len(c.Stack)-1]

	val := zeroExtendI64(c, a)
	c.Assembler.Emit(arm64.CMPI(val, 0))
	flag := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.CSET(flag, arm64.CondEQ))

	boxed := l.boxI32(c, flag)
	c.Stack[len(c.Stack)-1] = boxed
	c.IP += instrWidth(c.Code, c.IP)
	return true
}

// lowerI64Shift lowers SHL / SHR_S / SHR_U on boxed i64 inputs. The
// shift count is masked to 6 bits because ARM64 register-shifts read
// more bits than WebAssembly's i64 shift modulo 64. The value lane is
// prepped per opcode (sign or zero extend) so the shift sees the right
// upper bits.
func (l Lowerer) lowerI64Shift(
	c *jit.Context,
	op func(dst, src1, src2 asm.Reg) asm.Instruction,
	prep func(*jit.Context, asm.VReg) asm.VReg,
) bool {
	if len(c.Stack) < 2 {
		return false
	}
	b := c.Stack[len(c.Stack)-1]
	a := c.Stack[len(c.Stack)-2]

	shift := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ANDI(shift, b, 0x3F))

	val := prep(c, a)
	raw := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(op(raw, val, shift))

	boxed := l.boxI64(c, raw)
	c.Stack = append(c.Stack[:len(c.Stack)-2], boxed)
	c.IP += instrWidth(c.Code, c.IP)
	return true
}

// boxI64 masks val to the 49-bit value lane and ORs in the i64 tag.
// val may carry sign-extended high bits — the ANDI step drops them.
func (Lowerer) boxI64(c *jit.Context, val asm.VReg) asm.VReg {
	lo := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ANDI(lo, val, i64ValMask))

	tag := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.LDI(tag, boxedI64Tag)...)

	boxed := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ORR(boxed, lo, tag))
	return boxed
}

// boxI32 takes a vreg holding a value whose low 32 bits carry the
// integer and whose upper 32 bits are zero (any ARM64 32-bit op or an
// ANDI mask of 0xFFFFFFFF gives this shape), and produces a fresh
// vreg holding the NaN-boxed Boxed.
func (Lowerer) boxI32(c *jit.Context, val asm.VReg) asm.VReg {
	lo := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ANDI(lo, val, i32LoMask))

	tag := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.LDI(tag, boxedI32Tag)...)

	boxed := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ORR(boxed, lo, tag))
	return boxed
}

// instrWidth returns the encoded width in bytes of the opcode at code[ip].
func instrWidth(code []byte, ip int) int {
	return instr.Instruction(code[ip:]).Width()
}
