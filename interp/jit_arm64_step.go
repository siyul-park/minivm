//go:build arm64

package interp

import (
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

func (l arm64Lowerer) emitStep(ctx *lowering, op step, static bool) (bool, bool) {
	ok := false
	switch op.op {
	case instr.I32_CONST:
		ok = l.i32Const(ctx, op)
	case instr.I64_CONST:
		ok = l.i64Const(ctx, op)
	case instr.F32_CONST:
		ok = l.f32Const(ctx, op)
	case instr.F64_CONST:
		ok = l.f64Const(ctx, op)
	case instr.CONST_GET:
		if static {
			ok = l.constGetKnown(ctx, op)
		} else {
			ok = l.constGet(ctx, op)
		}
	case instr.LOCAL_GET:
		ok = l.localGet(ctx, op)
	case instr.LOCAL_SET:
		ok = l.localSet(ctx, op, true)
	case instr.LOCAL_TEE:
		ok = l.localSet(ctx, op, false)
	case instr.GLOBAL_GET:
		ok = l.globalGet(ctx, op)
	case instr.GLOBAL_SET:
		ok = l.globalSet(ctx, op, true)
	case instr.GLOBAL_TEE:
		ok = l.globalSet(ctx, op, false)
	case instr.DROP:
		ok = l.drop(ctx, op)
	case instr.DUP:
		ok = l.dup(ctx)
	case instr.SWAP:
		ok = l.swap(ctx)
	case instr.SELECT:
		ok = l.selectOp(ctx)
	case instr.I32_ADD:
		ok = l.i32Binary(ctx, arm64.ADD)
	case instr.I32_SUB:
		ok = l.i32Binary(ctx, arm64.SUB)
	case instr.I32_MUL:
		ok = l.i32Binary(ctx, arm64.MUL)
	case instr.I32_AND:
		ok = l.i32Bitwise(ctx, arm64.AND)
	case instr.I32_OR:
		ok = l.i32Bitwise(ctx, arm64.ORR)
	case instr.I32_XOR:
		ok = l.i32Bitwise(ctx, arm64.EOR)
	case instr.I32_EQZ:
		ok = l.i32Eqz(ctx)
	case instr.I32_EQ:
		ok = l.i32Cmp(ctx, arm64.CondEQ)
	case instr.I32_NE:
		ok = l.i32Cmp(ctx, arm64.CondNE)
	case instr.I32_LT_S:
		ok = l.i32Cmp(ctx, arm64.CondLT)
	case instr.I32_LE_S:
		ok = l.i32Cmp(ctx, arm64.CondLE)
	case instr.I32_GT_S:
		ok = l.i32Cmp(ctx, arm64.CondGT)
	case instr.I32_GE_S:
		ok = l.i32Cmp(ctx, arm64.CondGE)
	case instr.I32_LT_U:
		ok = l.i32Cmp(ctx, arm64.CondCC)
	case instr.I32_LE_U:
		ok = l.i32Cmp(ctx, arm64.CondLS)
	case instr.I32_GT_U:
		ok = l.i32Cmp(ctx, arm64.CondHI)
	case instr.I32_GE_U:
		ok = l.i32Cmp(ctx, arm64.CondCS)
	case instr.I64_ADD:
		ok = l.i64Binary(ctx, op, arm64.ADD, true)
	case instr.I64_SUB:
		ok = l.i64Binary(ctx, op, arm64.SUB, true)
	case instr.I64_MUL:
		ok = l.i64Binary(ctx, op, arm64.MUL, true)
	case instr.I64_AND:
		ok = l.i64Binary(ctx, op, arm64.AND, false)
	case instr.I64_OR:
		ok = l.i64Binary(ctx, op, arm64.ORR, false)
	case instr.I64_XOR:
		ok = l.i64Binary(ctx, op, arm64.EOR, false)
	case instr.I64_EQZ:
		ok = l.i64Eqz(ctx)
	case instr.I64_EQ:
		ok = l.i64Cmp(ctx, arm64.CondEQ)
	case instr.I64_NE:
		ok = l.i64Cmp(ctx, arm64.CondNE)
	case instr.I64_LT_S:
		ok = l.i64Cmp(ctx, arm64.CondLT)
	case instr.I64_LE_S:
		ok = l.i64Cmp(ctx, arm64.CondLE)
	case instr.I64_GT_S:
		ok = l.i64Cmp(ctx, arm64.CondGT)
	case instr.I64_GE_S:
		ok = l.i64Cmp(ctx, arm64.CondGE)
	case instr.I64_LT_U:
		ok = l.i64Cmp(ctx, arm64.CondCC)
	case instr.I64_LE_U:
		ok = l.i64Cmp(ctx, arm64.CondLS)
	case instr.I64_GT_U:
		ok = l.i64Cmp(ctx, arm64.CondHI)
	case instr.I64_GE_U:
		ok = l.i64Cmp(ctx, arm64.CondCS)
	case instr.F32_ADD:
		ok = l.f32Binary(ctx, arm64.FADD)
	case instr.F32_SUB:
		ok = l.f32Binary(ctx, arm64.FSUB)
	case instr.F32_MUL:
		ok = l.f32Binary(ctx, arm64.FMUL)
	case instr.F32_DIV:
		ok = l.f32Binary(ctx, arm64.FDIV)
	case instr.F32_EQ:
		ok = l.f32Cmp(ctx, arm64.CondEQ)
	case instr.F32_NE:
		ok = l.f32Cmp(ctx, arm64.CondNE)
	case instr.F32_LT:
		ok = l.f32Cmp(ctx, arm64.CondMI)
	case instr.F32_GT:
		ok = l.f32Cmp(ctx, arm64.CondGT)
	case instr.F32_LE:
		ok = l.f32Cmp(ctx, arm64.CondLS)
	case instr.F32_GE:
		ok = l.f32Cmp(ctx, arm64.CondGE)
	case instr.F64_ADD:
		ok = l.f64Binary(ctx, arm64.FADD)
	case instr.F64_SUB:
		ok = l.f64Binary(ctx, arm64.FSUB)
	case instr.F64_MUL:
		ok = l.f64Binary(ctx, arm64.FMUL)
	case instr.F64_DIV:
		ok = l.f64Binary(ctx, arm64.FDIV)
	case instr.F64_EQ:
		ok = l.f64Cmp(ctx, arm64.CondEQ)
	case instr.F64_NE:
		ok = l.f64Cmp(ctx, arm64.CondNE)
	case instr.F64_LT:
		ok = l.f64Cmp(ctx, arm64.CondMI)
	case instr.F64_GT:
		ok = l.f64Cmp(ctx, arm64.CondGT)
	case instr.F64_LE:
		ok = l.f64Cmp(ctx, arm64.CondLS)
	case instr.F64_GE:
		ok = l.f64Cmp(ctx, arm64.CondGE)
	case instr.ARRAY_GET:
		if static {
			ok = l.arrayGetKnown(ctx, op)
		} else {
			ok = l.arrayGet(ctx, op)
		}
	default:
		return false, false
	}
	return true, ok
}

func (l arm64Lowerer) constGetKnown(ctx *lowering, op step) bool {
	idx := int(instr.Instruction(ctx.frame().code[op.ip:]).Operand(0))
	if idx >= len(ctx.constants) {
		return false
	}
	boxed := ctx.constants[idx]
	if boxed.Kind() != types.KindRef {
		return l.constGet(ctx, op)
	}
	ref := boxed.Ref()
	if ref <= 0 || ref >= len(ctx.heap) {
		return false
	}
	switch ctx.heap[ref].(type) {
	case types.TypedArray[bool], types.TypedArray[int8], types.TypedArray[int32],
		types.TypedArray[float32], types.TypedArray[float64]:
		ctx.push(value{kind: types.KindRef, raw: true, ref: ref})
		return true
	default:
		return l.constGet(ctx, op)
	}
}

func (l arm64Lowerer) arrayGetKnown(ctx *lowering, op step) bool {
	if ctx.count() < 2 || ctx.values[len(ctx.values)-1].kind != types.KindI32 {
		return false
	}
	marker := ctx.values[len(ctx.values)-2]
	constant := marker.ref
	if !marker.raw || constant <= 0 || constant >= len(ctx.heap) {
		return false
	}

	var kind types.Kind
	var want uintptr
	var scale uint8
	switch value := ctx.heap[constant].(type) {
	case types.TypedArray[bool]:
		kind, want = types.KindI1, itab(value)
	case types.TypedArray[int8]:
		kind, want = types.KindI8, itab(value)
	case types.TypedArray[int32]:
		kind, want, scale = types.KindI32, itab(value), 2
	case types.TypedArray[float32]:
		kind, want, scale = types.KindF32, itab(value), 2
	case types.TypedArray[float64]:
		kind, want, scale = types.KindF64, itab(value), 3
	default:
		return false
	}

	pre := append([]value(nil), ctx.values...)
	boxed := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(boxed, uint64(types.BoxRef(constant)))...)
	ctx.values[len(ctx.values)-2] = value{reg: boxed, kind: types.KindRef, raw: false, ref: constant}
	if !l.flush(ctx, false) {
		return false
	}
	clear(ctx.frame().loaded)
	clear(ctx.frame().dirty)
	fail := l.cfgExit(ctx, op.ip, constant)

	a := ctx.assembler
	heap := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(heap, ctx.pin(scratchCtrl), int16(journalHeap*8)))
	off := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(off, uint64(constant))...)
	a.Emit(arm64.LSLI(off, off, 4))
	cell := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADD(cell, heap, off))
	actual := a.Reg(asm.RegTypeInt, asm.Width64)
	data := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(actual, cell, 0), arm64.LDR(data, cell, 8))
	l.guardItab(ctx, actual, want, fail)

	idx := l.sign32(ctx, ctx.values[len(ctx.values)-1].reg)
	dataPtr, n := l.sliceHeader(ctx, data, 0)
	l.guardIndex(ctx, idx, n, fail)
	result := a.Reg(asm.RegTypeInt, asm.Width64)
	switch kind {
	case types.KindI1:
		elemAddr := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ADD(elemAddr, dataPtr, idx))
		a.Emit(arm64.LDRB(result, elemAddr, 0))
	case types.KindI8:
		elemAddr := a.Reg(asm.RegTypeInt, asm.Width64)
		elem := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ADD(elemAddr, dataPtr, idx))
		a.Emit(arm64.LDRB(elem, elemAddr, 0))
		a.Emit(arm64.SXTB(result, elem))
	case types.KindI32, types.KindF32:
		elemOff := a.Reg(asm.RegTypeInt, asm.Width64)
		elemAddr := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LSLI(elemOff, idx, scale))
		a.Emit(arm64.ADD(elemAddr, dataPtr, elemOff))
		a.Emit(arm64.LDRSW(result, elemAddr, 0))
	case types.KindF64:
		elemOff := a.Reg(asm.RegTypeInt, asm.Width64)
		elemAddr := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LSLI(elemOff, idx, scale))
		a.Emit(arm64.ADD(elemAddr, dataPtr, elemOff))
		a.Emit(arm64.LDR(result, elemAddr, 0))
	}
	ctx.values = append(pre[:len(pre)-2:len(pre)-2], value{reg: result, kind: kind, raw: true})
	return true
}

func (l arm64Lowerer) cfgExit(ctx *lowering, resume, retain int) asm.Label {
	return ctx.queueExit(nil, resume, retain)
}
