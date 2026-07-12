//go:build arm64

package interp

import (
	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

const cfgTableLimit = 32

// lowerCFG lowers every basic block with canonical VM stack state at each edge.
// Unsupported instructions end their block with an exact-IP fallback instead
// of rejecting the whole function.
func (l arm64Lowerer) lowerCFG(ctx *lowering, blocks []*analysis.BasicBlock, kinds [][]types.Kind, labels []asm.Label) bool {
	if len(blocks) == 0 || len(blocks) != len(kinds) || len(blocks) != len(labels) {
		return false
	}
	l.enter(ctx)
	index := make(map[int]int, len(blocks))
	for idx, block := range blocks {
		index[block.Start] = idx
	}

	for idx, block := range blocks {
		ctx.assembler.Bind(labels[idx])
		l.cfgEntry(ctx, kinds[idx])
		if !l.cfgBlock(ctx, block, labels, index) {
			return false
		}
	}
	for _, exit := range ctx.exits {
		ctx.values = exit.values
		ctx.frames = exit.frames
		ctx.assembler.Bind(exit.label)
		if exit.retain > 0 {
			l.retain(ctx, exit.retain)
		}
		l.trapFlushed(ctx, trapFallback, exit.resume)
	}
	return true
}

func (l arm64Lowerer) cfgEntry(ctx *lowering, kinds []types.Kind) {
	ctx.values = ctx.values[:0]
	for _, kind := range kinds {
		ctx.values = append(ctx.values, value{kind: kind})
	}
	f := ctx.frame()
	clear(f.loaded)
	clear(f.dirty)
	l.reload(ctx)
}

func (l arm64Lowerer) cfgBlock(ctx *lowering, block *analysis.BasicBlock, labels []asm.Label, index map[int]int) bool {
	f := ctx.frame()
	for ip := block.Start; ip < block.End; {
		inst := instr.Instruction(f.code[ip:])
		op := step{op: inst.Opcode(), fn: ctx.addr, ip: ip}
		next := ip + inst.Width()

		if op.op == instr.CONST_GET && next < block.End && instr.Opcode(f.code[next]) == instr.CALL {
			callee, ok := l.cfgTarget(ctx, inst)
			if !ok {
				return l.exit(ctx, ip)
			}
			call := step{op: instr.CALL, fn: ctx.addr, ip: next, callee: callee}
			if !l.cfgCall(ctx, call) {
				return false
			}
			ip = next + 1
			continue
		}

		switch op.op {
		case instr.BR:
			if !l.flush(ctx, false) {
				return false
			}
			return l.cfgEdge(ctx, instr.Targets(f.code, ip)[0], block.Start, labels, index)
		case instr.BR_IF:
			return l.cfgIf(ctx, op, block.Start, next, labels, index)
		case instr.BR_TABLE:
			return l.cfgTable(ctx, op, block.Start, labels, index)
		case instr.RETURN:
			return l.ret(ctx)
		}

		ok, terminal := l.cfgOp(ctx, op)
		if !ok {
			return false
		}
		if terminal {
			return true
		}
		ip = next
	}

	if !l.flush(ctx, false) {
		return false
	}
	return l.cfgEdge(ctx, block.End, block.Start, labels, index)
}

func (l arm64Lowerer) cfgOp(ctx *lowering, op step) (bool, bool) {
	ok := false
	switch op.op {
	case instr.NOP:
		ok = true
	case instr.UNREACHABLE:
		return l.exit(ctx, op.ip), true
	case instr.I32_CONST:
		ok = l.i32Const(ctx, op)
	case instr.I64_CONST:
		ok = l.i64Const(ctx, op)
	case instr.F32_CONST:
		ok = l.f32Const(ctx, op)
	case instr.F64_CONST:
		ok = l.f64Const(ctx, op)
	case instr.CONST_GET:
		ok = l.cfgConstGet(ctx, op)
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
		ok = l.cfgArrayGet(ctx, op)
	default:
		return l.exit(ctx, op.ip), true
	}
	return ok, false
}

func (l arm64Lowerer) cfgConstGet(ctx *lowering, op step) bool {
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

func (l arm64Lowerer) cfgArrayGet(ctx *lowering, op step) bool {
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
	label := ctx.assembler.Label()
	values, frames := ctx.snapshot()
	ctx.exits = append(ctx.exits, sideExit{label: label, values: values, frames: frames, resume: resume, retain: retain})
	return label
}

func (l arm64Lowerer) cfgTarget(ctx *lowering, inst instr.Instruction) (int, bool) {
	idx := int(inst.Operand(0))
	if idx >= len(ctx.constants) || ctx.constants[idx].Kind() != types.KindRef {
		return 0, false
	}
	ref := ctx.constants[idx].Ref()
	if ref <= 0 || ref >= len(ctx.heap) {
		return 0, false
	}
	fn, ok := ctx.heap[ref].(*types.Function)
	if !ok || fn.Typ == nil || len(fn.Captures) != 0 {
		return 0, false
	}
	ctx.push(value{kind: types.KindRef, raw: true, fn: ref, ref: ref})
	return ref, true
}

func (l arm64Lowerer) cfgCall(ctx *lowering, op step) bool {
	target := ctx.funcs[op.callee]
	if op.callee == ctx.addr || target == nil || target.Typ == nil || ctx.count() < 1 {
		return false
	}
	params := len(target.Typ.Params)
	if ctx.count() < params+1 {
		return false
	}
	for _, typ := range target.Typ.Returns {
		switch typ.Kind() {
		case types.KindI1, types.KindI8, types.KindI32, types.KindI64, types.KindF32, types.KindF64:
		default:
			return false
		}
	}

	a := ctx.assembler
	vCtrl := ctx.pin(scratchCtrl)
	natives := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(natives, vCtrl, int16(journalNatives*8)))
	callee := a.Reg(asm.RegTypeInt, asm.Width64)
	if op.callee > 4095 {
		return false
	}
	a.Emit(arm64.LDR(callee, natives, int16(op.callee*8)))
	ready := a.Label()
	a.Emit(arm64.CBNZLabel(callee, ready))
	if !l.exit(ctx, op.ip) {
		return false
	}
	a.Bind(ready)

	marker := ctx.pop()
	if marker.fn != op.callee || ctx.count() < params || !l.args(ctx, target, params) {
		return false
	}
	if !l.flush(ctx, false) {
		return false
	}

	active := ctx.pinTo(arm64.X15)
	limit := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(limit, vCtrl, int16(journalCap*8)))
	a.Emit(arm64.CMP(active, limit))
	hasFrame := a.Label()
	a.Emit(arm64.BCondLabel(arm64.OpBCC, hasFrame))
	l.overflow(ctx, op)
	a.Bind(hasFrame)
	a.Emit(arm64.ADDI(active, active, 1))
	a.Emit(arm64.STR(active, vCtrl, int16(journalActive*8)))

	vBP := ctx.pin(scratchBP)
	nextSP := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADDI(nextSP, vBP, uint16(ctx.sp())))
	oldBP := ctx.scratch[scratchBP]
	oldSP := ctx.scratch[scratchSP]
	a.Emit(
		arm64.SUBI(arm64.SP, arm64.SP, 32),
		arm64.STP(oldBP, oldSP, arm64.SP, 0),
		arm64.STR(arm64.LR, arm64.SP, 16),
	)
	calleeBP := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.SUBI(calleeBP, nextSP, uint16(params)))
	a.Emit(arm64.MOV(ctx.pinTo(oldBP), calleeBP))

	localKinds := target.LocalKinds()
	if len(localKinds) > params {
		stack := ctx.pin(scratchStack)
		base := l.base(ctx, stack)
		for idx := params; idx < len(localKinds); idx++ {
			zero, ok := cfgZero(localKinds[idx])
			if !ok {
				return false
			}
			reg := a.Reg(asm.RegTypeInt, asm.Width64)
			a.Emit(arm64.LDI(reg, uint64(zero))...)
			a.Emit(arm64.STR(reg, base, int16((ctx.sp()-params+idx)*8)))
		}
	}
	calleeSP := calleeBP
	if len(localKinds) > 0 {
		calleeSP = a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ADDI(calleeSP, calleeBP, uint16(len(localKinds))))
	}
	a.Emit(arm64.MOV(ctx.pinTo(oldSP), calleeSP))
	a.Emit(arm64.MOV(arm64.X0, vCtrl))
	a.Emit(arm64.BLR(callee))

	vCtrl = ctx.pin(scratchCtrl)
	trap := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(trap, vCtrl, int16(journalTrap*8)))
	normal := a.Label()
	a.Emit(arm64.CBZLabel(trap, normal), arm64.LDR(oldBP, arm64.SP, 0))
	l.unwind(ctx, vCtrl, op.ip+1)
	a.Emit(
		arm64.LDR(arm64.LR, arm64.SP, 16),
		arm64.ADDI(arm64.SP, arm64.SP, 32),
		arm64.RET(),
	)
	a.Bind(normal)

	active = ctx.pinTo(arm64.X15)
	a.Emit(arm64.SUBI(active, active, 1))
	a.Emit(arm64.STR(active, vCtrl, int16(journalActive*8)))
	a.Emit(
		arm64.LDP(oldBP, oldSP, arm64.SP, 0),
		arm64.LDR(arm64.LR, arm64.SP, 16),
		arm64.ADDI(arm64.SP, arm64.SP, 32),
	)

	rets := target.Typ.Returns
	regs := make([]asm.VReg, len(rets))
	for idx := range rets {
		if idx >= len(arm64.IntRets) {
			return false
		}
		regs[idx] = ctx.pinTo(arm64.IntRets[idx])
	}
	ctx.values = ctx.values[:len(ctx.values)-params]
	for fi := range ctx.frames {
		clear(ctx.frames[fi].loaded)
		clear(ctx.frames[fi].dirty)
	}
	l.reload(ctx)
	for idx, typ := range rets {
		ctx.push(value{reg: regs[idx], kind: typ.Kind(), raw: true})
	}
	return true
}

func cfgZero(kind types.Kind) (types.Boxed, bool) {
	switch kind {
	case types.KindI1:
		return types.BoxI1(false), true
	case types.KindI8:
		return types.BoxI8(0), true
	case types.KindI32:
		return types.BoxI32(0), true
	case types.KindI64:
		return types.BoxI64(0), true
	case types.KindF32:
		return types.BoxF32(0), true
	case types.KindF64:
		return types.BoxF64(0), true
	case types.KindRef:
		return types.BoxedNull, true
	default:
		return 0, false
	}
}

func (l arm64Lowerer) cfgIf(ctx *lowering, op step, from, nextIP int, labels []asm.Label, index map[int]int) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI32, 1) {
		return false
	}
	cond := ctx.pop()
	if !l.flush(ctx, false) {
		return false
	}
	targets := instr.Targets(ctx.frame().code, op.ip)
	if len(targets) != 1 {
		return false
	}
	taken := ctx.assembler.Label()
	ctx.assembler.Emit(arm64.CBNZLabel(l.narrow32(cond.reg), taken))
	if !l.cfgEdge(ctx, nextIP, from, labels, index) {
		return false
	}
	ctx.assembler.Bind(taken)
	return l.cfgEdge(ctx, targets[0], from, labels, index)
}

func (l arm64Lowerer) cfgTable(ctx *lowering, op step, from int, labels []asm.Label, index map[int]int) bool {
	if ctx.count() < 1 || !l.kinds(ctx, types.KindI32, 1) {
		return false
	}
	code := ctx.frame().code
	count := int(code[op.ip+1])
	if count > cfgTableLimit {
		return false
	}
	targets := instr.Targets(code, op.ip)
	if len(targets) != count+1 {
		return false
	}
	cond := ctx.pop()
	cond32 := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(cond32, cond.reg, maskI32))
	if !l.flush(ctx, false) {
		return false
	}
	paths := make([]asm.Label, len(targets))
	for idx := range paths {
		paths[idx] = ctx.assembler.Label()
	}
	for idx := 0; idx < count; idx++ {
		ctx.assembler.Emit(arm64.CMPI(cond32, uint16(idx)))
		ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, paths[idx]))
	}
	ctx.assembler.Emit(arm64.BLabel(paths[count]))
	for idx, label := range paths {
		ctx.assembler.Bind(label)
		if !l.cfgEdge(ctx, targets[idx], from, labels, index) {
			return false
		}
	}
	return true
}

func (l arm64Lowerer) cfgEdge(ctx *lowering, target, from int, labels []asm.Label, index map[int]int) bool {
	if target == len(ctx.frame().code) {
		if ctx.addr == 0 {
			return l.complete(ctx)
		}
		return l.ret(ctx)
	}
	idx, ok := index[target]
	if !ok {
		return false
	}
	if target > from {
		ctx.assembler.Emit(arm64.BLabel(labels[idx]))
		return true
	}

	a := ctx.assembler
	vCtrl := ctx.pin(scratchCtrl)
	budget := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(budget, vCtrl, int16(journalBudget*8)))
	a.Emit(arm64.SUBI(budget, budget, 1))
	a.Emit(arm64.STR(budget, vCtrl, int16(journalBudget*8)))
	a.Emit(arm64.CBNZLabel(budget, labels[idx]))
	l.trapFlushed(ctx, trapYield, target)
	return true
}
