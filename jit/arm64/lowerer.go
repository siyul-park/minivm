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

// Boxing masks and tags used by scalar lowering.
const (
	tagI32  = uint64(0x7FF6_0000_0000_0000)
	maskI32 = uint64(0xFFFFFFFF)

	tagI64  = uint64(0x7FF4_0000_0000_0000)
	maskI64 = uint64(0x0001_FFFF_FFFF_FFFF)

	signI64 = uint8(15)
)

var (
	_       jit.Lowerer = Lowerer{}
	theArch             = arm64.New()
)

// Arch returns the asm.Arch the compiler should use when targeting this
// backend.
func (Lowerer) Arch() asm.Arch { return theArch }

// Prologue binds segment live-ins to ABI args and emits no-op moves so the
// allocator treats them as live from entry.
func (Lowerer) Prologue(c *jit.Context, _ *types.Function) {
	bindInputs(c)
	for _, v := range c.Inputs {
		c.Assembler.Emit(arm64.MOV(v, v))
	}
}

// Epilogue is a no-op until whole-function Entry lowering lands.
func (Lowerer) Epilogue(_ *jit.Context) {}

// Lower dispatches one opcode. Returns false (leaving Context untouched)
// for opcodes Phase A does not yet implement.
func (l Lowerer) Lower(c *jit.Context, op instr.Opcode) bool {
	switch op {
	case instr.NOP:
		return l.nop(c)
	case instr.DROP:
		return l.drop(c)
	case instr.DUP:
		return l.dup(c)
	case instr.SWAP:
		return l.swap(c)
	case instr.I32_CONST:
		return l.i32Const(c)
	case instr.I64_CONST:
		return l.i64Const(c)
	case instr.F32_CONST:
		return l.f32Const(c)
	case instr.F64_CONST:
		return l.f64Const(c)
	case instr.CONST_GET:
		return l.constGet(c)
	case instr.GLOBAL_GET:
		return l.globalGet(c)
	case instr.GLOBAL_SET:
		return l.globalSet(c)
	case instr.LOCAL_GET:
		return l.localGet(c)
	case instr.LOCAL_SET:
		return l.localSet(c)
	case instr.I32_ADD:
		return l.i32Add(c)
	case instr.I32_SUB:
		return l.i32Sub(c)
	case instr.I32_MUL:
		return l.i32Mul(c)
	case instr.I32_AND:
		return l.i32And(c)
	case instr.I32_OR:
		return l.i32Or(c)
	case instr.I32_XOR:
		return l.i32Xor(c)
	case instr.I32_EQZ:
		return l.i32Eqz(c)
	case instr.I32_SHL:
		return l.i32Shl(c)
	case instr.I32_SHR_S:
		return l.i32ShrS(c)
	case instr.I32_SHR_U:
		return l.i32ShrU(c)
	case instr.I32_EQ:
		return l.i32Cmp(c, nil, arm64.CondEQ)
	case instr.I32_NE:
		return l.i32Cmp(c, nil, arm64.CondNE)
	case instr.I32_LT_S:
		return l.i32Cmp(c, signExtendI32, arm64.CondLT)
	case instr.I32_LE_S:
		return l.i32Cmp(c, signExtendI32, arm64.CondLE)
	case instr.I32_GT_S:
		return l.i32Cmp(c, signExtendI32, arm64.CondGT)
	case instr.I32_GE_S:
		return l.i32Cmp(c, signExtendI32, arm64.CondGE)
	case instr.I32_LT_U:
		return l.i32Cmp(c, zeroExtendI32, arm64.CondCC)
	case instr.I32_LE_U:
		return l.i32Cmp(c, zeroExtendI32, arm64.CondLS)
	case instr.I32_GT_U:
		return l.i32Cmp(c, zeroExtendI32, arm64.CondHI)
	case instr.I32_GE_U:
		return l.i32Cmp(c, zeroExtendI32, arm64.CondCS)
	case instr.I64_EQ:
		return l.i64Cmp(c, nil, arm64.CondEQ)
	case instr.I64_NE:
		return l.i64Cmp(c, nil, arm64.CondNE)
	case instr.I64_EQZ:
		return l.i64Eqz(c)
	case instr.I64_LT_S:
		return l.i64Cmp(c, signExtendI64, arm64.CondLT)
	case instr.I64_LE_S:
		return l.i64Cmp(c, signExtendI64, arm64.CondLE)
	case instr.I64_GT_S:
		return l.i64Cmp(c, signExtendI64, arm64.CondGT)
	case instr.I64_GE_S:
		return l.i64Cmp(c, signExtendI64, arm64.CondGE)
	case instr.I64_LT_U:
		return l.i64Cmp(c, zeroExtendI64, arm64.CondCC)
	case instr.I64_LE_U:
		return l.i64Cmp(c, zeroExtendI64, arm64.CondLS)
	case instr.I64_GT_U:
		return l.i64Cmp(c, zeroExtendI64, arm64.CondHI)
	case instr.I64_GE_U:
		return l.i64Cmp(c, zeroExtendI64, arm64.CondCS)
	case instr.I64_SHR_S:
		return l.i64ShrS(c)
	case instr.BR:
		return l.br(c)
	case instr.BR_IF:
		return l.brIf(c)
	case instr.SELECT:
		return l.selectOp(c)
	case instr.LOCAL_TEE:
		return l.localTee(c)
	case instr.GLOBAL_TEE:
		return l.globalTee(c)
	case instr.I32_TO_I64_S:
		return l.i32ToI64S(c)
	case instr.I32_TO_I64_U:
		return l.i32ToI64U(c)
	case instr.I64_TO_I32:
		return l.i64ToI32(c)
	}
	return false
}

// Exit emits the segment terminator. Two steps:
//
//  1. Pin the segment-local stack shadow to ABI return registers.
//  2. Load nextIP into scratch slot 3 and RET.
func (Lowerer) Exit(c *jit.Context, nextIP int) {
	rNext := c.Scratch[jit.ScratchNext]

	bindInputs(c)

	c.Returns = c.Returns[:0]
	for i, v := range c.Stack {
		ret := theArch.ABI().Return(i, v.Type(), v.Width())
		_ = c.Assembler.Pin(v, ret)
		c.Returns = append(c.Returns, ret)
	}

	vNext := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = c.Assembler.Pin(vNext, rNext)
	c.Assembler.Emit(arm64.LDI(vNext, uint64(nextIP))...)
	c.Assembler.Emit(arm64.RET())
}

func (Lowerer) nop(c *jit.Context) bool {
	c.IP += instrWidth(c.Code, c.IP)
	return true
}

func (Lowerer) drop(c *jit.Context) bool {
	if !need(c, 1) {
		return false
	}
	c.IP += instrWidth(c.Code, c.IP)
	c.Stack = c.Stack[:len(c.Stack)-1]
	return true
}

func (Lowerer) dup(c *jit.Context) bool {
	if !need(c, 1) {
		return false
	}
	top := c.Stack[len(c.Stack)-1]
	dst := c.Assembler.Reg(top.Type(), top.Width())
	c.Assembler.Emit(arm64.MOV(dst, top))
	c.IP += instrWidth(c.Code, c.IP)
	c.Stack = append(c.Stack, dst)
	return true
}

func (l Lowerer) i32Const(c *jit.Context) bool {
	width := instrWidth(c.Code, c.IP)
	val := int32(binary.LittleEndian.Uint32(c.Code[c.IP+1 : c.IP+width]))
	boxed := uint64(types.BoxI32(val))
	return l.pushImm(c, boxed, width)
}

func (l Lowerer) i64Const(c *jit.Context) bool {
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

func (l Lowerer) f32Const(c *jit.Context) bool {
	width := instrWidth(c.Code, c.IP)
	bits := binary.LittleEndian.Uint32(c.Code[c.IP+1 : c.IP+width])
	boxed := uint64(types.BoxF32(math.Float32frombits(bits)))
	return l.pushImm(c, boxed, width)
}

func (l Lowerer) f64Const(c *jit.Context) bool {
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

func (Lowerer) swap(c *jit.Context) bool {
	if !need(c, 2) {
		return false
	}
	last := len(c.Stack) - 1
	c.Stack[last], c.Stack[last-1] = c.Stack[last-1], c.Stack[last]
	c.IP += instrWidth(c.Code, c.IP)
	return true
}

func (l Lowerer) constGet(c *jit.Context) bool {
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

// globalGet pushes globals[idx] onto the segment stack via a direct
// LDR from the globals base. Rejects when globals[idx] is a ref because
// Phase A does not yet model the runtime retain.
func (Lowerer) globalGet(c *jit.Context) bool {
	width := instrWidth(c.Code, c.IP)
	idx := int(uint16(c.Code[c.IP+1]) | uint16(c.Code[c.IP+2])<<8)
	if idx >= len(c.Snap.Globals) {
		return false
	}
	if c.Snap.Globals[idx].Kind() == types.KindRef {
		return false
	}

	vGlobal := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	if err := c.Assembler.Pin(vGlobal, c.Scratch[jit.ScratchGlobals]); err != nil {
		return false
	}
	dst := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.LDR(dst, vGlobal, int16(idx*8)))
	c.IP += width
	c.Stack = append(c.Stack, dst)
	return true
}

// globalSet pops the segment stack top and stores it to globals[idx].
// The same ref-handling restriction as globalGet applies; in addition,
// SET overwriting a previously held ref would leak it, so a current ref in
// globals[idx] also rejects.
func (Lowerer) globalSet(c *jit.Context) bool {
	width := instrWidth(c.Code, c.IP)
	idx := int(uint16(c.Code[c.IP+1]) | uint16(c.Code[c.IP+2])<<8)
	if idx >= len(c.Snap.Globals) {
		return false
	}
	if c.Snap.Globals[idx].Kind() == types.KindRef {
		return false
	}
	if !need(c, 1) {
		return false
	}

	src := c.Stack[len(c.Stack)-1]

	vGlobal := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	if err := c.Assembler.Pin(vGlobal, c.Scratch[jit.ScratchGlobals]); err != nil {
		return false
	}
	c.Assembler.Emit(arm64.STR(src, vGlobal, int16(idx*8)))

	c.IP += width
	c.Stack = c.Stack[:len(c.Stack)-1]
	return true
}

// localGet pushes stack[bp+idx] (a previously stored local) onto the
// segment stack via LDR. Ref locals reject for the same reason GLOBAL_GET
// rejects ref globals.
func (l Lowerer) localGet(c *jit.Context) bool {
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

// localSet pops the segment stack top into stack[bp+idx].
func (l Lowerer) localSet(c *jit.Context) bool {
	width := instrWidth(c.Code, c.IP)
	idx := int(c.Code[c.IP+1])
	if idx >= len(c.Snap.Locals) {
		return false
	}
	if c.Snap.Locals[idx] == types.KindRef {
		return false
	}
	if !need(c, 1) {
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
	if err := c.Assembler.Pin(vStack, c.Scratch[jit.ScratchStack]); err != nil {
		return asm.VReg{}, false
	}
	vBP := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	if err := c.Assembler.Pin(vBP, c.Scratch[jit.ScratchBP]); err != nil {
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

func (l Lowerer) i32Add(c *jit.Context) bool {
	return l.i32BinOp(c, arm64.ADD)
}

func (l Lowerer) i32Sub(c *jit.Context) bool {
	return l.i32BinOp(c, arm64.SUB)
}

func (l Lowerer) i32Mul(c *jit.Context) bool {
	return l.i32BinOp(c, arm64.MUL)
}

// i32BinOp lowers an i32 binary arithmetic opcode whose result can
// land in any bit pattern (ADD, SUB, MUL). The lowered sequence runs the
// op on the boxed inputs in 64-bit registers, then re-masks and re-tags
// the result so it lands as a fresh boxed i32 on the segment stack.
func (l Lowerer) i32BinOp(c *jit.Context, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	if !need(c, 2) {
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

// i32And and i32Or take the fast path: ANDing or ORing two
// boxed i32 values preserves the tag bits because both operands share
// the same tag pattern (tag&tag == tag, tag|tag == tag). No re-box step
// is required.
func (l Lowerer) i32And(c *jit.Context) bool {
	return l.i32Logic(c, arm64.AND)
}

func (l Lowerer) i32Or(c *jit.Context) bool {
	return l.i32Logic(c, arm64.ORR)
}

func (Lowerer) i32Logic(c *jit.Context, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	if !need(c, 2) {
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

// i32Xor needs an explicit re-tag: XORing two same-tagged inputs
// cancels the tag bits in the upper half, so we OR the tag back in.
func (l Lowerer) i32Xor(c *jit.Context) bool {
	if !need(c, 2) {
		return false
	}
	b := c.Stack[len(c.Stack)-1]
	a := c.Stack[len(c.Stack)-2]

	xord := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.EOR(xord, a, b))

	tag := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.LDI(tag, tagI32)...)
	boxed := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ORR(boxed, xord, tag))

	c.Stack = append(c.Stack[:len(c.Stack)-2], boxed)
	c.IP += instrWidth(c.Code, c.IP)
	return true
}

// i32Eqz pops one boxed i32, compares its low 32 bits to zero, and
// pushes a boxed i32 1 (equal) or 0 (not equal).
func (l Lowerer) i32Eqz(c *jit.Context) bool {
	if !need(c, 1) {
		return false
	}
	a := c.Stack[len(c.Stack)-1]

	lo := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ANDI(lo, a, maskI32))
	c.Assembler.Emit(arm64.CMPI(lo, 0))

	flag := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.CSET(flag, arm64.CondEQ))

	boxed := l.boxI32(c, flag)
	c.Stack[len(c.Stack)-1] = boxed
	c.IP += instrWidth(c.Code, c.IP)
	return true
}

// i32Shl lowers a logical left shift on boxed i32 inputs. The shift
// count is masked to 5 bits before LSL because ARM64 register-shifts
// read more bits than i32 shift semantics allow.
func (l Lowerer) i32Shl(c *jit.Context) bool {
	return l.i32Shift(c, arm64.LSL, zeroExtendI32)
}

// i32ShrS lowers an arithmetic right shift; the value lane must be
// sign-extended so the high bits carry the correct fill.
func (l Lowerer) i32ShrS(c *jit.Context) bool {
	return l.i32Shift(c, arm64.ASR, signExtendI32)
}

// i32ShrU lowers a logical right shift; zero-extending the value
// lane drops any tag bits before the shift.
func (l Lowerer) i32ShrU(c *jit.Context) bool {
	return l.i32Shift(c, arm64.LSR, zeroExtendI32)
}

func (l Lowerer) i32Shift(
	c *jit.Context,
	op func(dst, src1, src2 asm.Reg) asm.Instruction,
	prep func(*jit.Context, asm.VReg) asm.VReg,
) bool {
	if !need(c, 2) {
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

// i32Cmp pops two boxed i32 values, optionally preps each (sign- or
// zero-extending to 64 bits for signed/unsigned compares), runs CMP on
// the prepared operands, and pushes a boxed 0/1 from the chosen
// condition code. prep is nil for EQ/NE because the boxed tag is
// identical across both operands, so a raw 64-bit compare is correct.
func (l Lowerer) i32Cmp(
	c *jit.Context,
	prep func(*jit.Context, asm.VReg) asm.VReg,
	cond uint8,
) bool {
	if !need(c, 2) {
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
	c.Assembler.Emit(arm64.ANDI(out, v, maskI32))
	return out
}

// signExtendI64 sign-extends bit 48 of v's value lane into bits 49..63.
// LSL by 15 pushes bit 48 to bit 63; ASR by 15 then drags the sign back
// down so the full 64-bit register holds the i64 in two's complement.
func signExtendI64(c *jit.Context, v asm.VReg) asm.VReg {
	tmp := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.LSLI(tmp, v, signI64))
	out := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ASRI(out, tmp, signI64))
	return out
}

// zeroExtendI64 masks v down to its 49-bit value lane in a fresh 64-bit
// vreg, dropping the tag bits so the result can feed into shifts or
// unsigned 64-bit compares without contamination.
func zeroExtendI64(c *jit.Context, v asm.VReg) asm.VReg {
	out := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ANDI(out, v, maskI64))
	return out
}

// i64Cmp pops two boxed i64 inputs, optionally preps each (sign- or
// zero-extending to 64 bits for signed/unsigned compares), runs CMP, and
// pushes a boxed 0/1 from the chosen condition. prep is nil for EQ/NE
// because matching tags make a 64-bit compare sufficient.
func (l Lowerer) i64Cmp(
	c *jit.Context,
	prep func(*jit.Context, asm.VReg) asm.VReg,
	cond uint8,
) bool {
	if !need(c, 2) {
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

// i64Eqz pops one boxed i64, masks off the tag, compares the value
// lane to zero, and pushes the boxed 0/1 result (as a boxed i32 per the
// WebAssembly EQZ semantics).
func (l Lowerer) i64Eqz(c *jit.Context) bool {
	if !need(c, 1) {
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

// i64ShrS is safe to lower because arithmetic right shift of a
// boxable i64 stays boxable. Left shift and unsigned right shift can
// produce values that the interpreter heap-promotes, so they reject.
func (l Lowerer) i64ShrS(c *jit.Context) bool {
	if !need(c, 2) {
		return false
	}
	b := c.Stack[len(c.Stack)-1]
	a := c.Stack[len(c.Stack)-2]

	shift := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ANDI(shift, b, 0x3F))

	val := signExtendI64(c, a)
	raw := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ASR(raw, val, shift))

	boxed := l.boxI64(c, raw)
	c.Stack = append(c.Stack[:len(c.Stack)-2], boxed)
	c.IP += instrWidth(c.Code, c.IP)
	return true
}

// boxI64 masks val to the 49-bit value lane and ORs in the i64 tag.
// val may carry sign-extended high bits — the ANDI step drops them.
func (Lowerer) boxI64(c *jit.Context, val asm.VReg) asm.VReg {
	lo := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ANDI(lo, val, maskI64))

	tag := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.LDI(tag, tagI64)...)

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
	c.Assembler.Emit(arm64.ANDI(lo, val, maskI32))

	tag := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.LDI(tag, tagI32)...)

	boxed := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ORR(boxed, lo, tag))
	return boxed
}

// selectOp implements SELECT: pops cond, val2, val1 (bottom-to-top) and
// pushes val1 if cond != 0, else val2. The condition is tested against the
// low 32 bits (the i32 value lane).
func (Lowerer) selectOp(c *jit.Context) bool {
	if !need(c, 3) {
		return false
	}
	cond := c.Stack[len(c.Stack)-1]
	v2 := c.Stack[len(c.Stack)-2]
	v1 := c.Stack[len(c.Stack)-3]

	lo := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ANDI(lo, cond, maskI32))
	c.Assembler.Emit(arm64.CMPI(lo, 0))

	dst := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.CSEL(dst, v1, v2, arm64.CondNE))

	c.Stack = append(c.Stack[:len(c.Stack)-3], dst)
	c.IP += instrWidth(c.Code, c.IP)
	return true
}

// localTee stores the stack top to stack[bp+idx] and leaves it on the stack.
func (l Lowerer) localTee(c *jit.Context) bool {
	width := instrWidth(c.Code, c.IP)
	idx := int(c.Code[c.IP+1])
	if idx >= len(c.Snap.Locals) {
		return false
	}
	if c.Snap.Locals[idx] == types.KindRef {
		return false
	}
	if !need(c, 1) {
		return false
	}

	src := c.Stack[len(c.Stack)-1]
	addr, ok := l.localAddr(c, idx)
	if !ok {
		return false
	}
	c.Assembler.Emit(arm64.STR(src, addr, 0))
	c.IP += width
	return true
}

// globalTee stores the stack top to globals[idx] and leaves it on the stack.
func (Lowerer) globalTee(c *jit.Context) bool {
	width := instrWidth(c.Code, c.IP)
	idx := int(uint16(c.Code[c.IP+1]) | uint16(c.Code[c.IP+2])<<8)
	if idx >= len(c.Snap.Globals) {
		return false
	}
	if c.Snap.Globals[idx].Kind() == types.KindRef {
		return false
	}
	if !need(c, 1) {
		return false
	}

	src := c.Stack[len(c.Stack)-1]
	vGlobal := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	if err := c.Assembler.Pin(vGlobal, c.Scratch[jit.ScratchGlobals]); err != nil {
		return false
	}
	c.Assembler.Emit(arm64.STR(src, vGlobal, int16(idx*8)))
	c.IP += width
	return true
}

// i32ToI64S sign-extends the i32 value lane of a boxed i32 to a full 64-bit
// value, then boxes the result as an i64. All i32 values are within the
// boxable i64 range, so no overflow check is needed.
func (l Lowerer) i32ToI64S(c *jit.Context) bool {
	if !need(c, 1) {
		return false
	}
	a := c.Stack[len(c.Stack)-1]
	// Sign-extend the low 32 bits (i32 value lane) to 64 bits.
	ext := signExtendI32(c, a)
	boxed := l.boxI64(c, ext)
	c.Stack[len(c.Stack)-1] = boxed
	c.IP += instrWidth(c.Code, c.IP)
	return true
}

// i32ToI64U zero-extends the i32 value lane of a boxed i32 to a 64-bit value,
// then boxes the result as an i64.
func (l Lowerer) i32ToI64U(c *jit.Context) bool {
	if !need(c, 1) {
		return false
	}
	a := c.Stack[len(c.Stack)-1]
	// Zero-extend: mask to lower 32 bits (unsigned i32).
	ext := zeroExtendI32(c, a)
	boxed := l.boxI64(c, ext)
	c.Stack[len(c.Stack)-1] = boxed
	c.IP += instrWidth(c.Code, c.IP)
	return true
}

// i64ToI32 extracts the low 32 bits of a boxed i64's value lane and boxes
// the result as a boxed i32.
func (l Lowerer) i64ToI32(c *jit.Context) bool {
	if !need(c, 1) {
		return false
	}
	a := c.Stack[len(c.Stack)-1]
	// Mask to 32-bit value lane from the boxed i64 (49-bit value lane contains i64).
	lo := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ANDI(lo, a, maskI32))
	boxed := l.boxI32(c, lo)
	c.Stack[len(c.Stack)-1] = boxed
	c.IP += instrWidth(c.Code, c.IP)
	return true
}

// br lowers an unconditional branch. No instructions are emitted; Exit writes
// the target IP to scratch.
func (Lowerer) br(c *jit.Context) bool {
	offset := int(int16(binary.LittleEndian.Uint16(c.Code[c.IP+1 : c.IP+3])))
	c.IP += 3 + offset
	c.Successor = c.IP
	c.Stop = true
	return true
}

// brIf lowers a conditional branch. It pops the boxed i32 condition,
// emits a CBNZ that splits into two inline exit paths (false-target and
// taken-target), each writing the appropriate nextIP to scratch and
// RET-ing.
func (l Lowerer) brIf(c *jit.Context) bool {
	if !need(c, 1) {
		return false
	}
	offset := int(int16(binary.LittleEndian.Uint16(c.Code[c.IP+1 : c.IP+3])))
	falseTarget := c.IP + 3
	takenTarget := c.IP + 3 + offset

	cond := c.Stack[len(c.Stack)-1]
	c.Stack = c.Stack[:len(c.Stack)-1]

	// Extract i32 value lane from the boxed condition.
	condI32 := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ANDI(condI32, cond, maskI32))

	// Pin remaining stack and inputs to ABI registers — same for both paths.
	bindInputs(c)
	c.Returns = c.Returns[:0]
	for i, v := range c.Stack {
		ret := theArch.ABI().Return(i, v.Type(), v.Width())
		_ = c.Assembler.Pin(v, ret)
		c.Returns = append(c.Returns, ret)
	}

	rNext := c.Scratch[jit.ScratchNext]
	takenLbl := c.Assembler.Label()
	c.Assembler.Emit(arm64.CBNZLabel(condI32, takenLbl))

	// Fall-through path: condition was zero.
	vNextFalse := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = c.Assembler.Pin(vNextFalse, rNext)
	c.Assembler.Emit(arm64.LDI(vNextFalse, uint64(falseTarget))...)
	c.Assembler.Emit(arm64.RET())

	// Taken path: condition was non-zero.
	c.Assembler.Bind(takenLbl)
	vNextTaken := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = c.Assembler.Pin(vNextTaken, rNext)
	c.Assembler.Emit(arm64.LDI(vNextTaken, uint64(takenTarget))...)
	c.Assembler.Emit(arm64.RET())

	c.IP = falseTarget
	c.Stop = true
	c.Closed = true
	return true
}

// instrWidth returns the encoded width in bytes of the opcode at code[ip].
func instrWidth(code []byte, ip int) int {
	return instr.Instruction(code[ip:]).Width()
}

func need(c *jit.Context, n int) bool {
	missing := n - len(c.Stack)
	if missing <= 0 {
		return true
	}
	if len(c.Inputs)+missing > theArch.ABI().MaxArgs() {
		return false
	}

	inputs := make([]asm.VReg, missing)
	for i := range inputs {
		inputs[i] = c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	}
	c.Inputs = append(inputs, c.Inputs...)
	c.Stack = append(inputs, c.Stack...)
	return true
}

func bindInputs(c *jit.Context) {
	c.Args = c.Args[:0]
	for i, v := range c.Inputs {
		arg := theArch.ABI().Arg(i, v.Type(), v.Width())
		_ = c.Assembler.Pin(v, arg)
		c.Args = append(c.Args, arg)
	}
}
