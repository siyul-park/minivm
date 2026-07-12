package interp

import (
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
	"github.com/siyul-park/minivm/types"
)

// call lowers a recorded CALL. The callee marker must resolve to an observed
// function ref: a self-call becomes a framed native BL into this trace's own
// head, and non-self callees inline as fused frames the deopt path can rebuild.
func (l arm64Lowerer) call(ctx *lowering, op step) bool {
	if ctx.count() < 1 {
		return false
	}
	v := ctx.values[len(ctx.values)-1]
	if v.kind != types.KindRef {
		return false
	}
	target := resolve(ctx.module, ctx.heap, op.callee)
	if target == nil || target.Typ == nil {
		return false
	}
	closureRef := 0
	params := len(target.Typ.Params)
	if v.raw {
		if v.fn != op.callee {
			return false
		}
		if v.ref != op.callee {
			closureRef = v.ref
		}
	} else {
		if op.seen.Kind() != types.KindRef {
			return false
		}
		wantRef := op.seen.Ref()
		closureRef = wantRef
		if wantRef < 0 || wantRef >= len(ctx.heap) {
			return false
		}
		if cl, ok := ctx.heap[wantRef].(*types.Closure); ok {
			if int(cl.Fn) != op.callee {
				return false
			}
		} else if wantRef != op.callee {
			return false
		}
		pre := ctx.pre()
		fail, ok := l.sideExit(ctx, pre, op.ip)
		if !ok {
			return false
		}
		want := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDI(want, uint64(types.BoxRef(wantRef)))...)
		ctx.assembler.Emit(arm64.CMP(v.reg, want))
		ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBNE, fail))
		l.releaseBox(ctx, v.reg, pre, op.ip)
	}
	if len(target.Captures) > 0 {
		if closureRef <= 0 || closureRef >= len(ctx.heap) {
			return false
		}
		if _, ok := ctx.heap[closureRef].(*types.Closure); !ok {
			return false
		}
	} else {
		closureRef = 0
	}
	ctx.pop()
	if ctx.count() < params || !l.args(ctx, target, params) {
		return false
	}
	if op.callee == ctx.addr {
		if len(target.Captures) > 0 {
			return false
		}
		return l.selfCall(ctx, op, target, params)
	}
	if len(ctx.frames) >= 4 {
		return false
	}

	base := ctx.sp() - params
	vStack := ctx.pin(scratchStack)
	addr := l.base(ctx, vStack)
	for k := 0; k < params; k++ {
		boxed, ok := l.box(ctx, ctx.values[len(ctx.values)-params+k])
		if !ok {
			return false
		}
		ctx.assembler.Emit(arm64.STR(boxed, addr, int16((base+k)*8)))
	}

	f := ctx.frame()
	f.resume = op.ip + 1
	frame := newActivation(op.callee, target, base, len(ctx.values)-params)
	frame.upvalRef = closureRef
	ctx.values = ctx.values[:len(ctx.values)-params]
	if len(frame.kinds) > params {
		zero := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDI(zero, 0)...)
		for k := params; k < len(frame.kinds); k++ {
			switch frame.kinds[k] {
			case types.KindI1, types.KindI8, types.KindI32, types.KindF32, types.KindF64, types.KindI64:
			default:
				return false
			}
			frame.locals[k] = value{reg: zero, kind: frame.kinds[k], raw: true}
			frame.loaded[k] = true
			frame.dirty[k] = true
		}
	}
	ctx.frames = append(ctx.frames, frame)
	return true
}

// selfCall emits a framed native recursion into this trace's own head:
// flush state, check the frame budget, swap bp/sp, BL, propagate traps by
// recording the live frame chain, and reload everything afterwards because the
// callee owns every allocatable register.
func (l arm64Lowerer) selfCall(ctx *lowering, op step, target *types.Function, params int) bool {
	a := ctx.assembler
	for _, typ := range target.Typ.Returns {
		switch typ.Kind() {
		case types.KindI1, types.KindI8, types.KindI32, types.KindF32, types.KindF64, types.KindI64:
		default:
			return false
		}
	}
	if !l.flush(ctx, true) {
		return false
	}

	vCtrl := ctx.pin(scratchCtrl)
	active := ctx.pinTo(arm64.X15)
	budget := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(budget, vCtrl, int16(journalCap*8)))
	a.Emit(arm64.CMP(active, budget))
	hasFrame := a.Label()
	a.Emit(arm64.BCondLabel(arm64.OpBCC, hasFrame))
	l.overflow(ctx, op)
	a.Bind(hasFrame)

	a.Emit(
		arm64.ADDI(active, active, 1),
		arm64.STR(active, vCtrl, int16(journalActive*8)),
	)

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
	nBP := ctx.pinTo(oldBP)
	a.Emit(arm64.MOV(nBP, calleeBP))
	calleeSP := calleeBP
	if n := len(target.LocalKinds()); n > 0 {
		calleeSP = a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ADDI(calleeSP, calleeBP, uint16(n)))
	}
	nSP := ctx.pinTo(oldSP)
	a.Emit(arm64.MOV(nSP, calleeSP))

	a.Emit(arm64.BLLabel(ctx.head))

	// A trapped callee already recorded its frames; restore this caller's VM
	// bp, append the live frame chain inner-to-outer, and keep unwinding.
	vCtrl = ctx.pin(scratchCtrl)
	trap := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(trap, vCtrl, int16(journalTrap*8)))
	normal := a.Label()
	a.Emit(
		arm64.CBZLabel(trap, normal),
		arm64.LDR(oldBP, arm64.SP, 0),
	)
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

	// Capture returns before any reload can claim the ABI registers.
	base := ctx.sp() - params
	rets := target.Typ.Returns
	regs := make([]asm.VReg, len(rets))
	if len(rets) <= len(arm64.IntRets) {
		for k := range rets {
			regs[k] = ctx.pinTo(arm64.IntRets[k])
		}
	}
	ctx.values = ctx.values[:len(ctx.values)-params]
	for fi := range ctx.frames {
		f := &ctx.frames[fi]
		for k := range f.loaded {
			f.loaded[k] = false
			f.dirty[k] = false
		}
	}
	l.reload(ctx)
	if len(rets) > len(arm64.IntRets) {
		vStack := ctx.pin(scratchStack)
		addr := l.base(ctx, vStack)
		for k := range rets {
			regs[k] = a.Reg(asm.RegTypeInt, asm.Width64)
			a.Emit(arm64.LDR(regs[k], addr, int16((base+k)*8)))
		}
	}
	for k, typ := range rets {
		ctx.push(value{reg: regs[k], kind: typ.Kind(), raw: true})
	}
	return true
}

// tailTarget resolves a recorded tail call's compile-time function target and
// consumes the funcref marker, emitting the runtime funcref guard for a
// non-constant ref (mirrors call's guard). Tail calls carry no closure upvals,
// so a captured target is rejected; the trace stays threaded. On success the
// top params operands are the validated arguments, still live.
func (l arm64Lowerer) tailTarget(ctx *lowering, op step) (*types.Function, int, bool) {
	if ctx.count() < 1 {
		return nil, 0, false
	}
	v := ctx.values[len(ctx.values)-1]
	if v.kind != types.KindRef {
		return nil, 0, false
	}
	target := resolve(ctx.module, ctx.heap, op.callee)
	if target == nil || target.Typ == nil || len(target.Captures) > 0 {
		return nil, 0, false
	}
	params := len(target.Typ.Params)
	if v.raw {
		if v.fn != op.callee || v.ref != op.callee {
			return nil, 0, false
		}
	} else {
		if op.seen.Kind() != types.KindRef {
			return nil, 0, false
		}
		wantRef := op.seen.Ref()
		if wantRef != op.callee || wantRef < 0 || wantRef >= len(ctx.heap) {
			return nil, 0, false
		}
		if _, ok := ctx.heap[wantRef].(*types.Function); !ok {
			return nil, 0, false
		}
		want := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDI(want, uint64(types.BoxRef(wantRef)))...)
		ctx.assembler.Emit(arm64.CMP(v.reg, want))
		ok := ctx.assembler.Label()
		ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, ok))
		if !l.exit(ctx, op.ip) {
			return nil, 0, false
		}
		ctx.assembler.Bind(ok)
		pre := ctx.pre()
		l.releaseBox(ctx, v.reg, pre, op.ip)
	}
	ctx.pop()
	if ctx.count() < params || !l.args(ctx, target, params) {
		return nil, 0, false
	}
	return target, params, true
}

// locals fills frame f with the call arguments args in its parameter slots and
// a raw zero in every remaining local, matching threaded tail()/CALL's clear.
// Each slot is loaded and dirty so the next flush commits it to the VM stack.
func (l arm64Lowerer) locals(ctx *lowering, f *activation, args []value) bool {
	for k := range args {
		f.locals[k] = args[k]
		f.loaded[k] = true
		f.dirty[k] = true
	}
	if len(f.kinds) > len(args) {
		zero := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDI(zero, 0)...)
		for k := len(args); k < len(f.kinds); k++ {
			switch f.kinds[k] {
			case types.KindI1, types.KindI8, types.KindI32, types.KindF32, types.KindF64, types.KindI64:
			default:
				return false
			}
			f.locals[k] = value{reg: zero, kind: f.kinds[k], raw: true}
			f.loaded[k] = true
			f.dirty[k] = true
		}
	}
	return true
}

// tailLoop lowers a tail call back to the trace anchor as a native loop
// back-edge: the new arguments become the anchor entry frame's params, the
// other locals reset, everything commits to the VM stack, and iterate branches
// to the head (or yields when the safepoint budget runs out). Constant stack
// depth — no BL, no journalActive — so self/mutual tail recursion never grows.
func (l arm64Lowerer) tailLoop(ctx *lowering, op step) bool {
	target, params, ok := l.tailTarget(ctx, op)
	if !ok {
		return false
	}
	args := make([]value, params)
	for k := params - 1; k >= 0; k-- {
		args[k] = ctx.pop()
	}
	// A tail call stands in return position: no operands survive besides the
	// arguments just consumed.
	if ctx.count() != 0 {
		return false
	}
	f := newActivation(ctx.addr, target, 0, 0)
	if !l.locals(ctx, &f, args) {
		return false
	}
	ctx.frames = append(ctx.frames[:0], f)
	if !l.flush(ctx, true) {
		return false
	}
	return l.iterate(ctx, 0)
}

// tailMorph lowers a tail call to a different function by reusing the current
// frame in place: the activation is replaced by the callee at the same base,
// its params seeded from the arguments and its other locals reset, then the
// step emission continues into the callee's body. The frame record save/unwind writes
// describes the callee, so a later trap rebuilds the reused frame as the callee
// exactly as threaded tail() leaves it.
func (l arm64Lowerer) tailMorph(ctx *lowering, op step) bool {
	target, params, ok := l.tailTarget(ctx, op)
	if !ok {
		return false
	}
	old := ctx.frame()
	base := old.base
	args := make([]value, params)
	for k := params - 1; k >= 0; k-- {
		args[k] = ctx.pop()
	}
	if ctx.count() != 0 {
		return false
	}
	f := newActivation(op.callee, target, base, len(ctx.values))
	f.resume = op.ip + 1
	if !l.locals(ctx, &f, args) {
		return false
	}
	ctx.frames[len(ctx.frames)-1] = f
	return true
}

// stitch retires an inlined frame at its RETURN: the top return values
// land where the interpreter would put them — on the caller's operand stack.
func (l arm64Lowerer) stitch(ctx *lowering) bool {
	f := ctx.frame()
	if ctx.count() < f.returns {
		return false
	}
	rets := append([]value(nil), ctx.values[len(ctx.values)-f.returns:]...)
	ctx.values = ctx.values[:f.opBase]
	ctx.frames = ctx.frames[:len(ctx.frames)-1]
	ctx.values = append(ctx.values, rets...)
	return true
}

// ret closes the entry frame: boxed returns land at the frame base for
// the Go wrapper and in the ABI return registers for native callers.
func (l arm64Lowerer) ret(ctx *lowering) bool {
	if ctx.count() < ctx.returns {
		return false
	}
	a := ctx.assembler
	vStack := ctx.pin(scratchStack)
	addr := l.base(ctx, vStack)
	for idx := 0; idx < ctx.returns; idx++ {
		v := ctx.values[len(ctx.values)-ctx.returns+idx]
		boxed, ok := l.box(ctx, v)
		if !ok {
			return false
		}
		a.Emit(arm64.STR(boxed, addr, int16(idx*8)))
		if idx < len(arm64.IntRets) {
			ret := ctx.pinTo(arm64.IntRets[idx])
			a.Emit(arm64.MOV(ret, boxed))
		}
	}
	a.Emit(
		arm64.RET(),
	)
	return true
}

// complete finishes top-level module code: live locals and operands are boxed
// back to the VM stack, SP is published, and the wrapper marks the frame done.
func (l arm64Lowerer) complete(ctx *lowering) bool {
	if !l.flush(ctx, false) {
		return false
	}
	a := ctx.assembler
	vCtrl := ctx.pin(scratchCtrl)
	vBP := ctx.pin(scratchBP)
	sp := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADDI(sp, vBP, uint16(ctx.sp())))
	a.Emit(arm64.STR(sp, vCtrl, int16(journalSP*8)))
	l.report(ctx, vCtrl, trapNone, ctx.frame().end)
	a.Emit(
		arm64.RET(),
	)
	return true
}

// exit deopts to the threaded interpreter: flush every live value boxed,
// publish sp, record the live frame chain, and report a fallback at resume.
func (l arm64Lowerer) exit(ctx *lowering, resume int) bool {
	return l.trap(ctx, trapFallback, resume)
}

// trap unwinds the inlined native state into the journal and returns to the Go
// wrapper: every live value is flushed boxed, sp is published, the frame chain
// is recorded resuming at resume, and the trap kind is reported. trapFallback
// resumes threaded dispatch; trapYield re-enters native after a safepoint.
func (l arm64Lowerer) trap(ctx *lowering, kind, resume int) bool {
	if !l.flush(ctx, false) {
		return false
	}
	l.trapFlushed(ctx, kind, resume)
	return true
}

func (l arm64Lowerer) trapFlushed(ctx *lowering, kind, resume int) {
	a := ctx.assembler
	vCtrl := ctx.pin(scratchCtrl)
	vBP := ctx.pin(scratchBP)
	sp := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADDI(sp, vBP, uint16(ctx.sp())))
	a.Emit(arm64.STR(sp, vCtrl, int16(journalSP*8)))
	l.unwind(ctx, vCtrl, resume)
	l.report(ctx, vCtrl, kind, resume)
	a.Emit(
		arm64.RET(),
	)
}

// sideExit snapshots a guard fallback from the pre-op stack shape. The snapshot
// may include inlined frames; trapFlushed records the frame chain so the Go
// wrapper can rebuild the same threaded resume shape.
func (l arm64Lowerer) sideExit(ctx *lowering, pre []value, resume int) (asm.Label, bool) {
	ctx.values = append(ctx.values[:0], pre...)
	if !l.flush(ctx, false) {
		return 0, false
	}
	label := ctx.queueExit(nil, resume, 0)
	ctx.values = append(ctx.values[:0], pre...)
	return label, true
}

// iterate spends one unit of the safepoint budget at a loop back-edge:
// decrement journalBudget and branch to the loop head while budget remains,
// otherwise yield to the safepoint at the header. The caller has already
// committed loop-carried locals to the VM stack.
func (l arm64Lowerer) iterate(ctx *lowering, header int) bool {
	a := ctx.assembler
	vCtrl := ctx.pin(scratchCtrl)
	budget := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(budget, vCtrl, int16(journalBudget*8)))
	a.Emit(arm64.SUBI(budget, budget, 1))
	a.Emit(arm64.STR(budget, vCtrl, int16(journalBudget*8)))
	a.Emit(arm64.CBNZLabel(budget, ctx.back))
	return l.trap(ctx, trapYield, header)
}

// overflow surfaces a frame-budget overflow: the consumed callee marker
// is rematerialized and retained so the rebuilt interpreter state owns the
// reference the threaded CALL expects on top of the stack.
func (l arm64Lowerer) overflow(ctx *lowering, op step) {
	a := ctx.assembler
	vCtrl := ctx.pin(scratchCtrl)
	vStack := ctx.pin(scratchStack)
	addr := l.base(ctx, vStack)

	boxed := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(boxed, uint64(types.BoxRef(op.callee)))...)
	l.retain(ctx, op.callee)
	a.Emit(arm64.STR(boxed, addr, int16(ctx.sp()*8)))

	vBP := ctx.pin(scratchBP)
	sp := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADDI(sp, vBP, uint16(ctx.sp()+1)))
	a.Emit(arm64.STR(sp, vCtrl, int16(journalSP*8)))
	l.unwind(ctx, vCtrl, op.ip)
	l.report(ctx, vCtrl, trapOverflow, op.ip)
	a.Emit(
		arm64.RET(),
	)
}

// flush writes every dirty local and live operand to its VM stack home
// in boxed form. commit clears dirty marks — only the hot path may do that;
// a guard's cold path flushes a copy of the state and must leave the symbolic
// state of the surviving hot path untouched.
func (l arm64Lowerer) flush(ctx *lowering, commit bool) bool {
	a := ctx.assembler
	vStack := ctx.pin(scratchStack)
	addr := l.base(ctx, vStack)
	for fi := range ctx.frames {
		f := &ctx.frames[fi]
		for idx := range f.kinds {
			if !f.dirty[idx] {
				continue
			}
			boxed, ok := l.box(ctx, f.locals[idx])
			if !ok {
				return false
			}
			a.Emit(arm64.STR(boxed, addr, int16((f.base+idx)*8)))
			if commit {
				f.dirty[idx] = false
			}
		}
	}
	for j, v := range ctx.values {
		if v.kind == types.KindRef && commit {
			return false
		}
		boxed, ok := l.box(ctx, v)
		if !ok {
			return false
		}
		a.Emit(arm64.STR(boxed, addr, int16(ctx.slot(j)*8)))
	}
	return true
}

// reload pulls operands back from VM stack homes after a call or continuation.
func (l arm64Lowerer) reload(ctx *lowering) {
	a := ctx.assembler
	if len(ctx.values) == 0 {
		return
	}
	vStack := ctx.pin(scratchStack)
	addr := l.base(ctx, vStack)
	for j := range ctx.values {
		v := &ctx.values[j]
		if v.kind == types.KindRef && v.raw {
			continue
		}
		reg := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDR(reg, addr, int16(ctx.slot(j)*8)))
		v.reg = reg
		if v.kind != types.KindRef {
			if v.kind == types.KindI64 {
				v.reg = l.sign64(ctx, reg)
			}
			v.raw = true
		}
	}
}

// loadLocal materializes local idx from the VM stack on first use. A
// declared i32 or f64 local is unboxed for free: the boxed i32 keeps its
// value in the low lane and a boxed f64 is its own bit pattern. The narrow
// integer locals (i8, i1) share the i32 representation, so they load the same
// way and keep their kind.
func (l arm64Lowerer) loadLocal(ctx *lowering, f *activation, idx, ip int) bool {
	if f.loaded[idx] {
		return true
	}
	kind := f.kinds[idx]
	switch kind {
	case types.KindI1, types.KindI8, types.KindI32, types.KindF32, types.KindF64, types.KindI64, types.KindRef:
	default:
		return false
	}
	vStack := ctx.pin(scratchStack)
	addr := l.base(ctx, vStack)
	reg := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(reg, addr, int16((f.base+idx)*8)))
	if kind == types.KindI64 {
		// A heap-promoted i64 is a ref the trace cannot read as a value; guard
		// the inline tag and deopt at the load if it promoted, then sign-extend
		// the 49-bit value lane to a full raw i64 (always boxable thereafter).
		if !l.guardI64(ctx, reg, ip) {
			return false
		}
		reg = l.sign64(ctx, reg)
	}
	raw := kind != types.KindRef
	f.locals[idx] = value{reg: reg, kind: kind, raw: raw}
	f.loaded[idx] = true
	return true
}

func (l arm64Lowerer) upvalGet(ctx *lowering, op step) bool {
	f := ctx.frame()
	idx := int(op.args[0])
	if idx >= len(f.upvals) {
		return false
	}
	kind := f.upvals[idx]
	base := l.upvalBase(ctx)
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(dst, base, int16(idx*8)))
	if kind == types.KindI64 {
		if !l.guardI64(ctx, dst, op.ip) {
			return false
		}
		dst = l.sign64(ctx, dst)
	}
	if kind == types.KindRef {
		l.retainBox(ctx, dst)
		ctx.push(value{reg: dst, kind: kind, raw: false})
		return true
	}
	ctx.push(value{reg: dst, kind: kind, raw: true})
	return true
}

func (l arm64Lowerer) upvalSet(ctx *lowering, op step) bool {
	f := ctx.frame()
	idx := int(op.args[0])
	if idx >= len(f.upvals) || ctx.count() < 1 {
		return false
	}
	kind := f.upvals[idx]
	v := ctx.values[len(ctx.values)-1]
	if v.kind != kind {
		return false
	}
	boxed, ok := l.box(ctx, v)
	if !ok {
		return false
	}
	base := l.upvalBase(ctx)
	if kind == types.KindRef || kind == types.KindI64 {
		pre := ctx.pre()
		old := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDR(old, base, int16(idx*8)))
		l.releaseBoxUnlessEqual(ctx, old, boxed, pre, op.ip)
	}
	ctx.assembler.Emit(arm64.STR(boxed, base, int16(idx*8)))
	ctx.pop()
	return true
}

func (l arm64Lowerer) upvalBase(ctx *lowering) asm.VReg {
	f := ctx.frame()
	if f.upvalRef > 0 {
		heap := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDR(heap, ctx.pin(scratchCtrl), int16(journalHeap*8)))
		off := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDI(off, uint64(f.upvalRef))...)
		ctx.assembler.Emit(arm64.LSLI(off, off, 4))
		cell := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.ADD(cell, heap, off))
		data := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDR(data, cell, 8))
		base := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDR(base, data, int16(closureUpvs+sliceData)))
		return base
	}
	base := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(base, ctx.pin(scratchCtrl), int16(journalUpvals*8)))
	return base
}
