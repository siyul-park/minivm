package arm64

import (
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/journal"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

func (l lowerer) refNull(ctx *lowering) bool {
	boxed := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(boxed, uint64(types.BoxedNull))...)
	l.retainBox(ctx, boxed)
	ctx.push(value{reg: boxed, kind: types.KindRef})
	return true
}

func (l lowerer) refIsNull(ctx *lowering, op jit.Step) bool {
	if ctx.count() < 1 || ctx.values[len(ctx.values)-1].kind != types.KindRef {
		return false
	}
	owned := ctx.values[len(ctx.values)-1].backing == jit.BackingStack
	pre := ctx.pre()
	ref, ok := l.box(ctx, ctx.values[len(ctx.values)-1])
	if !ok {
		return false
	}
	vNull := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(vNull, uint64(types.BoxedNull))...)
	ctx.assembler.Emit(arm64.CMP(ref, vNull))
	// Capture the flags before release clobbers them, then release the consumed
	// ref (when it carries its own retain) so the bool result leaves no leaked
	// reference on the stack. A deferred operand carries no retain to release.
	flag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.CSET(flag, arm64.CondEQ))
	if owned {
		l.releaseBox(ctx, ref, pre, op.IP)
	}
	ctx.pop()
	ctx.push(value{reg: flag, kind: types.KindI1, raw: true})
	return true
}

// refEq lowers REF_EQ/REF_NE as a boxed-word compare. string.eq and string.ne
// compare content rather than ref identity, so they are not lowered here. At
// most one operand may own its retain: a single owned operand releases like refIsNull,
// while two owned operands report a terminal fallback because the second
// release could deopt after the first already decremented a refcount inline.
func (l lowerer) refEq(ctx *lowering, op jit.Step, negate bool) (bool, bool) {
	if ctx.count() < 2 || ctx.values[len(ctx.values)-1].kind != types.KindRef || ctx.values[len(ctx.values)-2].kind != types.KindRef {
		return false, false
	}
	owned1 := ctx.values[len(ctx.values)-1].backing == jit.BackingStack
	owned2 := ctx.values[len(ctx.values)-2].backing == jit.BackingStack
	if owned1 && owned2 {
		return true, l.exit(ctx, op.IP, prof.ExitTerminalOp, int(op.Op))
	}
	pre := ctx.pre()
	v1, ok := l.box(ctx, ctx.values[len(ctx.values)-1])
	if !ok {
		return false, false
	}
	v2, ok := l.box(ctx, ctx.values[len(ctx.values)-2])
	if !ok {
		return false, false
	}
	ctx.assembler.Emit(arm64.CMP(v2, v1))
	// Capture the flags before release clobbers them, then release the one
	// consumed ref that carries its own retain; deferred operands have none.
	cond := arm64.CondEQ
	if negate {
		cond = arm64.CondNE
	}
	flag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.CSET(flag, cond))
	if owned1 {
		l.releaseBox(ctx, v1, pre, op.IP)
	} else if owned2 {
		l.releaseBox(ctx, v2, pre, op.IP)
	}
	ctx.pop()
	ctx.pop()
	ctx.push(value{reg: flag, kind: types.KindI1, raw: true})
	return false, true
}

// retainDeferred re-takes the deferred retain for every operand in the current
// snapshot whose ref was flushed to its VM stack slot without one. A cold path
// that hands the flushed operand stack to the threaded interpreter (a
// journal.TrapFallback terminal exit, module completion) needs this: the interpreter
// adopts each stack ref as owned and later releases it, so a deferred ref
// flushed without a retain would be freed once too often. The caller must have
// flushed the operands first — the slot is authoritative — and must NOT change
// any backing, because the surviving hot path keeps emitting with the same
// symbolic state. Each call allocates fresh registers; the guard-exit stubs
// need a register-reusing variant instead (see emitExits) because they
// emit this per exit rather than once per trace.
func (l lowerer) retainDeferred(ctx *lowering) {
	var addr asm.VReg
	for j, v := range ctx.values {
		switch v.backing {
		case jit.BackingStack:
		case jit.BackingConst:
			if ref := v.ref; ref > 0 {
				l.retain(ctx, ref)
			}
		default:
			if addr.Width() == asm.WidthUndefined {
				addr = l.base(ctx, ctx.pin(scratchStack))
			}
			reg := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
			ctx.assembler.Emit(arm64.LDR(reg, addr, int16(ctx.slot(j)*8)))
			refAddr := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
			ctx.assembler.Emit(arm64.ANDI(refAddr, reg, maskI32))
			l.retainRef(ctx, refAddr)
		}
	}
}

// releaseOverwritten drops the retain a slot held before it is overwritten.
// The outgoing and incoming values may be the same address, and releasing it
// first would free a value the store is about to publish - but only an owned
// incoming value can alias that way, because a deferred one still borrows its
// own backing slot's retain. So the aliasing check is needed exactly when the
// incoming value is not deferred.
func (l lowerer) releaseOverwritten(ctx *lowering, old, val asm.VReg, deferred bool, pre []value, ip int) {
	if deferred {
		l.releaseBox(ctx, old, pre, ip)
		return
	}
	l.releaseBoxExcept(ctx, old, val, pre, ip)
}

func (l lowerer) releaseBoxExcept(ctx *lowering, old, val asm.VReg, pre []value, ip int) {
	done := ctx.assembler.Label()
	ctx.assembler.Emit(arm64.CMP(old, val), arm64.BCondLabel(arm64.OpBEQ, done))
	l.releaseBox(ctx, old, pre, ip)
	ctx.assembler.Bind(done)
	ctx.values = append(ctx.values[:0], pre...)
}

func (l lowerer) releaseBox(ctx *lowering, v asm.VReg, pre []value, ip int) {
	l.refOnly(ctx, v, func(addr asm.VReg) {
		a := ctx.assembler
		done := a.Label()
		a.Emit(arm64.CMPI(addr, 0), arm64.BCondLabel(arm64.OpBEQ, done))
		l.releaseRef(ctx, addr, pre, ip)
		a.Bind(done)
	})
}

func (l lowerer) retainBoxExcept(ctx *lowering, old, val asm.VReg) {
	done := ctx.assembler.Label()
	ctx.assembler.Emit(arm64.CMP(old, val), arm64.BCondLabel(arm64.OpBEQ, done))
	l.retainBox(ctx, val)
	ctx.assembler.Bind(done)
}

func (l lowerer) retainBox(ctx *lowering, v asm.VReg) {
	l.refOnly(ctx, v, func(addr asm.VReg) {
		l.retainRef(ctx, addr)
	})
}

func (l lowerer) retainRef(ctx *lowering, addr asm.VReg) {
	base := l.rcBase(ctx)
	rc := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDRR(rc, base, addr))
	ctx.assembler.Emit(arm64.ADDI(rc, rc, 1))
	ctx.assembler.Emit(arm64.STRR(rc, base, addr))
}

// releaseRef decrements addr after guardRC proves it will stay live.
func (l lowerer) releaseRef(ctx *lowering, addr asm.VReg, pre []value, ip int) {
	fail, ok := l.sideExit(ctx, pre, ip, prof.ExitGuardValue, ctx.opcode(ip))
	if !ok {
		return
	}
	rcBase := l.rcBase(ctx)
	rc := l.guardRC(ctx, addr, rcBase, fail)
	ctx.assembler.Emit(arm64.SUBI(rc, rc, 1))
	ctx.assembler.Emit(arm64.STRR(rc, rcBase, addr))
}

// detach owns every operand backed by the slot identified by (backing, slot) before
// that slot's content changes underneath it — a LOCAL_SET/GLOBAL_SET/UPVAL_SET
// write, or a frame dying at RETURN/tail dispatch. A deferred value left
// undetached would keep pointing at a slot whose content (or existence) no
// longer matches what it observed.
func (l lowerer) detach(ctx *lowering, b jit.Backing, slot int) bool {
	for i := range ctx.values {
		v := &ctx.values[i]
		if v.kind == types.KindRef && v.backing == b && v.slot == slot {
			if _, ok := l.own(ctx, v); !ok {
				return false
			}
		}
	}
	return true
}

// ownRefs transfers every live deferred ref onto the operand stack. Calls use
// this ownership barrier before handing flushed state to another execution
// context that may release or mutate the backing storage.
func (l lowerer) ownRefs(ctx *lowering) bool {
	for i := range ctx.values {
		v := &ctx.values[i]
		if v.kind == types.KindRef && v.backing != jit.BackingStack {
			if _, ok := l.own(ctx, v); !ok {
				return false
			}
		}
	}
	return true
}

// own boxes v and, when its reference count is deferred to backing storage
// (backing != jit.BackingStack), takes the retain that transfers ownership onto the
// operand stack and marks v owned. Callers pass a pointer into ctx.values (or
// other frame-owned storage) so a later exit snapshot never also stub-retains
// the same transfer.
func (l lowerer) own(ctx *lowering, v *value) (asm.VReg, bool) {
	reg, ok := l.box(ctx, *v)
	if !ok {
		return asm.VReg{}, false
	}
	if v.kind != types.KindRef || v.backing == jit.BackingStack {
		return reg, true
	}
	if v.backing == jit.BackingConst {
		l.retain(ctx, v.ref)
		v.reg = reg
	} else {
		addr := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.ANDI(addr, reg, maskI32))
		l.retainRef(ctx, addr)
	}
	v.backing = jit.BackingStack
	return reg, true
}

// box produces the boxed form of v in a fresh register for read-only use.
// It takes no reference-count action: a jit.BackingConst ref materializes its
// compile-time constant with no retain, and every other ref (jit.BackingStack or
// deferred to slot-backed storage) is already boxed in v.reg.
func (l lowerer) box(ctx *lowering, v value) (asm.VReg, bool) {
	a := ctx.assembler
	switch v.kind {
	case types.KindI32:
		if !v.raw {
			return v.reg, true
		}
		lo := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ANDI(lo, v.reg, maskI32))
		a.Emit(arm64.MOVK(lo, uint16(tagI32>>48), 48))
		return lo, true
	case types.KindI8:
		if !v.raw {
			return v.reg, true
		}
		lo := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ANDI(lo, v.reg, maskI32))
		a.Emit(arm64.MOVK(lo, uint16(tagI8>>48), 48))
		return lo, true
	case types.KindI1:
		if !v.raw {
			return v.reg, true
		}
		lo := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ANDI(lo, v.reg, maskI32))
		a.Emit(arm64.MOVK(lo, uint16(tagI1>>48), 48))
		return lo, true
	case types.KindF32:
		lo := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ANDI(lo, v.reg, maskI32))
		tag := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDI(tag, tagF32)...)
		a.Emit(arm64.ORR(lo, lo, tag))
		return lo, true
	case types.KindI64:
		// Raw i64 is the full signed value and boxable by invariant; mask the
		// 49-bit lane and tag.
		lo := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ANDI(lo, v.reg, maskI64))
		tag := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDI(tag, tagI64)...)
		a.Emit(arm64.ORR(lo, lo, tag))
		return lo, true
	case types.KindF64:
		return v.reg, true
	case types.KindRef:
		if v.backing != jit.BackingConst {
			return v.reg, true
		}
		boxed := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDI(boxed, uint64(types.BoxRef(v.ref)))...)
		return boxed, true
	}
	return asm.VReg{}, false
}

// retain bumps the refcount of the heap cell at compile-time address fn.
func (l lowerer) retain(ctx *lowering, fn int) {
	a := ctx.assembler
	base := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(base, ctx.pin(scratchCtrl), int16(journal.CellRC*8)))
	slot := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(slot, uint64(fn))...)
	rc := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDRR(rc, base, slot))
	a.Emit(arm64.ADDI(rc, rc, 1))
	a.Emit(arm64.STRR(rc, base, slot))
}

// guardRC keeps releases that could free objects in the interpreter.
func (l lowerer) guardRC(ctx *lowering, addr, rcBase asm.VReg, fail asm.Label) asm.VReg {
	rc := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	a := ctx.assembler
	a.Emit(arm64.LDRR(rc, rcBase, addr), arm64.CMPI(rc, 1), arm64.BCondLabel(arm64.OpBLE, fail))
	return rc
}

func (l lowerer) refOnly(ctx *lowering, v asm.VReg, body func(asm.VReg)) {
	tag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LSRI(tag, v, uint8(types.VBits)))
	want := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(want, tagRef>>types.VBits)...)
	ctx.assembler.Emit(arm64.CMP(tag, want))

	done := ctx.assembler.Label()
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBNE, done))

	addr := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(addr, v, maskI32))
	body(addr)

	ctx.assembler.Bind(done)
}

func (l lowerer) rcBase(ctx *lowering) asm.VReg {
	base := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(base, ctx.pin(scratchCtrl), int16(journal.CellRC*8)))
	return base
}
