package arm64

import (
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/journal"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

type sideExit struct {
	label  asm.Label
	values []value
	frames []activation
	resume int
	id     int
}

// dispatch reads the journal's entry-IP cell and, when the Go wrapper set it
// to resume a bridge (see Interpreter.bridge), branches directly to that
// block's label instead of falling into the normal anchor start. Zero — the
// value every ordinary Call leaves it at — falls through to the anchor
// unchanged. Only blocks marked bridge (the resume point after a
// jit.TerminateBridge step) participate; ordinary state-backed blocks stay
// reachable only through internal branches.
func (l lowerer) dispatch(ctx *lowering, vCtrl asm.VReg) {
	var resumable []int
	for id, block := range ctx.blocks {
		if block.Bridge {
			resumable = append(resumable, id)
		}
	}
	if len(resumable) == 0 {
		return
	}
	a := ctx.assembler
	entry := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(entry, vCtrl, int16(journal.CellEntry*8)))
	done := a.Label()
	a.Emit(arm64.CMPI(entry, 0), arm64.BCondLabel(arm64.OpBEQ, done))
	for _, id := range resumable {
		want := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDI(want, uint64(ctx.blocks[id].Anchor.IP))...)
		a.Emit(arm64.CMP(entry, want), arm64.BCondLabel(arm64.OpBEQ, ctx.labels[id]))
	}
	a.Bind(done)
}

// emitExits emits every queued cold stub. A deferred ref in the exit
// snapshot was flushed to its VM stack slot without a retain, and the
// interpreter resuming there releases each stack ref it pops, so the stub
// takes the retain here — on the cold path only.
func (l lowerer) emitExits(ctx *lowering) bool {
	// Every exit's cold stub is a mutually exclusive, straight-line block
	// (each ends in trapFlushed, an unconditional trap/return), so the
	// registers used to reload-and-retain a deferred value are safe to reuse
	// across every exit and every deferred value in this function: their live
	// ranges never overlap at runtime. Allocating them once keeps this
	// bookkeeping from competing with the hot path's own no-spill register
	// budget (a function with several guarded ops and a live deferred operand
	// can have many exits, each otherwise adding its own fresh registers).
	var reg, refAddr, rcBase, rc asm.VReg
	for _, exit := range ctx.sideExits {
		ctx.values = exit.values
		ctx.frames = exit.frames
		ctx.assembler.Bind(exit.label)
		if ctx.budget.Width() != asm.WidthUndefined {
			ctx.assembler.Emit(arm64.STR(ctx.budget, ctx.pin(scratchCtrl), int16(journal.CellBudget*8)))
		}
		if !l.commitCarried(ctx) {
			return false
		}
		var addr asm.VReg
		for j, v := range exit.values {
			switch v.backing {
			case jit.BackingConst:
				if ref := v.ref; ref > 0 {
					l.retain(ctx, ref)
				}
			case jit.BackingLocal, jit.BackingGlobal, jit.BackingUpval:
				// The stub owns no live registers, but flush wrote every
				// operand to its VM stack slot before the guard, so the slot
				// is authoritative: reload the boxed ref from there and take
				// the retain the interpreter frame will release.
				if addr.Width() == asm.WidthUndefined {
					vStack := ctx.pin(scratchStack)
					addr = l.base(ctx, vStack)
				}
				if reg.Width() == asm.WidthUndefined {
					reg = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
				}
				ctx.assembler.Emit(arm64.LDR(reg, addr, int16(ctx.slot(j)*8)))
				if refAddr.Width() == asm.WidthUndefined {
					refAddr = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
				}
				ctx.assembler.Emit(arm64.ANDI(refAddr, reg, maskI32))
				if rcBase.Width() == asm.WidthUndefined {
					rcBase = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
				}
				ctx.assembler.Emit(arm64.LDR(rcBase, ctx.pin(scratchCtrl), int16(journal.CellRC*8)))
				if rc.Width() == asm.WidthUndefined {
					rc = ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
				}
				ctx.assembler.Emit(arm64.LDRR(rc, rcBase, refAddr))
				ctx.assembler.Emit(arm64.ADDI(rc, rc, 1))
				ctx.assembler.Emit(arm64.STRR(rc, rcBase, refAddr))
			}
		}
		l.trapFlushed(ctx, journal.TrapFallback, exit.resume, exit.id)
	}
	return true
}

// guardDivisor deopts before a divide by zero. When trace recorded a non-zero
// divisor, guardRaw owns the mismatch exit; otherwise the zero check protects
// the native divide itself.
func (l lowerer) guardDivisor(ctx *lowering, divisor value, reg asm.VReg, observed uint64, ip int) bool {
	guarded := false
	if !divisor.known && observed != 0 {
		if !l.guardRaw(ctx, reg, observed, ip) {
			return false
		}
		guarded = true
	}
	if divisor.known && divisor.imm != 0 || guarded {
		return true
	}
	fail, ok := l.sideExit(ctx, ctx.values, ip, prof.ExitGuardValue, ctx.opcode(ip))
	if !ok {
		return false
	}
	ctx.assembler.Emit(arm64.CMPI(reg, 0))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, fail))
	return true
}

// boxableI64 keeps raw i64 values within the boxed 49-bit lane.
func (l lowerer) boxableI64(ctx *lowering, raw asm.VReg, ip int) bool {
	fail, ok := l.sideExit(ctx, ctx.values, ip, prof.ExitGuardValue, ctx.opcode(ip))
	if !ok {
		return false
	}
	l.guardBoxable(ctx, raw, fail)
	return true
}

// guardRaw keeps observed narrow inputs speculative: a different runtime value
// exits before the opcode, so the threaded handler owns the general case.
func (l lowerer) guardRaw(ctx *lowering, got asm.VReg, val uint64, ip int) bool {
	fail, ok := l.sideExit(ctx, ctx.values, ip, prof.ExitGuardValue, ctx.opcode(ip))
	if !ok {
		return false
	}
	want := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(want, val)...)
	if got.Width() == asm.Width32 {
		want = l.narrow32(want)
	}
	ctx.assembler.Emit(arm64.CMP(got, want))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBNE, fail))
	return true
}

// guardI64 deopts when v is a heap-promoted i64.
func (l lowerer) guardI64(ctx *lowering, v asm.VReg, ip int) bool {
	fail, ok := l.sideExit(ctx, ctx.values, ip, prof.ExitGuardKind, ctx.opcode(ip))
	if !ok {
		return false
	}
	tag := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LSRI(tag, v, uint8(types.VBits)))
	want := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDI(want, tagI64>>types.VBits)...)
	ctx.assembler.Emit(arm64.CMP(tag, want))
	ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBNE, fail))
	return true
}

// sign64 sign-extends the 49-bit value lane of a boxed i64 to a full raw
// i64 value.
func (l lowerer) sign64(ctx *lowering, v asm.VReg) asm.VReg {
	out := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.SBFX(out, v, 0, boxableWidth))
	return out
}

// exit deopts to the threaded interpreter: flush every live value boxed,
// publish sp, record the live frame chain, and report a fallback at resume.
func (l lowerer) exit(ctx *lowering, resume int, reason prof.ExitReason, opcode int) bool {
	return l.trap(ctx, journal.TrapFallback, resume, reason, opcode)
}

// trap unwinds the inlined native state into the journal and returns to the Go
// wrapper: every live value is flushed boxed, sp is published, the frame chain
// is recorded resuming at resume, and the trap kind is reported. journal.TrapFallback
// resumes threaded dispatch; journal.TrapYield re-enters native after a safepoint;
// journal.TrapBridge hands exactly one opcode to the threaded interpreter and resumes
// native afterward (see Interpreter.bridge).
func (l lowerer) trap(ctx *lowering, kind journal.Trap, resume int, reason prof.ExitReason, opcode int) bool {
	if !l.flush(ctx, flushSnapshot) {
		return false
	}
	if !l.commitCarried(ctx) {
		return false
	}
	id := -1
	if kind == journal.TrapFallback || kind == journal.TrapBridge {
		// Both hand the flushed operand stack to code that adopts it as owned —
		// the threaded interpreter on journal.TrapFallback, the one bridged closure on
		// journal.TrapBridge — which releases each stack ref it pops. Re-take every
		// deferred ref's retain from its backing slot first. journal.TrapYield never
		// reaches here with a deferred live: its only caller commits first, and
		// a committing flush rejects deferred refs (see flush).
		l.retainDeferred(ctx)
	}
	if kind == journal.TrapFallback {
		id = len(ctx.exits)
		ctx.exits = append(ctx.exits, jit.Exit{Reason: reason, Opcode: opcode})
	}
	l.trapFlushed(ctx, kind, resume, id)
	return true
}

// bridge deopts one opcode the backend cannot lower: the Go wrapper runs that
// opcode's own threaded closure once and re-enters this callable at the
// closure's new IP (see Interpreter.bridge, dispatch). Unlike exit, it
// carries no exit descriptor — a bridge is productive continuation, not a
// trace-cut (see watchdog) — and the block that follows it in the plan needs
// no branch here: it is reached only through a fresh external entry.
func (l lowerer) bridge(ctx *lowering, ip int) bool {
	return l.trap(ctx, journal.TrapBridge, ip, prof.ExitNone, prof.OpcodeNone)
}

func (l lowerer) trapFlushed(ctx *lowering, kind journal.Trap, resume, exitID int) {
	a := ctx.assembler
	vCtrl := ctx.pin(scratchCtrl)
	vBP := ctx.pin(scratchBP)
	sp := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADDI(sp, vBP, uint16(ctx.sp())))
	a.Emit(arm64.STR(sp, vCtrl, int16(journal.CellSP*8)))
	l.unwind(ctx, vCtrl, resume)
	vExit := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(vExit, uint64(exitID+1))...)
	a.Emit(arm64.STR(vExit, vCtrl, int16(journal.CellExitID*8)))
	l.report(ctx, vCtrl, kind, resume)
	a.Emit(
		arm64.RET(),
	)
}

// unwind appends one journal frame record per live symbolic frame,
// innermost first so deopt rebuilds the chain in interpreter order. The
// innermost frame resumes at resume; outer frames resume past their calls.
func (l lowerer) unwind(ctx *lowering, vCtrl asm.VReg, resume int) {
	for k := len(ctx.frames) - 1; k >= 0; k-- {
		f := &ctx.frames[k]
		ip := f.resume
		if k == len(ctx.frames)-1 {
			ip = resume
		}
		l.save(ctx, vCtrl, f, ip)
	}
}

func (l lowerer) save(ctx *lowering, vCtrl asm.VReg, f *activation, ip int) {
	a := ctx.assembler
	depth := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(depth, vCtrl, int16(journal.CellDepth*8)))
	off := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LSLI(off, depth, journal.Shift))
	base := a.Reg(asm.RegTypeInt, asm.Width64)
	// base is record depth's first cell; the STP immediates below add each
	// field's offset within record 0, matching journal.At(0, field).
	a.Emit(arm64.ADD(base, vCtrl, off))

	vAddr := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(vAddr, uint64(f.addr))...)
	bp := ctx.pin(scratchBP)
	if f.base != 0 {
		shifted := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ADDI(shifted, bp, uint16(f.base)))
		bp = shifted
	}
	a.Emit(arm64.STP(vAddr, bp, base, int16(journal.At(0, journal.RecordAddr)*8)))

	vIP := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(vIP, uint64(ip))...)
	vReturns := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(vReturns, uint64(f.returns))...)
	a.Emit(arm64.STP(vIP, vReturns, base, int16(journal.At(0, journal.RecordIP)*8)))

	a.Emit(arm64.ADDI(depth, depth, 1))
	a.Emit(arm64.STR(depth, vCtrl, int16(journal.CellDepth*8)))
}

func (l lowerer) report(ctx *lowering, vCtrl asm.VReg, trap journal.Trap, nextIP int) {
	a := ctx.assembler
	vTrap := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(vTrap, uint64(trap))...)
	a.Emit(arm64.STR(vTrap, vCtrl, int16(journal.CellTrap*8)))
	vIP := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(vIP, uint64(nextIP))...)
	a.Emit(arm64.STR(vIP, vCtrl, int16(journal.CellNextIP*8)))
}

// sideExit snapshots a guard fallback from the pre-op stack shape. The snapshot
// may include inlined frames; trapFlushed records the frame chain so the Go
// wrapper can rebuild the same threaded resume shape.
func (l lowerer) sideExit(ctx *lowering, pre []value, resume int, reason prof.ExitReason, opcode int) (asm.Label, bool) {
	ctx.values = append(ctx.values[:0], pre...)
	if !l.flush(ctx, flushSnapshot) {
		return 0, false
	}
	label := ctx.queueExit(nil, resume, reason, opcode)
	ctx.values = append(ctx.values[:0], pre...)
	return label, true
}
