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

type call struct {
	params  int
	returns int
	offset  int
	slot    uintptr
	self    bool
}

// Boxing masks and tags used by scalar lowering.
const (
	tagI32  = uint64(0x7FF6_0000_0000_0000)
	maskI32 = uint64(0xFFFFFFFF)

	tagI64  = uint64(0x7FF4_0000_0000_0000)
	maskI64 = uint64(0x0001_FFFF_FFFF_FFFF)

	tagF32 = uint64(0x7FF2_0000_0000_0000)

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
func (l Lowerer) Prologue(c *jit.Context, _ *types.Function) {
	l.bind(c)
	for _, v := range c.Inputs {
		c.Assembler.Emit(arm64.MOV(v, v))
	}
}

// Epilogue is a no-op until whole-function Entry lowering lands.
func (Lowerer) Epilogue(_ *jit.Context) {}

// Lower dispatches one opcode. Returns false (leaving Context untouched)
// for opcodes Phase A does not yet implement.
func (l Lowerer) Lower(c *jit.Context, op instr.Opcode) bool {
	// Reset Target before every opcode EXCEPT CALL so that a CONST_GET
	// of a Ref constant can set Target and have it visible to the
	// immediately-following CALL dispatch. Any other opcode between a Ref
	// CONST_GET and a CALL will clear the stale value.
	if op != instr.CALL {
		c.Target = -1
	}
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
		return l.i32Cmp(c, sign32, arm64.CondLT)
	case instr.I32_LE_S:
		return l.i32Cmp(c, sign32, arm64.CondLE)
	case instr.I32_GT_S:
		return l.i32Cmp(c, sign32, arm64.CondGT)
	case instr.I32_GE_S:
		return l.i32Cmp(c, sign32, arm64.CondGE)
	case instr.I32_LT_U:
		return l.i32Cmp(c, zero32, arm64.CondCC)
	case instr.I32_LE_U:
		return l.i32Cmp(c, zero32, arm64.CondLS)
	case instr.I32_GT_U:
		return l.i32Cmp(c, zero32, arm64.CondHI)
	case instr.I32_GE_U:
		return l.i32Cmp(c, zero32, arm64.CondCS)
	case instr.I64_EQ:
		return l.i64Cmp(c, nil, arm64.CondEQ)
	case instr.I64_NE:
		return l.i64Cmp(c, nil, arm64.CondNE)
	case instr.I64_EQZ:
		return l.i64Eqz(c)
	case instr.I64_LT_S:
		return l.i64Cmp(c, sign64, arm64.CondLT)
	case instr.I64_LE_S:
		return l.i64Cmp(c, sign64, arm64.CondLE)
	case instr.I64_GT_S:
		return l.i64Cmp(c, sign64, arm64.CondGT)
	case instr.I64_GE_S:
		return l.i64Cmp(c, sign64, arm64.CondGE)
	case instr.I64_LT_U:
		return l.i64Cmp(c, zero64, arm64.CondCC)
	case instr.I64_LE_U:
		return l.i64Cmp(c, zero64, arm64.CondLS)
	case instr.I64_GT_U:
		return l.i64Cmp(c, zero64, arm64.CondHI)
	case instr.I64_GE_U:
		return l.i64Cmp(c, zero64, arm64.CondCS)
	case instr.I64_SHR_S:
		return l.i64ShrS(c)
	case instr.BR:
		return l.br(c)
	case instr.BR_IF:
		return l.brIf(c)
	case instr.BR_TABLE:
		return l.brTable(c)
	case instr.SELECT:
		return l.choose(c)
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
	case instr.F32_ADD:
		return l.f32Binary(c, arm64.FADD)
	case instr.F32_SUB:
		return l.f32Binary(c, arm64.FSUB)
	case instr.F32_MUL:
		return l.f32Binary(c, arm64.FMUL)
	case instr.F32_DIV:
		return l.f32Binary(c, arm64.FDIV)
	case instr.F32_EQ:
		return l.f32Cmp(c, arm64.CondEQ)
	case instr.F32_NE:
		return l.f32Cmp(c, arm64.CondNE)
	case instr.F32_LT:
		return l.f32Cmp(c, arm64.CondMI)
	case instr.F32_GT:
		return l.f32Cmp(c, arm64.CondGT)
	case instr.F32_LE:
		return l.f32Cmp(c, arm64.CondLS)
	case instr.F32_GE:
		return l.f32Cmp(c, arm64.CondGE)
	case instr.F64_ADD:
		return l.f64Binary(c, arm64.FADD)
	case instr.F64_SUB:
		return l.f64Binary(c, arm64.FSUB)
	case instr.F64_MUL:
		return l.f64Binary(c, arm64.FMUL)
	case instr.F64_DIV:
		return l.f64Binary(c, arm64.FDIV)
	case instr.F64_EQ:
		return l.f64Cmp(c, arm64.CondEQ)
	case instr.F64_NE:
		return l.f64Cmp(c, arm64.CondNE)
	case instr.F64_LT:
		return l.f64Cmp(c, arm64.CondMI)
	case instr.F64_GT:
		return l.f64Cmp(c, arm64.CondGT)
	case instr.F64_LE:
		return l.f64Cmp(c, arm64.CondLS)
	case instr.F64_GE:
		return l.f64Cmp(c, arm64.CondGE)
	case instr.I32_TO_F32_S:
		return l.toFloat(c, asm.Width32, arm64.SCVTF, sign32)
	case instr.I32_TO_F32_U:
		return l.toFloat(c, asm.Width32, arm64.UCVTF, zero32)
	case instr.I64_TO_F32_S:
		return l.toFloat(c, asm.Width32, arm64.SCVTF, sign64)
	case instr.I64_TO_F32_U:
		return l.toFloat(c, asm.Width32, arm64.UCVTF, zero64)
	case instr.I32_TO_F64_S:
		return l.toFloat(c, asm.Width64, arm64.SCVTF, sign32)
	case instr.I32_TO_F64_U:
		return l.toFloat(c, asm.Width64, arm64.UCVTF, zero32)
	case instr.I64_TO_F64_S:
		return l.toFloat(c, asm.Width64, arm64.SCVTF, sign64)
	case instr.I64_TO_F64_U:
		return l.toFloat(c, asm.Width64, arm64.UCVTF, zero64)
	case instr.F32_TO_F64:
		return l.f32ToF64(c)
	case instr.F64_TO_F32:
		return l.f64ToF32(c)
	case instr.RETURN:
		return l.ret(c)
	case instr.CALL:
		return l.call(c)
	case instr.REF_NULL:
		return l.refNull(c)
	case instr.REF_IS_NULL:
		return l.refIsNull(c)
	case instr.REF_EQ:
		return l.refEq(c)
	}
	return false
}

// Exit emits the segment terminator. Two steps:
//
//  1. Pin the segment-local stack shadow to ABI return registers.
//  2. Load nextIP into scratch slot 3 and RET.
func (l Lowerer) Exit(c *jit.Context, nextIP int) {
	rNext := c.Scratch[jit.ScratchNext]

	l.bind(c)

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

func pushLR(c *jit.Context) {
	vSP := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = c.Assembler.Pin(vSP, arm64.XZR)
	vLR := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = c.Assembler.Pin(vLR, arm64.X30)
	c.Assembler.Emit(arm64.SUBI(vSP, vSP, 16))
	c.Assembler.Emit(arm64.STR(vLR, vSP, 0))
}

func popLR(c *jit.Context) {
	vSP := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = c.Assembler.Pin(vSP, arm64.XZR)
	vLR := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = c.Assembler.Pin(vLR, arm64.X30)
	c.Assembler.Emit(arm64.LDR(vLR, vSP, 0))
	c.Assembler.Emit(arm64.ADDI(vSP, vSP, 16))
}

func (Lowerer) nop(c *jit.Context) bool {
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	return true
}

func (l Lowerer) drop(c *jit.Context) bool {
	if !l.need(c, 1) {
		return false
	}
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	c.Stack = c.Stack[:len(c.Stack)-1]
	return true
}

func (l Lowerer) dup(c *jit.Context) bool {
	if !l.need(c, 1) {
		return false
	}
	top := c.Stack[len(c.Stack)-1]
	dst := c.Assembler.Reg(top.Type(), top.Width())
	c.Assembler.Emit(arm64.MOV(dst, top))
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	c.Stack = append(c.Stack, dst)
	return true
}

func (l Lowerer) i32Const(c *jit.Context) bool {
	width := instr.Instruction(c.Code[c.IP:]).Width()
	val := int32(binary.LittleEndian.Uint32(c.Code[c.IP+1 : c.IP+width]))
	boxed := uint64(types.BoxI32(val))
	return l.imm(c, boxed, width)
}

func (l Lowerer) i64Const(c *jit.Context) bool {
	width := instr.Instruction(c.Code[c.IP:]).Width()
	val := int64(binary.LittleEndian.Uint64(c.Code[c.IP+1 : c.IP+width]))
	// Skip values that would heap-promote during interp boxing; segment
	// must produce an authentic Boxed without heap allocation.
	if !types.IsBoxable(val) {
		return false
	}
	boxed := uint64(types.BoxI64(val))
	return l.imm(c, boxed, width)
}

func (l Lowerer) f32Const(c *jit.Context) bool {
	width := instr.Instruction(c.Code[c.IP:]).Width()
	bits := binary.LittleEndian.Uint32(c.Code[c.IP+1 : c.IP+width])
	boxed := uint64(types.BoxF32(math.Float32frombits(bits)))
	return l.imm(c, boxed, width)
}

func (l Lowerer) f64Const(c *jit.Context) bool {
	width := instr.Instruction(c.Code[c.IP:]).Width()
	bits := binary.LittleEndian.Uint64(c.Code[c.IP+1 : c.IP+width])
	boxed := uint64(types.BoxF64(math.Float64frombits(bits)))
	return l.imm(c, boxed, width)
}

// imm loads boxed as a 64-bit immediate into a fresh VReg and tracks it
// on the segment-local stack shadow. width is the encoded byte length of
// the source opcode; the IP advances by that many bytes on success.
func (Lowerer) imm(c *jit.Context, boxed uint64, width int) bool {
	dst := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.LDI(dst, boxed)...)
	c.IP += width
	c.Stack = append(c.Stack, dst)
	return true
}

func (l Lowerer) swap(c *jit.Context) bool {
	if !l.need(c, 2) {
		return false
	}
	last := len(c.Stack) - 1
	c.Stack[last], c.Stack[last-1] = c.Stack[last-1], c.Stack[last]
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	return true
}

func (l Lowerer) constGet(c *jit.Context) bool {
	width := instr.Instruction(c.Code[c.IP:]).Width()
	idx := int(uint16(c.Code[c.IP+1]) | uint16(c.Code[c.IP+2])<<8)
	if idx >= len(c.Snap.Constants) {
		return false
	}
	v := c.Snap.Constants[idx]
	if v.Kind() == types.KindRef {
		addr := v.Ref()
		next := c.IP + width
		if next >= c.End || instr.Opcode(c.Code[next]) != instr.CALL {
			return false
		}
		if _, ok := l.target(c, addr, len(c.Stack)+1); !ok {
			return false
		}
		c.Target = addr
		return l.imm(c, uint64(v), width)
	}
	return l.imm(c, uint64(v), width)
}

// globalGet pushes globals[idx] onto the segment stack via a direct
// LDR from the globals base. Rejects when globals[idx] is a ref because
// Phase A does not yet model the runtime retain.
func (Lowerer) globalGet(c *jit.Context) bool {
	idx, width, ok := global(c)
	if !ok {
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
func (l Lowerer) globalSet(c *jit.Context) bool {
	idx, width, ok := global(c)
	if !ok {
		return false
	}
	if !l.need(c, 1) {
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
	width := instr.Instruction(c.Code[c.IP:]).Width()
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
	width := instr.Instruction(c.Code[c.IP:]).Width()
	idx := int(c.Code[c.IP+1])
	if idx >= len(c.Snap.Locals) {
		return false
	}
	if c.Snap.Locals[idx] == types.KindRef {
		return false
	}
	if !l.need(c, 1) {
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
	return l.i32Binary(c, arm64.ADD)
}

func (l Lowerer) i32Sub(c *jit.Context) bool {
	return l.i32Binary(c, arm64.SUB)
}

func (l Lowerer) i32Mul(c *jit.Context) bool {
	return l.i32Binary(c, arm64.MUL)
}

// i32Binary lowers an i32 binary arithmetic opcode whose result can
// land in any bit pattern (ADD, SUB, MUL). The lowered sequence runs the
// op on the boxed inputs in 64-bit registers, then re-masks and re-tags
// the result so it lands as a fresh boxed i32 on the segment stack.
func (l Lowerer) i32Binary(c *jit.Context, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	if !l.need(c, 2) {
		return false
	}
	b := c.Stack[len(c.Stack)-1]
	a := c.Stack[len(c.Stack)-2]

	raw := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(op(raw, a, b))

	boxed := l.boxI32(c, raw)
	c.Stack = append(c.Stack[:len(c.Stack)-2], boxed)
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
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

func (l Lowerer) i32Logic(c *jit.Context, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	if !l.need(c, 2) {
		return false
	}
	b := c.Stack[len(c.Stack)-1]
	a := c.Stack[len(c.Stack)-2]
	dst := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(op(dst, a, b))
	c.Stack = append(c.Stack[:len(c.Stack)-2], dst)
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	return true
}

// i32Xor needs an explicit re-tag: XORing two same-tagged inputs
// cancels the tag bits in the upper half, so we OR the tag back in.
func (l Lowerer) i32Xor(c *jit.Context) bool {
	if !l.need(c, 2) {
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
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	return true
}

// i32Eqz pops one boxed i32, compares its low 32 bits to zero, and
// pushes a boxed i32 1 (equal) or 0 (not equal).
func (l Lowerer) i32Eqz(c *jit.Context) bool {
	if !l.need(c, 1) {
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
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	return true
}

// i32Shl lowers a logical left shift on boxed i32 inputs. The shift
// count is masked to 5 bits before LSL because ARM64 register-shifts
// read more bits than i32 shift semantics allow.
func (l Lowerer) i32Shl(c *jit.Context) bool {
	return l.i32Shift(c, arm64.LSL, zero32)
}

// i32ShrS lowers an arithmetic right shift; the value lane must be
// sign-extended so the high bits carry the correct fill.
func (l Lowerer) i32ShrS(c *jit.Context) bool {
	return l.i32Shift(c, arm64.ASR, sign32)
}

// i32ShrU lowers a logical right shift; zero-extending the value
// lane drops any tag bits before the shift.
func (l Lowerer) i32ShrU(c *jit.Context) bool {
	return l.i32Shift(c, arm64.LSR, zero32)
}

func (l Lowerer) i32Shift(
	c *jit.Context,
	op func(dst, src1, src2 asm.Reg) asm.Instruction,
	prep func(*jit.Context, asm.VReg) asm.VReg,
) bool {
	if !l.need(c, 2) {
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
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
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
	if !l.need(c, 2) {
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
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	return true
}

// sign32 sign-extends the low 32 bits of v into a fresh 64-bit
// vreg so signed 64-bit compares and arithmetic produce correct results.
func sign32(c *jit.Context, v asm.VReg) asm.VReg {
	out := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.SXTW(out, v))
	return out
}

// zero32 masks v down to its low 32 bits in a fresh 64-bit vreg,
// dropping the tag bits so the result can feed into shifts or unsigned
// 64-bit compares without contamination.
func zero32(c *jit.Context, v asm.VReg) asm.VReg {
	out := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ANDI(out, v, maskI32))
	return out
}

// sign64 sign-extends bit 48 of v's value lane into bits 49..63.
// LSL by 15 pushes bit 48 to bit 63; ASR by 15 then drags the sign back
// down so the full 64-bit register holds the i64 in two's complement.
func sign64(c *jit.Context, v asm.VReg) asm.VReg {
	tmp := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.LSLI(tmp, v, signI64))
	out := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ASRI(out, tmp, signI64))
	return out
}

// zero64 masks v down to its 49-bit value lane in a fresh 64-bit
// vreg, dropping the tag bits so the result can feed into shifts or
// unsigned 64-bit compares without contamination.
func zero64(c *jit.Context, v asm.VReg) asm.VReg {
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
	if !l.need(c, 2) {
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
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	return true
}

// i64Eqz pops one boxed i64, masks off the tag, compares the value
// lane to zero, and pushes the boxed 0/1 result (as a boxed i32 per the
// WebAssembly EQZ semantics).
func (l Lowerer) i64Eqz(c *jit.Context) bool {
	if !l.need(c, 1) {
		return false
	}
	a := c.Stack[len(c.Stack)-1]

	val := zero64(c, a)
	c.Assembler.Emit(arm64.CMPI(val, 0))
	flag := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.CSET(flag, arm64.CondEQ))

	boxed := l.boxI32(c, flag)
	c.Stack[len(c.Stack)-1] = boxed
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	return true
}

// i64ShrS is safe to lower because arithmetic right shift of a
// boxable i64 stays boxable. Left shift and unsigned right shift can
// produce values that the interpreter heap-promotes, so they reject.
func (l Lowerer) i64ShrS(c *jit.Context) bool {
	if !l.need(c, 2) {
		return false
	}
	b := c.Stack[len(c.Stack)-1]
	a := c.Stack[len(c.Stack)-2]

	shift := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ANDI(shift, b, 0x3F))

	val := sign64(c, a)
	raw := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ASR(raw, val, shift))

	boxed := l.boxI64(c, raw)
	c.Stack = append(c.Stack[:len(c.Stack)-2], boxed)
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
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

// choose implements SELECT: pops cond, val2, val1 (bottom-to-top) and
// pushes val1 if cond != 0, else val2. The condition is tested against the
// low 32 bits (the i32 value lane).
func (l Lowerer) choose(c *jit.Context) bool {
	if !l.need(c, 3) {
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
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	return true
}

// localTee stores the stack top to stack[bp+idx] and leaves it on the stack.
func (l Lowerer) localTee(c *jit.Context) bool {
	width := instr.Instruction(c.Code[c.IP:]).Width()
	idx := int(c.Code[c.IP+1])
	if idx >= len(c.Snap.Locals) {
		return false
	}
	if c.Snap.Locals[idx] == types.KindRef {
		return false
	}
	if !l.need(c, 1) {
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
func (l Lowerer) globalTee(c *jit.Context) bool {
	idx, width, ok := global(c)
	if !ok {
		return false
	}
	if !l.need(c, 1) {
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
	if !l.need(c, 1) {
		return false
	}
	a := c.Stack[len(c.Stack)-1]
	// Sign-extend the low 32 bits (i32 value lane) to 64 bits.
	ext := sign32(c, a)
	boxed := l.boxI64(c, ext)
	c.Stack[len(c.Stack)-1] = boxed
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	return true
}

// i32ToI64U zero-extends the i32 value lane of a boxed i32 to a 64-bit value,
// then boxes the result as an i64.
func (l Lowerer) i32ToI64U(c *jit.Context) bool {
	if !l.need(c, 1) {
		return false
	}
	a := c.Stack[len(c.Stack)-1]
	// Zero-extend: mask to lower 32 bits (unsigned i32).
	ext := zero32(c, a)
	boxed := l.boxI64(c, ext)
	c.Stack[len(c.Stack)-1] = boxed
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	return true
}

// i64ToI32 extracts the low 32 bits of a boxed i64's value lane and boxes
// the result as a boxed i32.
func (l Lowerer) i64ToI32(c *jit.Context) bool {
	if !l.need(c, 1) {
		return false
	}
	a := c.Stack[len(c.Stack)-1]
	// Mask to 32-bit value lane from the boxed i64 (49-bit value lane contains i64).
	lo := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ANDI(lo, a, maskI32))
	boxed := l.boxI32(c, lo)
	c.Stack[len(c.Stack)-1] = boxed
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	return true
}

// ret lowers RETURN. It signals the compiler to terminate the segment
// here; the Exit call emitted by the compiler pins any stack values to ABI
// return registers and emits RET.
func (Lowerer) ret(c *jit.Context) bool {
	if !c.Whole {
		return false
	}
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	c.Stop = true
	return true
}

// call lowers a direct CALL when the immediately preceding opcode was
// CONST_GET of a known function Ref. The sequence is:
//
//  1. Store the N callee args from the shadow stack into stack[callee_bp..].
//  2. ADDI X12, X12, spOffset   (advance the native base pointer).
//  3. LDI+LDR slot_addr → entry ; BLR entry.
//  4. SUBI X12, X12, spOffset   (restore the native base pointer).
//  5. Reload stack values that survived the call.
//  6. Pop funcRef + N args; push M result VRegs from ABI return regs.
func (l Lowerer) call(c *jit.Context) bool {
	plan, ok := l.target(c, c.Target, len(c.Stack))
	if !ok {
		return false
	}
	if !l.need(c, plan.params+1) {
		return false
	}

	survivors := len(c.Stack) - plan.params - 1
	if !c.Whole && survivors > 0 {
		return false
	}
	for i := 0; i < survivors; i++ {
		addr, _ := l.localAddr(c, len(c.Snap.Locals)+i)
		c.Assembler.Emit(arm64.STR(c.Stack[i], addr, 0))
	}

	// Write args to stack[callee_bp .. callee_bp+nParams-1].
	// At this point X12 = caller_bp, so callee_bp = caller_bp + spOffset,
	// and localAddr(c, spOffset+i) addresses stack[caller_bp+spOffset+i].
	for i := 0; i < plan.params; i++ {
		arg := c.Stack[len(c.Stack)-1-plan.params+i]
		addr, _ := l.localAddr(c, plan.offset+i)
		c.Assembler.Emit(arm64.STR(arg, addr, 0))
	}

	// Advance X12 to callee_bp.
	vBPsrc := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = c.Assembler.Pin(vBPsrc, c.Scratch[jit.ScratchBP])
	vBPdst := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = c.Assembler.Pin(vBPdst, c.Scratch[jit.ScratchBP])
	c.Assembler.Emit(arm64.ADDI(vBPdst, vBPsrc, uint16(plan.offset)))

	// BL/BLR clobbers X30. Save LR so this native entry can return to its
	// caller after the callee returns.
	pushLR(c)
	if plan.self && c.Whole {
		c.Assembler.Emit(arm64.BLLabel(c.Entry))
	} else {
		vSlotAddr := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
		c.Assembler.Emit(arm64.LDI(vSlotAddr, uint64(plan.slot))...)
		vEntry := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
		c.Assembler.Emit(arm64.LDR(vEntry, vSlotAddr, 0))
		c.Assembler.Emit(arm64.BLR(vEntry))
	}
	popLR(c)

	// Restore X12 to caller_bp.
	vBPrestore := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = c.Assembler.Pin(vBPrestore, c.Scratch[jit.ScratchBP])
	vBPafter := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = c.Assembler.Pin(vBPafter, c.Scratch[jit.ScratchBP])
	c.Assembler.Emit(arm64.SUBI(vBPafter, vBPrestore, uint16(plan.offset)))

	// Capture return values from ABI return registers.
	retVregs := make([]asm.VReg, plan.returns)
	for i := range retVregs {
		v := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
		_ = c.Assembler.Pin(v, theArch.ABI().Return(i, asm.RegTypeInt, asm.Width64))
		c.Assembler.Emit(arm64.MOV(v, v))
		retVregs[i] = v
	}

	survVregs := make([]asm.VReg, survivors)
	for i := range survVregs {
		addr, _ := l.localAddr(c, len(c.Snap.Locals)+i)
		v := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
		c.Assembler.Emit(arm64.LDR(v, addr, 0))
		survVregs[i] = v
	}

	// Update the shadow stack: pop funcRef + N args, push M results.
	c.Stack = survVregs
	c.Stack = append(c.Stack, retVregs...)

	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	return true
}

func (Lowerer) target(c *jit.Context, addr int, stackLen int) (call, bool) {
	if c.Slots == nil || c.Snap.Functions == nil || addr < 0 {
		return call{}, false
	}
	callee, ok := c.Snap.Functions[addr]
	if !ok || callee.Typ == nil {
		return call{}, false
	}

	nParams := len(callee.Typ.Params)
	nReturns := len(callee.Typ.Returns)
	if nReturns > theArch.ABI().MaxReturns() {
		return call{}, false
	}
	for _, t := range callee.Typ.Params {
		if t.Kind() == types.KindRef {
			return call{}, false
		}
	}
	for _, t := range callee.Typ.Returns {
		if t.Kind() == types.KindRef {
			return call{}, false
		}
	}

	missing := nParams + 1 - stackLen
	if missing < 0 {
		missing = 0
	}
	if len(c.Inputs)+missing > theArch.ABI().MaxArgs() {
		return call{}, false
	}
	finalStack := stackLen + missing
	spOffset := len(c.Snap.Locals) + finalStack - nParams - 1
	if spOffset < 0 || spOffset > 4095 {
		return call{}, false
	}
	if nParams > 0 && (spOffset+nParams-1)*8 > 4095 {
		return call{}, false
	}

	slot, err := c.Slots.For(addr)
	if err != nil {
		return call{}, false
	}
	return call{
		params:  nParams,
		returns: nReturns,
		offset:  spOffset,
		slot:    uintptr(slot),
		self:    addr == c.Self,
	}, true
}

// refNull pushes the null reference constant (BoxedNull) onto the shadow stack.
func (l Lowerer) refNull(c *jit.Context) bool {
	return l.imm(c, uint64(types.BoxedNull), instr.Instruction(c.Code[c.IP:]).Width())
}

// refIsNull pops a boxed ref and pushes BoxI32(1) if it is null (addr == 0),
// BoxI32(0) otherwise. Null is defined as a raw bit-pattern equal to BoxedNull.
func (l Lowerer) refIsNull(c *jit.Context) bool {
	if !l.need(c, 1) {
		return false
	}
	a := c.Stack[len(c.Stack)-1]

	vNull := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.LDI(vNull, uint64(types.BoxedNull))...)
	c.Assembler.Emit(arm64.CMP(a, vNull))

	flag := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.CSET(flag, arm64.CondEQ))

	boxed := l.boxI32(c, flag)
	c.Stack[len(c.Stack)-1] = boxed
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	return true
}

// refEq pops two boxed refs and pushes BoxI32(1) if they are the same
// reference (identical bit-pattern), BoxI32(0) otherwise.
func (l Lowerer) refEq(c *jit.Context) bool {
	if !l.need(c, 2) {
		return false
	}
	b := c.Stack[len(c.Stack)-1]
	a := c.Stack[len(c.Stack)-2]

	c.Assembler.Emit(arm64.CMP(a, b))

	flag := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.CSET(flag, arm64.CondEQ))

	boxed := l.boxI32(c, flag)
	c.Stack = append(c.Stack[:len(c.Stack)-2], boxed)
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	return true
}

// br lowers an unconditional branch. In blocks mode it emits a direct
// BLabel to the target; otherwise no instructions are emitted and Exit
// writes the target IP to scratch.
func (Lowerer) br(c *jit.Context) bool {
	offset := int(int16(binary.LittleEndian.Uint16(c.Code[c.IP+1 : c.IP+3])))
	target := c.IP + 3 + offset
	if c.Labels != nil {
		lbl, ok := c.Labels[target]
		if !ok {
			return false
		}
		c.Assembler.Emit(arm64.BLabel(lbl))
		c.IP = target
		c.Stop = true
		c.Closed = true
		return true
	}
	c.IP = target
	c.Successor = c.IP
	c.Stop = true
	return true
}

// brIf lowers a conditional branch. In blocks mode (c.Labels != nil) it
// emits a single CBNZ to the taken label and falls through to the false
// target, closing the block without interpreter exits. In segment mode
// it emits two inline exit paths (false-target and taken-target), each
// writing the appropriate nextIP to scratch and RET-ing.
func (l Lowerer) brIf(c *jit.Context) bool {
	if !l.need(c, 1) {
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

	if c.Labels != nil {
		// Blocks mode: emit intra-function conditional branch. Both targets
		// must be known block starts; fall back to segment mode if not.
		takenLbl, ok := c.Labels[takenTarget]
		if !ok {
			return false
		}
		c.Assembler.Emit(arm64.CBNZLabel(condI32, takenLbl))
		// Fall through to falseTarget — no interpreter exit needed.
		c.IP = falseTarget
		c.Stop = true
		c.Closed = true
		return true
	}

	// Segment mode: pin remaining stack to ABI registers for both paths.
	l.bind(c)
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

// brTable lowers BR_TABLE. It pops a boxed i32 condition, clamps it to
// [0..count], and emits a linear scan of CMPI+B.EQ pairs — one per case — that
// each jump to an inline exit path. The default exit falls through below the
// scan. Every exit pins the remaining shadow stack to ABI returns, loads the
// compile-time target IP into ScratchNext, and RETs.
//
// In blocks mode (c.Labels != nil) label-based dispatch is not yet
// implemented, so brTable rejects and blocks() falls back to segments().
func (l Lowerer) brTable(c *jit.Context) bool {
	if c.Labels != nil {
		return false
	}
	if !l.need(c, 1) {
		return false
	}

	count := int(c.Code[c.IP+1])
	width := count*2 + 4

	targets := make([]int, count+1)
	for i := range targets {
		at := c.IP + 2 + i*2
		offset := int(int16(binary.LittleEndian.Uint16(c.Code[at : at+2])))
		targets[i] = c.IP + width + offset
	}

	cond := c.Stack[len(c.Stack)-1]
	c.Stack = c.Stack[:len(c.Stack)-1]

	// Extract unsigned i32 value lane (negative i32s become large unsigned
	// values and fall through to the default).
	condI32 := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ANDI(condI32, cond, maskI32))

	// Pin remaining shadow stack and inputs to ABI registers once — all exit
	// paths share the same live-value shape.
	l.bind(c)
	c.Returns = c.Returns[:0]
	for i, v := range c.Stack {
		ret := theArch.ABI().Return(i, v.Type(), v.Width())
		_ = c.Assembler.Pin(v, ret)
		c.Returns = append(c.Returns, ret)
	}

	rNext := c.Scratch[jit.ScratchNext]

	// Emit one CMPI+B.EQ per case.
	labels := make([]asm.Label, count)
	for i := range labels {
		labels[i] = c.Assembler.Label()
		c.Assembler.Emit(arm64.CMPI(condI32, uint16(i)))
		c.Assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, labels[i]))
	}

	// Default exit (fall-through when no case matched).
	vNextDef := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = c.Assembler.Pin(vNextDef, rNext)
	c.Assembler.Emit(arm64.LDI(vNextDef, uint64(targets[count]))...)
	c.Assembler.Emit(arm64.RET())

	// Per-case exits.
	for i := 0; i < count; i++ {
		c.Assembler.Bind(labels[i])
		vNext := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
		_ = c.Assembler.Pin(vNext, rNext)
		c.Assembler.Emit(arm64.LDI(vNext, uint64(targets[i]))...)
		c.Assembler.Emit(arm64.RET())
	}

	c.IP += width
	c.Stop = true
	c.Closed = true
	c.Successor = targets[count]
	return true
}

// f32Binary lowers an F32 binary arithmetic opcode. Both boxed-f32 inputs are
// unboxed to float32 registers, the operation is performed, and the result is
// reboxed as a boxed f32.
func (l Lowerer) f32Binary(c *jit.Context, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	if !l.need(c, 2) {
		return false
	}
	b := c.Stack[len(c.Stack)-1]
	a := c.Stack[len(c.Stack)-2]

	fa := l.unboxF32(c, a)
	fb := l.unboxF32(c, b)
	fr := c.Assembler.Reg(asm.RegTypeFloat, asm.Width32)
	c.Assembler.Emit(op(fr, fa, fb))

	boxed := l.reboxF32(c, fr)
	c.Stack = append(c.Stack[:len(c.Stack)-2], boxed)
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	return true
}

// f64Binary lowers an F64 binary arithmetic opcode. Both boxed-f64 inputs are
// unboxed to float64 registers, the operation is performed, and the result is
// reboxed as a boxed f64.
func (l Lowerer) f64Binary(c *jit.Context, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	if !l.need(c, 2) {
		return false
	}
	b := c.Stack[len(c.Stack)-1]
	a := c.Stack[len(c.Stack)-2]

	fa := l.unboxF64(c, a)
	fb := l.unboxF64(c, b)
	fr := c.Assembler.Reg(asm.RegTypeFloat, asm.Width64)
	c.Assembler.Emit(op(fr, fa, fb))

	boxed := l.reboxF64(c, fr)
	c.Stack = append(c.Stack[:len(c.Stack)-2], boxed)
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	return true
}

// f32Cmp pops two boxed f32 values, runs FCMP on them, and pushes a boxed i32
// 0/1 from the chosen condition code. NaN comparisons are unordered; EQ/NE
// may not fully honour WebAssembly NaN semantics in Phase A.
func (l Lowerer) f32Cmp(c *jit.Context, cond uint8) bool {
	if !l.need(c, 2) {
		return false
	}
	b := c.Stack[len(c.Stack)-1]
	a := c.Stack[len(c.Stack)-2]

	fa := l.unboxF32(c, a)
	fb := l.unboxF32(c, b)
	c.Assembler.Emit(arm64.FCMP(fa, fb))

	flag := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.CSET(flag, cond))

	boxed := l.boxI32(c, flag)
	c.Stack = append(c.Stack[:len(c.Stack)-2], boxed)
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	return true
}

// f64Cmp pops two boxed f64 values, runs FCMP on them, and pushes a boxed i32
// 0/1 from the chosen condition code.
func (l Lowerer) f64Cmp(c *jit.Context, cond uint8) bool {
	if !l.need(c, 2) {
		return false
	}
	b := c.Stack[len(c.Stack)-1]
	a := c.Stack[len(c.Stack)-2]

	fa := l.unboxF64(c, a)
	fb := l.unboxF64(c, b)
	c.Assembler.Emit(arm64.FCMP(fa, fb))

	flag := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.CSET(flag, cond))

	boxed := l.boxI32(c, flag)
	c.Stack = append(c.Stack[:len(c.Stack)-2], boxed)
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	return true
}

// toFloat pops one boxed integer value, extracts its value lane via prep,
// converts it to a float of fWidth using cvtf (SCVTF or UCVTF), then boxes
// the result as f32 (Width32) or f64 (Width64).
func (l Lowerer) toFloat(
	c *jit.Context,
	fWidth asm.RegWidth,
	cvtf func(dst, src asm.Reg) asm.Instruction,
	prep func(*jit.Context, asm.VReg) asm.VReg,
) bool {
	if !l.need(c, 1) {
		return false
	}
	a := c.Stack[len(c.Stack)-1]
	val := prep(c, a)
	fr := c.Assembler.Reg(asm.RegTypeFloat, fWidth)
	c.Assembler.Emit(cvtf(fr, val))

	var boxed asm.VReg
	if fWidth == asm.Width32 {
		boxed = l.reboxF32(c, fr)
	} else {
		boxed = l.reboxF64(c, fr)
	}
	c.Stack[len(c.Stack)-1] = boxed
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	return true
}

// f32ToF64 pops a boxed f32, converts its float32 value to float64 via
// FCVT, and pushes the result as a boxed f64.
func (l Lowerer) f32ToF64(c *jit.Context) bool {
	if !l.need(c, 1) {
		return false
	}
	a := c.Stack[len(c.Stack)-1]
	fa := l.unboxF32(c, a)
	fd := c.Assembler.Reg(asm.RegTypeFloat, asm.Width64)
	c.Assembler.Emit(arm64.FCVT(fd, fa))
	boxed := l.reboxF64(c, fd)
	c.Stack[len(c.Stack)-1] = boxed
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	return true
}

// f64ToF32 pops a boxed f64, converts its float64 value to float32 via
// FCVT, and pushes the result as a boxed f32.
func (l Lowerer) f64ToF32(c *jit.Context) bool {
	if !l.need(c, 1) {
		return false
	}
	a := c.Stack[len(c.Stack)-1]
	fa := l.unboxF64(c, a)
	fs := c.Assembler.Reg(asm.RegTypeFloat, asm.Width32)
	c.Assembler.Emit(arm64.FCVT(fs, fa))
	boxed := l.reboxF32(c, fs)
	c.Stack[len(c.Stack)-1] = boxed
	c.IP += instr.Instruction(c.Code[c.IP:]).Width()
	return true
}

// unboxF32 extracts the float32 bit pattern from a boxed f32 (tagF32 | bits)
// by masking to the low 32 bits and issuing FMOV with a Width64 int source
// (the encoder uses the physical W alias, i.e. the low 32 bits of the X register).
func (l Lowerer) unboxF32(c *jit.Context, v asm.VReg) asm.VReg {
	bits := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ANDI(bits, v, maskI32))
	f := c.Assembler.Reg(asm.RegTypeFloat, asm.Width32)
	c.Assembler.Emit(arm64.FMOV(f, bits))
	return f
}

// reboxF32 boxes a float32 register back to a boxed f32 value. FMOV with a
// Width64 int destination zero-extends the float32 bits to 64 bits, then
// tagF32 is OR-ed in.
func (l Lowerer) reboxF32(c *jit.Context, f asm.VReg) asm.VReg {
	bits := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.FMOV(bits, f))
	tag := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.LDI(tag, tagF32)...)
	boxed := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.ORR(boxed, bits, tag))
	return boxed
}

// unboxF64 interprets the raw bits of a boxed f64 (stored as IEEE 754 float64
// bits) as a Float64 register via FMOV.
func (Lowerer) unboxF64(c *jit.Context, v asm.VReg) asm.VReg {
	f := c.Assembler.Reg(asm.RegTypeFloat, asm.Width64)
	c.Assembler.Emit(arm64.FMOV(f, v))
	return f
}

// reboxF64 packs a Float64 register back to a boxed f64 by moving the raw
// bits into an Int64 register via FMOV. BoxF64 stores the raw float64 bits
// directly, so no tag OR is needed.
func (Lowerer) reboxF64(c *jit.Context, f asm.VReg) asm.VReg {
	bits := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
	c.Assembler.Emit(arm64.FMOV(bits, f))
	return bits
}

func (Lowerer) need(c *jit.Context, n int) bool {
	missing := n - len(c.Stack)
	if missing <= 0 {
		return true
	}
	if c.Whole {
		return false
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

func global(c *jit.Context) (int, int, bool) {
	width := instr.Instruction(c.Code[c.IP:]).Width()
	idx := int(uint16(c.Code[c.IP+1]) | uint16(c.Code[c.IP+2])<<8)
	if idx >= len(c.Snap.Globals) {
		return 0, 0, false
	}
	if c.Snap.Globals[idx].Kind() == types.KindRef {
		return 0, 0, false
	}
	// LDR/STR unsigned-offset encodes at most 12 bits (0..4095 slots x 8 bytes).
	if idx > 4095 {
		return 0, 0, false
	}
	return idx, width, true
}

func (Lowerer) bind(c *jit.Context) {
	c.Args = c.Args[:0]
	for i, v := range c.Inputs {
		arg := theArch.ABI().Arg(i, v.Type(), v.Width())
		_ = c.Assembler.Pin(v, arg)
		c.Args = append(c.Args, arg)
	}
}
