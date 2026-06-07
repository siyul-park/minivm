package interp

import (
	"encoding/binary"
	"math"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// arm64JIT is the AArch64 opcode emitter.
type arm64JIT struct{}

// Boxing masks and tags used by scalar lowering.
const (
	tagI32  = uint64(0x7FF6_0000_0000_0000)
	maskI32 = uint64(0xFFFFFFFF)

	tagI64  = uint64(0x7FF4_0000_0000_0000)
	maskI64 = uint64(0x0001_FFFF_FFFF_FFFF)

	tagF32 = uint64(0x7FF2_0000_0000_0000)

	tagRef = uint64(0x7FF8_0000_0000_0000)

	signI64 = uint8(15)
)

var _ lowerer = arm64JIT{}

func newJITCompiler(cutoff int) (*jitCompiler, error) {
	buffer, err := asm.NewBuffer(4096)
	if err != nil {
		return nil, err
	}
	return &jitCompiler{lowerer: arm64JIT{}, arch: arm64.New(), buffer: buffer, cutoff: cutoff}, nil
}

// Prologue loads declared live-ins from the VM stack.
func (l arm64JIT) prologue(ctx *jitContext, _ *types.Function) {
	if len(ctx.inputs) == 0 {
		return
	}
	vStack := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(vStack, ctx.scratch[scratchStack])
	vSP := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(vSP, ctx.scratch[scratchSP])
	for i, v := range ctx.inputs {
		idx := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		back := len(ctx.inputs) - i
		ctx.assembler.Emit(arm64.SUBI(idx, vSP, uint16(back)))
		l.load(ctx, v, vStack, idx)
	}
}

// Lower dispatches one opcode. Returns false (leaving jitContext untouched)
// for opcodes Phase A does not yet implement.
func (l arm64JIT) lower(ctx *jitContext, op instr.Opcode) bool {
	switch op {
	case instr.NOP:
		return true
	case instr.UNREACHABLE:
		return l.unreachable(ctx)
	case instr.DROP:
		return l.drop(ctx)
	case instr.DUP:
		return l.dup(ctx)
	case instr.SWAP:
		return l.swap(ctx)
	case instr.I32_CONST:
		return l.i32Const(ctx)
	case instr.I64_CONST:
		return l.i64Const(ctx)
	case instr.F32_CONST:
		return l.f32Const(ctx)
	case instr.F64_CONST:
		return l.f64Const(ctx)
	case instr.CONST_GET:
		if ctx.framed && l.call(ctx) {
			return true
		}
		return l.constGet(ctx)
	case instr.GLOBAL_GET:
		return l.globalGet(ctx)
	case instr.GLOBAL_SET:
		return l.globalSet(ctx)
	case instr.LOCAL_GET:
		return l.localGet(ctx)
	case instr.LOCAL_SET:
		return l.localSet(ctx)
	case instr.I32_ADD:
		return l.i32Add(ctx)
	case instr.I32_SUB:
		return l.i32Sub(ctx)
	case instr.I32_MUL:
		return l.i32Mul(ctx)
	case instr.I32_AND:
		return l.i32And(ctx)
	case instr.I32_OR:
		return l.i32Or(ctx)
	case instr.I32_XOR:
		return l.i32Xor(ctx)
	case instr.I32_EQZ:
		return l.i32Eqz(ctx)
	case instr.I32_SHL:
		return l.i32Shl(ctx)
	case instr.I32_SHR_S:
		return l.i32ShrS(ctx)
	case instr.I32_SHR_U:
		return l.i32ShrU(ctx)
	case instr.I32_EQ:
		return l.i32Cmp(ctx, nil, arm64.CondEQ)
	case instr.I32_NE:
		return l.i32Cmp(ctx, nil, arm64.CondNE)
	case instr.I32_LT_S:
		return l.i32Cmp(ctx, l.sign32, arm64.CondLT)
	case instr.I32_LE_S:
		return l.i32Cmp(ctx, l.sign32, arm64.CondLE)
	case instr.I32_GT_S:
		return l.i32Cmp(ctx, l.sign32, arm64.CondGT)
	case instr.I32_GE_S:
		return l.i32Cmp(ctx, l.sign32, arm64.CondGE)
	case instr.I32_LT_U:
		return l.i32Cmp(ctx, l.zero32, arm64.CondCC)
	case instr.I32_LE_U:
		return l.i32Cmp(ctx, l.zero32, arm64.CondLS)
	case instr.I32_GT_U:
		return l.i32Cmp(ctx, l.zero32, arm64.CondHI)
	case instr.I32_GE_U:
		return l.i32Cmp(ctx, l.zero32, arm64.CondCS)
	case instr.I64_EQ:
		return l.i64Cmp(ctx, nil, arm64.CondEQ)
	case instr.I64_NE:
		return l.i64Cmp(ctx, nil, arm64.CondNE)
	case instr.I64_EQZ:
		return l.i64Eqz(ctx)
	case instr.I64_ADD:
		return l.i64Add(ctx)
	case instr.I64_LT_S:
		return l.i64Cmp(ctx, l.sign64, arm64.CondLT)
	case instr.I64_LE_S:
		return l.i64Cmp(ctx, l.sign64, arm64.CondLE)
	case instr.I64_GT_S:
		return l.i64Cmp(ctx, l.sign64, arm64.CondGT)
	case instr.I64_GE_S:
		return l.i64Cmp(ctx, l.sign64, arm64.CondGE)
	case instr.I64_LT_U:
		return l.i64Cmp(ctx, l.zero64, arm64.CondCC)
	case instr.I64_LE_U:
		return l.i64Cmp(ctx, l.zero64, arm64.CondLS)
	case instr.I64_GT_U:
		return l.i64Cmp(ctx, l.zero64, arm64.CondHI)
	case instr.I64_GE_U:
		return l.i64Cmp(ctx, l.zero64, arm64.CondCS)
	case instr.I64_SHR_S:
		return l.i64ShrS(ctx)
	case instr.BR:
		return l.br(ctx)
	case instr.BR_IF:
		return l.brIf(ctx)
	case instr.BR_TABLE:
		return l.brTable(ctx)
	case instr.SELECT:
		return l.choose(ctx)
	case instr.LOCAL_TEE:
		return l.localTee(ctx)
	case instr.GLOBAL_TEE:
		return l.globalTee(ctx)
	case instr.I32_TO_I64_S:
		return l.i32ToI64S(ctx)
	case instr.I32_TO_I64_U:
		return l.i32ToI64U(ctx)
	case instr.I64_TO_I32:
		return l.i64ToI32(ctx)
	case instr.F32_ADD:
		return l.f32Binary(ctx, arm64.FADD)
	case instr.F32_SUB:
		return l.f32Binary(ctx, arm64.FSUB)
	case instr.F32_MUL:
		return l.f32Binary(ctx, arm64.FMUL)
	case instr.F32_DIV:
		return l.f32Binary(ctx, arm64.FDIV)
	case instr.F32_EQ:
		return l.f32Cmp(ctx, arm64.CondEQ)
	case instr.F32_NE:
		return l.f32Cmp(ctx, arm64.CondNE)
	case instr.F32_LT:
		return l.f32Cmp(ctx, arm64.CondMI)
	case instr.F32_GT:
		return l.f32Cmp(ctx, arm64.CondGT)
	case instr.F32_LE:
		return l.f32Cmp(ctx, arm64.CondLS)
	case instr.F32_GE:
		return l.f32Cmp(ctx, arm64.CondGE)
	case instr.F64_ADD:
		return l.f64Binary(ctx, arm64.FADD)
	case instr.F64_SUB:
		return l.f64Binary(ctx, arm64.FSUB)
	case instr.F64_MUL:
		return l.f64Binary(ctx, arm64.FMUL)
	case instr.F64_DIV:
		return l.f64Binary(ctx, arm64.FDIV)
	case instr.F64_EQ:
		return l.f64Cmp(ctx, arm64.CondEQ)
	case instr.F64_NE:
		return l.f64Cmp(ctx, arm64.CondNE)
	case instr.F64_LT:
		return l.f64Cmp(ctx, arm64.CondMI)
	case instr.F64_GT:
		return l.f64Cmp(ctx, arm64.CondGT)
	case instr.F64_LE:
		return l.f64Cmp(ctx, arm64.CondLS)
	case instr.F64_GE:
		return l.f64Cmp(ctx, arm64.CondGE)
	case instr.I32_TO_F32_S:
		return l.toFloat(ctx, asm.Width32, arm64.SCVTF, l.sign32)
	case instr.I32_TO_F32_U:
		return l.toFloat(ctx, asm.Width32, arm64.UCVTF, l.zero32)
	case instr.I64_TO_F32_S:
		return l.toFloat(ctx, asm.Width32, arm64.SCVTF, l.sign64)
	case instr.I64_TO_F32_U:
		return l.toFloat(ctx, asm.Width32, arm64.UCVTF, l.zero64)
	case instr.I32_TO_F64_S:
		return l.toFloat(ctx, asm.Width64, arm64.SCVTF, l.sign32)
	case instr.I32_TO_F64_U:
		return l.toFloat(ctx, asm.Width64, arm64.UCVTF, l.zero32)
	case instr.I64_TO_F64_S:
		return l.toFloat(ctx, asm.Width64, arm64.SCVTF, l.sign64)
	case instr.I64_TO_F64_U:
		return l.toFloat(ctx, asm.Width64, arm64.UCVTF, l.zero64)
	case instr.F32_TO_F64:
		return l.f32ToF64(ctx)
	case instr.F64_TO_F32:
		return l.f64ToF32(ctx)
	case instr.RETURN:
		return l.ret(ctx)
	case instr.CALL:
		return false
	case instr.REF_NULL:
		return l.refNull(ctx)
	case instr.REF_IS_NULL:
		return l.refIsNull(ctx)
	case instr.REF_EQ:
		return l.refEq(ctx)
	}
	return false
}

func (arm64JIT) enter(ctx *jitContext) {
	ctx.assembler.Emit(
		arm64.SUBI(arm64.SP, arm64.SP, 16),
		arm64.STR(arm64.LR, arm64.SP, 0),
	)
}

// Exit materializes the shadow stack back into the interpreter stack, writes
// the next interpreter IP, and returns through the scratch-only trampoline.
func (l arm64JIT) exitIP(ctx *jitContext, nextIP int) {
	l.exit(ctx, uint64(nextIP))
}

func (l arm64JIT) exitFallback(ctx *jitContext, nextIP int) {
	ctx.fallback = true
	l.exit(ctx, scratchFallback|uint64(nextIP))
}

func (l arm64JIT) exit(ctx *jitContext, nextIP uint64) {
	rNext := ctx.scratch[scratchNext]

	nextSP := l.materialize(ctx)

	vSP := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(vSP, ctx.scratch[scratchSP])
	ctx.assembler.Emit(arm64.MOV(vSP, nextSP))

	vNext := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(vNext, rNext)
	ctx.assembler.Emit(arm64.LDI(vNext, nextIP)...)

	if ctx.framed {
		ctx.assembler.Emit(
			arm64.LDR(arm64.LR, arm64.SP, 0),
			arm64.ADDI(arm64.SP, arm64.SP, 16),
		)
	}
	ctx.assembler.Emit(arm64.RET())
}

func (arm64JIT) materialize(ctx *jitContext) asm.VReg {
	vStack := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(vStack, ctx.scratch[scratchStack])

	vBase := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	if ctx.whole {
		_ = ctx.assembler.Pin(vBase, ctx.scratch[scratchBP])
	} else {
		_ = ctx.assembler.Pin(vBase, ctx.scratch[scratchSP])
		if len(ctx.inputs) > 0 {
			out := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
			ctx.assembler.Emit(arm64.SUBI(out, vBase, uint16(len(ctx.inputs))))
			vBase = out
		}
	}

	for idx, v := range ctx.stack {
		slot := vBase
		if idx > 0 {
			slot = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
			ctx.assembler.Emit(arm64.ADDI(slot, vBase, uint16(idx)))
		}
		(arm64JIT{}).store(ctx, v, vStack, slot)
	}

	nextSP := vBase
	if len(ctx.stack) > 0 {
		nextSP = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.ADDI(nextSP, vBase, uint16(len(ctx.stack))))
	}
	return nextSP
}

func (l arm64JIT) materializeSP(ctx *jitContext) asm.VReg {
	vStack := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(vStack, ctx.scratch[scratchStack])
	vSP := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(vSP, ctx.scratch[scratchSP])
	for idx, v := range ctx.stack {
		slot := vSP
		if idx > 0 {
			slot = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
			ctx.assembler.Emit(arm64.ADDI(slot, vSP, uint16(idx)))
		}
		l.store(ctx, v, vStack, slot)
	}
	nextSP := vSP
	if len(ctx.stack) > 0 {
		nextSP = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.ADDI(nextSP, vSP, uint16(len(ctx.stack))))
	}
	return nextSP
}

func (arm64JIT) load(ctx *jitContext, dst asm.VReg, base, slot asm.Reg) {
	addr := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	off := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LSLI(off, slot, 3))
	ctx.assembler.Emit(arm64.ADD(addr, base, off))
	ctx.assembler.Emit(arm64.LDR(dst, addr, 0))
}

func (arm64JIT) store(ctx *jitContext, src asm.VReg, base, slot asm.Reg) {
	addr := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	off := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LSLI(off, slot, 3))
	ctx.assembler.Emit(arm64.ADD(addr, base, off))
	ctx.assembler.Emit(arm64.STR(src, addr, 0))
}

func (l arm64JIT) drop(ctx *jitContext) bool {
	if !l.need(ctx, 1) {
		return false
	}
	ctx.stack = ctx.stack[:len(ctx.stack)-1]
	return true
}

func (l arm64JIT) unreachable(ctx *jitContext) bool {
	l.exitFallback(ctx, ctx.ip)
	ctx.stop = true
	ctx.closed = true
	return true
}

func (l arm64JIT) dup(ctx *jitContext) bool {
	if !l.need(ctx, 1) {
		return false
	}
	top := ctx.stack[len(ctx.stack)-1]
	dst := ctx.assembler.Reg(top.Type(), top.Width())
	ctx.assembler.Emit(arm64.MOV(dst, top))
	ctx.stack = append(ctx.stack, dst)
	return true
}

func (l arm64JIT) i32Const(ctx *jitContext) bool {
	width := instr.Instruction(ctx.code[ctx.ip:]).Width()
	val := int32(binary.LittleEndian.Uint32(ctx.code[ctx.ip+1 : ctx.ip+width]))
	return l.imm(ctx, uint64(types.BoxI32(val)))
}

func (l arm64JIT) i64Const(ctx *jitContext) bool {
	width := instr.Instruction(ctx.code[ctx.ip:]).Width()
	val := int64(binary.LittleEndian.Uint64(ctx.code[ctx.ip+1 : ctx.ip+width]))
	// Skip values that would heap-promote during interp boxing; segment
	// must produce an authentic Boxed without heap allocation.
	if !types.IsBoxable(val) {
		return false
	}
	return l.imm(ctx, uint64(types.BoxI64(val)))
}

func (l arm64JIT) f32Const(ctx *jitContext) bool {
	width := instr.Instruction(ctx.code[ctx.ip:]).Width()
	bits := binary.LittleEndian.Uint32(ctx.code[ctx.ip+1 : ctx.ip+width])
	return l.imm(ctx, uint64(types.BoxF32(math.Float32frombits(bits))))
}

func (l arm64JIT) f64Const(ctx *jitContext) bool {
	width := instr.Instruction(ctx.code[ctx.ip:]).Width()
	bits := binary.LittleEndian.Uint64(ctx.code[ctx.ip+1 : ctx.ip+width])
	return l.imm(ctx, uint64(types.BoxF64(math.Float64frombits(bits))))
}

// imm loads boxed as a 64-bit immediate into a fresh VReg and tracks it
// on the segment-local stack shadow. The driver advances IP after Lower
// returns true.
func (arm64JIT) imm(ctx *jitContext, boxed uint64) bool {
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(dst, boxed)...)
	ctx.stack = append(ctx.stack, dst)
	return true
}

func (l arm64JIT) swap(ctx *jitContext) bool {
	if !l.need(ctx, 2) {
		return false
	}
	last := len(ctx.stack) - 1
	ctx.stack[last], ctx.stack[last-1] = ctx.stack[last-1], ctx.stack[last]
	return true
}

func (l arm64JIT) constGet(ctx *jitContext) bool {
	idx := int(uint16(ctx.code[ctx.ip+1]) | uint16(ctx.code[ctx.ip+2])<<8)
	if idx >= len(ctx.constants) {
		return false
	}
	v := ctx.constants[idx]
	if v.Kind() == types.KindRef {
		return false
	}
	return l.imm(ctx, uint64(v))
}

func (l arm64JIT) call(ctx *jitContext) bool {
	if ctx.ip+3 >= len(ctx.code) || instr.Opcode(ctx.code[ctx.ip]) != instr.CONST_GET || instr.Opcode(ctx.code[ctx.ip+3]) != instr.CALL {
		return false
	}
	idx := int(uint16(ctx.code[ctx.ip+1]) | uint16(ctx.code[ctx.ip+2])<<8)
	if idx < 0 || idx >= len(ctx.constants) || ctx.constants[idx].Kind() != types.KindRef {
		return false
	}
	target, ok := ctx.targets[ctx.constants[idx].Ref()]
	if !ok || target.fn == nil || target.fn.Typ == nil {
		return false
	}
	params := len(target.fn.Typ.Params)
	returns := len(target.fn.Typ.Returns)
	locals := len(target.fn.Locals)
	if !l.need(ctx, params) {
		return false
	}

	nextSP := l.materializeSP(ctx)
	oldBP := ctx.scratch[scratchBP]
	oldSP := ctx.scratch[scratchSP]
	ctx.assembler.Emit(
		arm64.SUBI(arm64.SP, arm64.SP, 16),
		arm64.STR(oldBP, arm64.SP, 0),
		arm64.STR(oldSP, arm64.SP, 8),
	)

	calleeBP := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.SUBI(calleeBP, nextSP, uint16(params)))
	vBP := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(vBP, oldBP)
	ctx.assembler.Emit(arm64.MOV(vBP, calleeBP))

	calleeSP := calleeBP
	if params+locals > 0 {
		calleeSP = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.ADDI(calleeSP, calleeBP, uint16(params+locals)))
	}
	vSP := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(vSP, oldSP)
	ctx.assembler.Emit(arm64.MOV(vSP, calleeSP))

	ctx.assembler.Emit(arm64.BLLabel(target.label))
	ctx.assembler.Emit(
		arm64.LDR(oldBP, arm64.SP, 0),
		arm64.LDR(oldSP, arm64.SP, 8),
		arm64.ADDI(arm64.SP, arm64.SP, 16),
	)

	vStack := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(vStack, ctx.scratch[scratchStack])
	base := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.MOV(base, oldSP))

	next := len(ctx.stack) - params + returns
	stack := make([]asm.VReg, next)
	for i := 0; i < next; i++ {
		slot := base
		if i > 0 {
			slot = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
			ctx.assembler.Emit(arm64.ADDI(slot, base, uint16(i)))
		}
		v := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		l.load(ctx, v, vStack, slot)
		stack[i] = v
	}
	ctx.stack = stack
	ctx.ip += 4
	return true
}

// globalGet pushes globals[idx] onto the segment stack via a direct
// LDR from the globals base. Rejects when globals[idx] is a ref because
// Phase A does not yet model the runtime retain.
func (l arm64JIT) globalGet(ctx *jitContext) bool {
	idx, ok := l.global(ctx)
	if !ok {
		return false
	}
	pre := append([]asm.VReg(nil), ctx.stack...)
	vGlobal := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(vGlobal, ctx.scratch[scratchGlobals])
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(dst, vGlobal, int16(idx*8)))
	l.guardRef(ctx, dst, pre)
	if ctx.globals[idx].Kind() == types.KindI64 {
		l.guardI64(ctx, dst)
	}
	ctx.stack = append(ctx.stack, dst)
	return true
}

// globalSet pops the segment stack top and stores it to globals[idx].
// The same ref-handling restriction as globalGet applies; in addition,
// SET overwriting a previously held ref would leak it, so a current ref in
// globals[idx] also rejects.
func (l arm64JIT) globalSet(ctx *jitContext) bool {
	idx, ok := l.global(ctx)
	if !ok {
		return false
	}
	if !l.need(ctx, 1) {
		return false
	}

	pre := append([]asm.VReg(nil), ctx.stack...)
	src := ctx.stack[len(ctx.stack)-1]

	vGlobal := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(vGlobal, ctx.scratch[scratchGlobals])
	old := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(old, vGlobal, int16(idx*8)))
	l.guardRef(ctx, old, pre)
	l.guardRef(ctx, src, pre)
	if ctx.globals[idx].Kind() == types.KindI64 {
		l.guardI64(ctx, old)
	}
	ctx.assembler.Emit(arm64.STR(src, vGlobal, int16(idx*8)))

	ctx.stack = ctx.stack[:len(ctx.stack)-1]
	return true
}

// localGet pushes stack[bp+idx] (a previously stored local) onto the
// segment stack via LDR. Ref locals reject for the same reason GLOBAL_GET
// rejects ref globals.
func (l arm64JIT) localGet(ctx *jitContext) bool {
	idx := int(ctx.code[ctx.ip+1])
	if idx >= len(ctx.locals) {
		return false
	}
	if ctx.locals[idx] == types.KindRef {
		return false
	}

	addr, ok := l.localAddr(ctx, idx)
	if !ok {
		return false
	}
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(dst, addr, 0))
	if ctx.locals[idx] == types.KindI64 {
		l.guardI64(ctx, dst)
	}
	ctx.stack = append(ctx.stack, dst)
	return true
}

// localSet pops the segment stack top into stack[bp+idx].
func (l arm64JIT) localSet(ctx *jitContext) bool {
	idx := int(ctx.code[ctx.ip+1])
	if idx >= len(ctx.locals) {
		return false
	}
	if ctx.locals[idx] == types.KindRef {
		return false
	}
	if !l.need(ctx, 1) {
		return false
	}

	src := ctx.stack[len(ctx.stack)-1]
	addr, ok := l.localAddr(ctx, idx)
	if !ok {
		return false
	}
	if ctx.locals[idx] == types.KindI64 {
		old := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDR(old, addr, 0))
		l.guardI64(ctx, old)
	}
	ctx.assembler.Emit(arm64.STR(src, addr, 0))

	ctx.stack = ctx.stack[:len(ctx.stack)-1]
	return true
}

// localAddr returns a VReg whose value is the byte address of
// stack[bp+idx]. The arithmetic is: rStack + (rBP + idx) * 8. The
// final +idx*8 displacement is folded into the LDR/STR offset, so this
// helper materializes only rStack + rBP*8 into the VReg.
func (arm64JIT) localAddr(ctx *jitContext, idx int) (asm.VReg, bool) {
	vStack := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(vStack, ctx.scratch[scratchStack])
	vBP := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(vBP, ctx.scratch[scratchBP])

	vShift := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	vBase := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LSLI(vShift, vBP, 3))
	ctx.assembler.Emit(arm64.ADD(vBase, vStack, vShift))

	// Caller emits LDR/STR with a #idx*8 immediate displacement off vBase.
	if idx != 0 {
		offset := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.ADDI(offset, vBase, uint16(idx*8)))
		return offset, true
	}
	return vBase, true
}

// guardI64 guards an i64 slot value against heap promotion. An i64
// whose magnitude exceeds the 49-bit boxed range is stored by the interpreter
// as a heap ref, which JIT i64 code can neither interpret (sign64/zero64 would
// read the ref's address bits as a value) nor refcount. The check isolates the
// 15 tag bits and, when they are not the inline KindI64 tag, restores the
// pre-op stack and exits to the threaded interpreter at ctx.ip so it handles the
// promotion (and the retain/release the JIT omits). Inline values fall through.
// Emit only for slots statically typed i64.
func (l arm64JIT) guardI64(ctx *jitContext, v asm.VReg) {
	pre := append([]asm.VReg(nil), ctx.stack...)

	tag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LSRI(tag, v, uint8(types.VBits)))
	want := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(want, tagI64>>types.VBits)...)
	ctx.assembler.Emit(arm64.CMP(tag, want))

	ok := ctx.assembler.Label()
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, ok))
	ctx.stack = pre
	l.exitFallback(ctx, ctx.ip)
	ctx.assembler.Bind(ok)
	ctx.stack = pre
}

func (l arm64JIT) i32Add(ctx *jitContext) bool {
	return l.i32Binary(ctx, arm64.ADD)
}

func (l arm64JIT) i32Sub(ctx *jitContext) bool {
	return l.i32Binary(ctx, arm64.SUB)
}

func (l arm64JIT) i32Mul(ctx *jitContext) bool {
	return l.i32Binary(ctx, arm64.MUL)
}

// i32Binary lowers an i32 binary arithmetic opcode whose result can
// land in any bit pattern (ADD, SUB, MUL). The lowered sequence runs the
// op on the boxed inputs in 64-bit registers, then re-masks and re-tags
// the result so it lands as a fresh boxed i32 on the segment stack.
func (l arm64JIT) i32Binary(ctx *jitContext, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	if !l.need(ctx, 2) {
		return false
	}
	b := ctx.stack[len(ctx.stack)-1]
	a := ctx.stack[len(ctx.stack)-2]

	raw := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(op(raw, a, b))

	boxed := l.boxI32(ctx, raw)
	ctx.stack = append(ctx.stack[:len(ctx.stack)-2], boxed)
	return true
}

// i32And and i32Or take the fast path: ANDing or ORing two
// boxed i32 values preserves the tag bits because both operands share
// the same tag pattern (tag&tag == tag, tag|tag == tag). No re-box step
// is required.
func (l arm64JIT) i32And(ctx *jitContext) bool {
	return l.i32Logic(ctx, arm64.AND)
}

func (l arm64JIT) i32Or(ctx *jitContext) bool {
	return l.i32Logic(ctx, arm64.ORR)
}

func (l arm64JIT) i32Logic(ctx *jitContext, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	if !l.need(ctx, 2) {
		return false
	}
	b := ctx.stack[len(ctx.stack)-1]
	a := ctx.stack[len(ctx.stack)-2]
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(op(dst, a, b))
	ctx.stack = append(ctx.stack[:len(ctx.stack)-2], dst)
	return true
}

// i32Xor needs an explicit re-tag: XORing two same-tagged inputs
// cancels the tag bits in the upper half, so we OR the tag back in.
func (l arm64JIT) i32Xor(ctx *jitContext) bool {
	if !l.need(ctx, 2) {
		return false
	}
	b := ctx.stack[len(ctx.stack)-1]
	a := ctx.stack[len(ctx.stack)-2]

	xord := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.EOR(xord, a, b))

	tag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(tag, tagI32)...)
	boxed := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ORR(boxed, xord, tag))

	ctx.stack = append(ctx.stack[:len(ctx.stack)-2], boxed)
	return true
}

// i32Eqz pops one boxed i32, compares its low 32 bits to zero, and
// pushes a boxed i32 1 (equal) or 0 (not equal).
func (l arm64JIT) i32Eqz(ctx *jitContext) bool {
	if !l.need(ctx, 1) {
		return false
	}
	a := ctx.stack[len(ctx.stack)-1]

	lo := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(lo, a, maskI32))
	ctx.assembler.Emit(arm64.CMPI(lo, 0))

	flag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.CSET(flag, arm64.CondEQ))

	boxed := l.boxI32(ctx, flag)
	ctx.stack[len(ctx.stack)-1] = boxed
	return true
}

// i32Shl lowers a logical left shift on boxed i32 inputs. The shift
// count is masked to 5 bits before LSL because ARM64 register-shifts
// read more bits than i32 shift semantics allow.
func (l arm64JIT) i32Shl(ctx *jitContext) bool {
	return l.i32Shift(ctx, arm64.LSL, l.zero32)
}

// i32ShrS lowers an arithmetic right shift; the value lane must be
// sign-extended so the high bits carry the correct fill.
func (l arm64JIT) i32ShrS(ctx *jitContext) bool {
	return l.i32Shift(ctx, arm64.ASR, l.sign32)
}

// i32ShrU lowers a logical right shift; zero-extending the value
// lane drops any tag bits before the shift.
func (l arm64JIT) i32ShrU(ctx *jitContext) bool {
	return l.i32Shift(ctx, arm64.LSR, l.zero32)
}

func (l arm64JIT) i32Shift(
	ctx *jitContext,
	op func(dst, src1, src2 asm.Reg) asm.Instruction,
	prep func(*jitContext, asm.VReg) asm.VReg,
) bool {
	if !l.need(ctx, 2) {
		return false
	}
	b := ctx.stack[len(ctx.stack)-1]
	a := ctx.stack[len(ctx.stack)-2]

	shift := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(shift, b, 0x1F))

	val := prep(ctx, a)
	raw := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(op(raw, val, shift))

	boxed := l.boxI32(ctx, raw)
	ctx.stack = append(ctx.stack[:len(ctx.stack)-2], boxed)
	return true
}

// i32Cmp pops two boxed i32 values, optionally preps each (sign- or
// zero-extending to 64 bits for signed/unsigned compares), runs CMP on
// the prepared operands, and pushes a boxed 0/1 from the chosen
// condition code. prep is nil for EQ/NE because the boxed tag is
// identical across both operands, so a raw 64-bit compare is correct.
func (l arm64JIT) i32Cmp(
	ctx *jitContext,
	prep func(*jitContext, asm.VReg) asm.VReg,
	cond uint8,
) bool {
	if !l.need(ctx, 2) {
		return false
	}
	b := ctx.stack[len(ctx.stack)-1]
	a := ctx.stack[len(ctx.stack)-2]

	if prep != nil {
		a = prep(ctx, a)
		b = prep(ctx, b)
	}
	ctx.assembler.Emit(arm64.CMP(a, b))

	flag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.CSET(flag, cond))

	boxed := l.boxI32(ctx, flag)
	ctx.stack = append(ctx.stack[:len(ctx.stack)-2], boxed)
	return true
}

// l.sign32 sign-extends the low 32 bits of v into a fresh 64-bit
// vreg so signed 64-bit compares and arithmetic produce correct results.
func (arm64JIT) sign32(ctx *jitContext, v asm.VReg) asm.VReg {
	out := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.SXTW(out, v))
	return out
}

// l.zero32 masks v down to its low 32 bits in a fresh 64-bit vreg,
// dropping the tag bits so the result can feed into shifts or unsigned
// 64-bit compares without contamination.
func (arm64JIT) zero32(ctx *jitContext, v asm.VReg) asm.VReg {
	out := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(out, v, maskI32))
	return out
}

// l.sign64 sign-extends bit 48 of v's value lane into bits 49..63.
// LSL by 15 pushes bit 48 to bit 63; ASR by 15 then drags the sign back
// down so the full 64-bit register holds the i64 in two's complement.
func (arm64JIT) sign64(ctx *jitContext, v asm.VReg) asm.VReg {
	tmp := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LSLI(tmp, v, signI64))
	out := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ASRI(out, tmp, signI64))
	return out
}

// l.zero64 masks v down to its 49-bit value lane in a fresh 64-bit
// vreg, dropping the tag bits so the result can feed into shifts or
// unsigned 64-bit compares without contamination.
func (arm64JIT) zero64(ctx *jitContext, v asm.VReg) asm.VReg {
	out := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(out, v, maskI64))
	return out
}

// i64Cmp pops two boxed i64 inputs, optionally preps each (sign- or
// zero-extending to 64 bits for signed/unsigned compares), runs CMP, and
// pushes a boxed 0/1 from the chosen condition. prep is nil for EQ/NE
// because matching tags make a 64-bit compare sufficient.
func (l arm64JIT) i64Cmp(
	ctx *jitContext,
	prep func(*jitContext, asm.VReg) asm.VReg,
	cond uint8,
) bool {
	if !l.need(ctx, 2) {
		return false
	}
	b := ctx.stack[len(ctx.stack)-1]
	a := ctx.stack[len(ctx.stack)-2]

	if prep != nil {
		a = prep(ctx, a)
		b = prep(ctx, b)
	}
	ctx.assembler.Emit(arm64.CMP(a, b))

	flag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.CSET(flag, cond))

	boxed := l.boxI32(ctx, flag)
	ctx.stack = append(ctx.stack[:len(ctx.stack)-2], boxed)
	return true
}

// i64Eqz pops one boxed i64, masks off the tag, compares the value
// lane to zero, and pushes the boxed 0/1 result (as a boxed i32 per the
// WebAssembly EQZ semantics).
func (l arm64JIT) i64Eqz(ctx *jitContext) bool {
	if !l.need(ctx, 1) {
		return false
	}
	a := ctx.stack[len(ctx.stack)-1]

	val := l.zero64(ctx, a)
	ctx.assembler.Emit(arm64.CMPI(val, 0))
	flag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.CSET(flag, arm64.CondEQ))

	boxed := l.boxI32(ctx, flag)
	ctx.stack[len(ctx.stack)-1] = boxed
	return true
}

// i64Add lowers the boxable fast path and emits an inline fallback for
// results outside the 49-bit boxed i64 range. The fallback materializes the
// pre-op stack and resumes threaded execution at this opcode.
func (l arm64JIT) i64Add(ctx *jitContext) bool {
	if !l.need(ctx, 2) {
		return false
	}
	pre := append([]asm.VReg(nil), ctx.stack...)
	b := ctx.stack[len(ctx.stack)-1]
	a := ctx.stack[len(ctx.stack)-2]

	raw := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ADD(raw, l.sign64(ctx, a), l.sign64(ctx, b)))

	shifted := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LSLI(shifted, raw, signI64))
	extended := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ASRI(extended, shifted, signI64))
	ctx.assembler.Emit(arm64.CMP(extended, raw))

	fallback := ctx.assembler.Label()
	done := ctx.assembler.Label()
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBNE, fallback))

	boxed := l.boxI64(ctx, raw)
	next := append(ctx.stack[:len(ctx.stack)-2:len(ctx.stack)-2], boxed)
	ctx.assembler.Emit(arm64.BLabel(done))

	ctx.assembler.Bind(fallback)
	ctx.stack = pre
	l.exitFallback(ctx, ctx.ip)

	ctx.assembler.Bind(done)
	ctx.stack = next
	return true
}

// i64ShrS is safe to lower because arithmetic right shift of a
// boxable i64 stays boxable. Left shift and unsigned right shift can
// produce values that the interpreter heap-promotes, so they reject.
func (l arm64JIT) i64ShrS(ctx *jitContext) bool {
	if !l.need(ctx, 2) {
		return false
	}
	b := ctx.stack[len(ctx.stack)-1]
	a := ctx.stack[len(ctx.stack)-2]

	shift := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(shift, b, 0x3F))

	val := l.sign64(ctx, a)
	raw := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ASR(raw, val, shift))

	boxed := l.boxI64(ctx, raw)
	ctx.stack = append(ctx.stack[:len(ctx.stack)-2], boxed)
	return true
}

// boxI64 masks val to the 49-bit value lane and ORs in the i64 tag.
// val may carry sign-extended high bits — the ANDI step drops them.
func (arm64JIT) boxI64(ctx *jitContext, val asm.VReg) asm.VReg {
	lo := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(lo, val, maskI64))

	tag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(tag, tagI64)...)

	boxed := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ORR(boxed, lo, tag))
	return boxed
}

// boxI32 takes a vreg holding a value whose low 32 bits carry the
// integer and whose upper 32 bits are zero (any ARM64 32-bit op or an
// ANDI mask of 0xFFFFFFFF gives this shape), and produces a fresh
// vreg holding the NaN-boxed Boxed.
func (arm64JIT) boxI32(ctx *jitContext, val asm.VReg) asm.VReg {
	lo := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(lo, val, maskI32))

	tag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(tag, tagI32)...)

	boxed := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ORR(boxed, lo, tag))
	return boxed
}

// choose implements SELECT: pops cond, val2, val1 (bottom-to-top) and
// pushes val1 if cond != 0, else val2. The condition is tested against the
// low 32 bits (the i32 value lane).
func (l arm64JIT) choose(ctx *jitContext) bool {
	if !l.need(ctx, 3) {
		return false
	}
	cond := ctx.stack[len(ctx.stack)-1]
	v2 := ctx.stack[len(ctx.stack)-2]
	v1 := ctx.stack[len(ctx.stack)-3]

	lo := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(lo, cond, maskI32))
	ctx.assembler.Emit(arm64.CMPI(lo, 0))

	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.CSEL(dst, v1, v2, arm64.CondNE))

	ctx.stack = append(ctx.stack[:len(ctx.stack)-3], dst)
	return true
}

// localTee stores the stack top to stack[bp+idx] and leaves it on the stack.
func (l arm64JIT) localTee(ctx *jitContext) bool {
	idx := int(ctx.code[ctx.ip+1])
	if idx >= len(ctx.locals) {
		return false
	}
	if ctx.locals[idx] == types.KindRef {
		return false
	}
	if !l.need(ctx, 1) {
		return false
	}

	src := ctx.stack[len(ctx.stack)-1]
	addr, ok := l.localAddr(ctx, idx)
	if !ok {
		return false
	}
	if ctx.locals[idx] == types.KindI64 {
		old := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDR(old, addr, 0))
		l.guardI64(ctx, old)
	}
	ctx.assembler.Emit(arm64.STR(src, addr, 0))
	return true
}

// globalTee stores the stack top to globals[idx] and leaves it on the stack.
func (l arm64JIT) globalTee(ctx *jitContext) bool {
	idx, ok := l.global(ctx)
	if !ok {
		return false
	}
	if !l.need(ctx, 1) {
		return false
	}

	pre := append([]asm.VReg(nil), ctx.stack...)
	src := ctx.stack[len(ctx.stack)-1]
	vGlobal := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(vGlobal, ctx.scratch[scratchGlobals])
	old := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(old, vGlobal, int16(idx*8)))
	l.guardRef(ctx, old, pre)
	l.guardRef(ctx, src, pre)
	if ctx.globals[idx].Kind() == types.KindI64 {
		l.guardI64(ctx, old)
	}
	ctx.assembler.Emit(arm64.STR(src, vGlobal, int16(idx*8)))
	return true
}

// i32ToI64S sign-extends the i32 value lane of a boxed i32 to a full 64-bit
// value, then boxes the result as an i64. All i32 values are within the
// boxable i64 range, so no overflow check is needed.
func (l arm64JIT) i32ToI64S(ctx *jitContext) bool {
	if !l.need(ctx, 1) {
		return false
	}
	a := ctx.stack[len(ctx.stack)-1]
	// Sign-extend the low 32 bits (i32 value lane) to 64 bits.
	ext := l.sign32(ctx, a)
	boxed := l.boxI64(ctx, ext)
	ctx.stack[len(ctx.stack)-1] = boxed
	return true
}

// i32ToI64U zero-extends the i32 value lane of a boxed i32 to a 64-bit value,
// then boxes the result as an i64.
func (l arm64JIT) i32ToI64U(ctx *jitContext) bool {
	if !l.need(ctx, 1) {
		return false
	}
	a := ctx.stack[len(ctx.stack)-1]
	// Zero-extend: mask to lower 32 bits (unsigned i32).
	ext := l.zero32(ctx, a)
	boxed := l.boxI64(ctx, ext)
	ctx.stack[len(ctx.stack)-1] = boxed
	return true
}

// i64ToI32 extracts the low 32 bits of a boxed i64's value lane and boxes
// the result as a boxed i32.
func (l arm64JIT) i64ToI32(ctx *jitContext) bool {
	if !l.need(ctx, 1) {
		return false
	}
	a := ctx.stack[len(ctx.stack)-1]
	// Mask to 32-bit value lane from the boxed i64 (49-bit value lane contains i64).
	lo := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(lo, a, maskI32))
	boxed := l.boxI32(ctx, lo)
	ctx.stack[len(ctx.stack)-1] = boxed
	return true
}

// ret lowers RETURN. It signals the compiler to terminate the segment
// here; the Exit call emitted by the compiler pins any stack values to ABI
// return registers and emits RET.
func (arm64JIT) ret(ctx *jitContext) bool {
	if ctx.framed {
		if len(ctx.stack) < ctx.returns {
			return false
		}
		vStack := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		_ = ctx.assembler.Pin(vStack, ctx.scratch[scratchStack])
		vBP := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		_ = ctx.assembler.Pin(vBP, ctx.scratch[scratchBP])
		for idx := 0; idx < ctx.returns; idx++ {
			src := ctx.stack[len(ctx.stack)-ctx.returns+idx]
			slot := vBP
			if idx > 0 {
				slot = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
				ctx.assembler.Emit(arm64.ADDI(slot, vBP, uint16(idx)))
			}
			(arm64JIT{}).store(ctx, src, vStack, slot)
		}
		ctx.assembler.Emit(
			arm64.LDR(arm64.LR, arm64.SP, 0),
			arm64.ADDI(arm64.SP, arm64.SP, 16),
			arm64.RET(),
		)
		ctx.stop = true
		ctx.closed = true
		return true
	}
	if !ctx.whole {
		return false
	}
	ctx.stop = true
	return true
}

// refNull pushes the null reference constant (BoxedNull) onto the shadow stack.
func (l arm64JIT) refNull(ctx *jitContext) bool {
	return l.imm(ctx, uint64(types.BoxedNull))
}

// refIsNull pops a boxed ref and pushes BoxI32(1) if it is null (addr == 0),
// BoxI32(0) otherwise. Null is defined as a raw bit-pattern equal to BoxedNull.
func (l arm64JIT) refIsNull(ctx *jitContext) bool {
	if !l.need(ctx, 1) {
		return false
	}
	a := ctx.stack[len(ctx.stack)-1]

	vNull := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(vNull, uint64(types.BoxedNull))...)
	ctx.assembler.Emit(arm64.CMP(a, vNull))

	flag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.CSET(flag, arm64.CondEQ))

	boxed := l.boxI32(ctx, flag)
	ctx.stack[len(ctx.stack)-1] = boxed
	return true
}

// refEq pops two boxed refs and pushes BoxI32(1) if they are the same
// reference (identical bit-pattern), BoxI32(0) otherwise.
func (l arm64JIT) refEq(ctx *jitContext) bool {
	if !l.need(ctx, 2) {
		return false
	}
	b := ctx.stack[len(ctx.stack)-1]
	a := ctx.stack[len(ctx.stack)-2]

	ctx.assembler.Emit(arm64.CMP(a, b))

	flag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.CSET(flag, arm64.CondEQ))

	boxed := l.boxI32(ctx, flag)
	ctx.stack = append(ctx.stack[:len(ctx.stack)-2], boxed)
	return true
}

// br lowers an unconditional branch. In blocks mode it emits a direct
// BLabel to the target; otherwise no instructions are emitted and Exit
// writes the target IP to scratch.
func (arm64JIT) br(ctx *jitContext) bool {
	offset := int(int16(binary.LittleEndian.Uint16(ctx.code[ctx.ip+1 : ctx.ip+3])))
	target := ctx.ip + 3 + offset
	if ctx.labels != nil {
		lbl, ok := ctx.labels[target]
		if !ok {
			return false
		}
		ctx.assembler.Emit(arm64.BLabel(lbl))
		ctx.ip = target
		ctx.stop = true
		ctx.closed = true
		return true
	}
	ctx.ip = target
	ctx.successor = ctx.ip
	ctx.stop = true
	return true
}

// brIf lowers a conditional branch. In blocks mode (ctx.labels != nil) it
// emits a single CBNZ to the taken label and falls through to the false
// target, closing the block without interpreter exits. In segment mode
// it emits two inline exit paths (false-target and taken-target), each
// writing the appropriate nextIP to scratch and RET-ing.
func (l arm64JIT) brIf(ctx *jitContext) bool {
	if !l.need(ctx, 1) {
		return false
	}
	offset := int(int16(binary.LittleEndian.Uint16(ctx.code[ctx.ip+1 : ctx.ip+3])))
	falseTarget := ctx.ip + 3
	takenTarget := ctx.ip + 3 + offset

	cond := ctx.stack[len(ctx.stack)-1]
	ctx.stack = ctx.stack[:len(ctx.stack)-1]

	// Extract i32 value lane from the boxed condition.
	condI32 := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(condI32, cond, maskI32))

	if ctx.labels != nil {
		// Blocks mode: emit intra-function conditional branch. Both targets
		// must be known block starts; fall back to segment mode if not.
		takenLbl, ok := ctx.labels[takenTarget]
		if !ok {
			return false
		}
		ctx.assembler.Emit(arm64.CBNZLabel(condI32, takenLbl))
		// Fall through to falseTarget — no interpreter exit needed.
		ctx.ip = falseTarget
		ctx.stop = true
		ctx.closed = true
		return true
	}

	// Segment mode: materialize remaining stack once; both exits share it.
	nextSP := l.materialize(ctx)
	vSP := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(vSP, ctx.scratch[scratchSP])
	ctx.assembler.Emit(arm64.MOV(vSP, nextSP))

	rNext := ctx.scratch[scratchNext]
	takenLbl := ctx.assembler.Label()
	ctx.assembler.Emit(arm64.CBNZLabel(condI32, takenLbl))

	// Fall-through path: condition was zero.
	vNextFalse := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(vNextFalse, rNext)
	ctx.assembler.Emit(arm64.LDI(vNextFalse, uint64(falseTarget))...)
	ctx.assembler.Emit(arm64.RET())

	// Taken path: condition was non-zero.
	ctx.assembler.Bind(takenLbl)
	vNextTaken := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(vNextTaken, rNext)
	ctx.assembler.Emit(arm64.LDI(vNextTaken, uint64(takenTarget))...)
	ctx.assembler.Emit(arm64.RET())

	// Chain the fall-through as the proactive successor, mirroring br. The
	// taken target is only compiled if independently hot; both exits already
	// RET to the interpreter, so this only widens segment coverage.
	ctx.ip = falseTarget
	ctx.successor = ctx.ip
	ctx.stop = true
	ctx.closed = true
	return true
}

// brTable lowers BR_TABLE. It pops a boxed i32 condition, clamps it to
// [0..count], and emits a linear scan of CMPI+B.EQ pairs — one per case — that
// each jump to an inline exit path. The default exit falls through below the
// scan. Every exit materializes the remaining shadow stack, writes SP and the
// compile-time target IP through scratch, and RETs.
//
// In blocks mode (ctx.labels != nil) label-based dispatch is not yet
// implemented, so brTable rejects and blocks() falls back to segments().
func (l arm64JIT) brTable(ctx *jitContext) bool {
	if ctx.labels != nil {
		return false
	}
	if !l.need(ctx, 1) {
		return false
	}

	count := int(ctx.code[ctx.ip+1])
	width := count*2 + 4

	targets := make([]int, count+1)
	for i := range targets {
		at := ctx.ip + 2 + i*2
		offset := int(int16(binary.LittleEndian.Uint16(ctx.code[at : at+2])))
		targets[i] = ctx.ip + width + offset
	}

	cond := ctx.stack[len(ctx.stack)-1]
	ctx.stack = ctx.stack[:len(ctx.stack)-1]

	// Extract unsigned i32 value lane (negative i32s become large unsigned
	// values and fall through to the default).
	condI32 := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(condI32, cond, maskI32))

	// Materialize remaining shadow stack once — all exit paths share it.
	nextSP := l.materialize(ctx)
	vSP := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(vSP, ctx.scratch[scratchSP])
	ctx.assembler.Emit(arm64.MOV(vSP, nextSP))

	rNext := ctx.scratch[scratchNext]

	// Emit one CMPI+B.EQ per case.
	labels := make([]asm.Label, count)
	for i := range labels {
		labels[i] = ctx.assembler.Label()
		ctx.assembler.Emit(arm64.CMPI(condI32, uint16(i)))
		ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, labels[i]))
	}

	// Default exit (fall-through when no case matched).
	vNextDef := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(vNextDef, rNext)
	ctx.assembler.Emit(arm64.LDI(vNextDef, uint64(targets[count]))...)
	ctx.assembler.Emit(arm64.RET())

	// Per-case exits.
	for i := 0; i < count; i++ {
		ctx.assembler.Bind(labels[i])
		vNext := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		_ = ctx.assembler.Pin(vNext, rNext)
		ctx.assembler.Emit(arm64.LDI(vNext, uint64(targets[i]))...)
		ctx.assembler.Emit(arm64.RET())
	}

	ctx.stop = true
	ctx.closed = true
	ctx.successor = targets[count]
	return true
}

// f32Binary lowers an F32 binary arithmetic opcode. Both boxed-f32 inputs are
// unboxed to float32 registers, the operation is performed, and the result is
// reboxed as a boxed f32.
func (l arm64JIT) f32Binary(ctx *jitContext, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	if !l.need(ctx, 2) {
		return false
	}
	b := ctx.stack[len(ctx.stack)-1]
	a := ctx.stack[len(ctx.stack)-2]

	fa := l.unboxF32(ctx, a)
	fb := l.unboxF32(ctx, b)
	fr := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	ctx.assembler.Emit(op(fr, fa, fb))

	boxed := l.reboxF32(ctx, fr)
	ctx.stack = append(ctx.stack[:len(ctx.stack)-2], boxed)
	return true
}

// f64Binary lowers an F64 binary arithmetic opcode. Both boxed-f64 inputs are
// unboxed to float64 registers, the operation is performed, and the result is
// reboxed as a boxed f64.
func (l arm64JIT) f64Binary(ctx *jitContext, op func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	if !l.need(ctx, 2) {
		return false
	}
	b := ctx.stack[len(ctx.stack)-1]
	a := ctx.stack[len(ctx.stack)-2]

	fa := l.unboxF64(ctx, a)
	fb := l.unboxF64(ctx, b)
	fr := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	ctx.assembler.Emit(op(fr, fa, fb))

	boxed := l.reboxF64(ctx, fr)
	ctx.stack = append(ctx.stack[:len(ctx.stack)-2], boxed)
	return true
}

// f32Cmp pops two boxed f32 values, runs FCMP on them, and pushes a boxed i32
// 0/1 from the chosen condition code. NaN comparisons are unordered; EQ/NE
// may not fully honour WebAssembly NaN semantics in Phase A.
func (l arm64JIT) f32Cmp(ctx *jitContext, cond uint8) bool {
	if !l.need(ctx, 2) {
		return false
	}
	b := ctx.stack[len(ctx.stack)-1]
	a := ctx.stack[len(ctx.stack)-2]

	fa := l.unboxF32(ctx, a)
	fb := l.unboxF32(ctx, b)
	ctx.assembler.Emit(arm64.FCMP(fa, fb))

	flag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.CSET(flag, cond))

	boxed := l.boxI32(ctx, flag)
	ctx.stack = append(ctx.stack[:len(ctx.stack)-2], boxed)
	return true
}

// f64Cmp pops two boxed f64 values, runs FCMP on them, and pushes a boxed i32
// 0/1 from the chosen condition code.
func (l arm64JIT) f64Cmp(ctx *jitContext, cond uint8) bool {
	if !l.need(ctx, 2) {
		return false
	}
	b := ctx.stack[len(ctx.stack)-1]
	a := ctx.stack[len(ctx.stack)-2]

	fa := l.unboxF64(ctx, a)
	fb := l.unboxF64(ctx, b)
	ctx.assembler.Emit(arm64.FCMP(fa, fb))

	flag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.CSET(flag, cond))

	boxed := l.boxI32(ctx, flag)
	ctx.stack = append(ctx.stack[:len(ctx.stack)-2], boxed)
	return true
}

// toFloat pops one boxed integer value, extracts its value lane via prep,
// converts it to a float of fWidth using cvtf (SCVTF or UCVTF), then boxes
// the result as f32 (Width32) or f64 (Width64).
func (l arm64JIT) toFloat(
	ctx *jitContext,
	fWidth asm.RegWidth,
	cvtf func(dst, src asm.Reg) asm.Instruction,
	prep func(*jitContext, asm.VReg) asm.VReg,
) bool {
	if !l.need(ctx, 1) {
		return false
	}
	a := ctx.stack[len(ctx.stack)-1]
	val := prep(ctx, a)
	fr := ctx.assembler.Reg(asm.RegTypeFloat, fWidth)
	ctx.assembler.Emit(cvtf(fr, val))

	var boxed asm.VReg
	if fWidth == asm.Width32 {
		boxed = l.reboxF32(ctx, fr)
	} else {
		boxed = l.reboxF64(ctx, fr)
	}
	ctx.stack[len(ctx.stack)-1] = boxed
	return true
}

// f32ToF64 pops a boxed f32, converts its float32 value to float64 via
// FCVT, and pushes the result as a boxed f64.
func (l arm64JIT) f32ToF64(ctx *jitContext) bool {
	if !l.need(ctx, 1) {
		return false
	}
	a := ctx.stack[len(ctx.stack)-1]
	fa := l.unboxF32(ctx, a)
	fd := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	ctx.assembler.Emit(arm64.FCVT(fd, fa))
	boxed := l.reboxF64(ctx, fd)
	ctx.stack[len(ctx.stack)-1] = boxed
	return true
}

// f64ToF32 pops a boxed f64, converts its float64 value to float32 via
// FCVT, and pushes the result as a boxed f32.
func (l arm64JIT) f64ToF32(ctx *jitContext) bool {
	if !l.need(ctx, 1) {
		return false
	}
	a := ctx.stack[len(ctx.stack)-1]
	fa := l.unboxF64(ctx, a)
	fs := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	ctx.assembler.Emit(arm64.FCVT(fs, fa))
	boxed := l.reboxF32(ctx, fs)
	ctx.stack[len(ctx.stack)-1] = boxed
	return true
}

// unboxF32 extracts the float32 bit pattern from a boxed f32 (tagF32 | bits)
// by masking to the low 32 bits and issuing FMOV with a Width64 int source
// (the encoder uses the physical W alias, i.e. the low 32 bits of the X register).
func (l arm64JIT) unboxF32(ctx *jitContext, v asm.VReg) asm.VReg {
	bits := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(bits, v, maskI32))
	f := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width32)
	ctx.assembler.Emit(arm64.FMOV(f, bits))
	return f
}

// reboxF32 boxes a float32 register back to a boxed f32 value. FMOV with a
// Width64 int destination zero-extends the float32 bits to 64 bits, then
// tagF32 is OR-ed in.
func (l arm64JIT) reboxF32(ctx *jitContext, f asm.VReg) asm.VReg {
	bits := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(bits, f))
	tag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(tag, tagF32)...)
	boxed := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ORR(boxed, bits, tag))
	return boxed
}

// unboxF64 interprets the raw bits of a boxed f64 (stored as IEEE 754 float64
// bits) as a Float64 register via FMOV.
func (arm64JIT) unboxF64(ctx *jitContext, v asm.VReg) asm.VReg {
	f := ctx.assembler.Reg(asm.RegTypeFloat, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(f, v))
	return f
}

// reboxF64 packs a Float64 register back to a boxed f64 by moving the raw
// bits into an Int64 register via FMOV. BoxF64 stores the raw float64 bits
// directly, so no tag OR is needed.
func (arm64JIT) reboxF64(ctx *jitContext, f asm.VReg) asm.VReg {
	bits := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.FMOV(bits, f))
	return bits
}

func (arm64JIT) need(ctx *jitContext, n int) bool {
	missing := n - len(ctx.stack)
	if missing <= 0 {
		return true
	}
	if ctx.whole {
		return false
	}

	inputs := make([]asm.VReg, missing)
	vStack := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(vStack, ctx.scratch[scratchStack])
	vSP := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = ctx.assembler.Pin(vSP, ctx.scratch[scratchSP])
	for i := range inputs {
		idx := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		back := missing + len(ctx.stack) - i
		ctx.assembler.Emit(arm64.SUBI(idx, vSP, uint16(back)))
		v := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		(arm64JIT{}).load(ctx, v, vStack, idx)
		inputs[i] = v
	}
	ctx.inputs = append(inputs, ctx.inputs...)
	ctx.stack = append(inputs, ctx.stack...)
	return true
}

func (arm64JIT) global(ctx *jitContext) (int, bool) {
	idx := int(uint16(ctx.code[ctx.ip+1]) | uint16(ctx.code[ctx.ip+2])<<8)
	// ctx.globals is the slice the interpreter held at compile time. Bounding
	// idx against its length is safe: execution is single-threaded, so the
	// globals slice cannot change underneath a running native segment, and a
	// segment only runs along the control flow that already populated those
	// globals. The runtime base pointer is re-read from &i.globals[0] on each
	// invocation (see scratch), so it tolerates a slice that was reallocated
	// since compilation.
	if idx >= len(ctx.globals) {
		return 0, false
	}
	if ctx.globals[idx].Kind() == types.KindRef {
		return 0, false
	}
	// LDR/STR unsigned-offset encodes at most 12 bits (0..4095 slots x 8 bytes).
	if idx > 4095 {
		return 0, false
	}
	return idx, true
}

func (l arm64JIT) guardRef(ctx *jitContext, v asm.VReg, pre []asm.VReg) {
	tag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LSRI(tag, v, uint8(types.VBits)))
	want := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(want, tagRef>>types.VBits)...)
	ctx.assembler.Emit(arm64.CMP(tag, want))

	ok := ctx.assembler.Label()
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBNE, ok))
	ctx.stack = append(ctx.stack[:0], pre...)
	l.exitFallback(ctx, ctx.ip)
	ctx.assembler.Bind(ok)
	ctx.stack = append(ctx.stack[:0], pre...)
}
