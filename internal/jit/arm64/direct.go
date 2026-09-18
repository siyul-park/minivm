package arm64

import (
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/journal"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

// call lowers CALL to a native BLR through the callee's natives slot. It
// admits only a constant, non-self-recursive callee whose whole signature
// is scalar, and declines everything else to the plan pipeline, which still
// serves it.
//
// A captured callee is declined outright: journal.CellUpvals is mirrored
// once per native dispatch cycle from the outermost frame (see
// interp/jit.go's journalPtr) and nothing here repoints it across the BLR,
// so a captured callee would read its caller's upvalues as its own.
func (e *emitter) call(op ssa.Operation) bool {
	if len(op.Args) < 1 || op.State == ssa.NoValue {
		return false
	}
	ref, ok := e.callee(op.Args[len(op.Args)-1])
	if !ok || ref <= 0 || ref == e.c.Input().Address || ref > maxSlot {
		return false
	}
	target := e.c.Input().Objects.Function(ref)
	if target == nil || target.Typ == nil || len(target.Captures) > 0 {
		return false
	}
	params := target.Typ.Params
	rets := target.Typ.Returns
	if len(op.Args) != len(params)+1 || len(op.Results) != len(rets) || len(rets) > len(arm64.IntRets) {
		return false
	}
	for i, p := range params {
		if p.Kind() == types.KindRef || e.c.Func().Type(op.Args[i]) != ssa.TypeOf(p.Kind()) {
			return false
		}
	}
	for _, r := range rets {
		if r.Kind() == types.KindRef {
			return false
		}
	}

	d := e.c.Exit(op.State, prof.ExitNone, prof.OpcodeNone)
	if !fits(d) {
		return false
	}

	ctrl := e.pin(scratchCtrl)
	natives := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.LDR(natives, ctrl, int16(journal.CellNatives*8)))
	entry := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.LDR(entry, natives, int16(ref*8)))
	// The fallback re-executes the whole CALL from op.State, whose frame
	// still holds the callee above every argument, so no marker needs
	// rematerializing the way the plan pipeline's own directCall remakes one.
	ready := e.a.Label()
	e.a.Emit(arm64.CBNZLabel(entry, ready))
	dTerm := e.c.Exit(op.State, prof.ExitTerminalOp, e.opcode(op.State))
	if !fits(dTerm) {
		return false
	}
	if !e.unwind(dTerm, journal.TrapFallback) {
		return false
	}
	e.a.Bind(ready)

	// frontend/walk.go's adopt retains the callee ahead of every CALL for a
	// deopt's flushed stack, but the threaded CONST_GET+CALL fusion takes no
	// matching retain for a static callee, so native code drops it back out.
	// This runs before the budget check and its active-count bookkeeping: a
	// freeing release redoes the whole CALL, which must never undo an active
	// increment it never reached. It is inline, like the entry-absent exit,
	// because a deferred stub shares e.base with the code after the BLR
	// across a branch that skips it, which the allocator correctly refuses
	// to spill there.
	relFail := e.a.Label()
	e.drop(e.c.Reg(op.Args[len(op.Args)-1]), ctrl, relFail)
	relDone := e.a.Label()
	e.a.Emit(arm64.BLabel(relDone))
	e.a.Bind(relFail)
	dRel := e.c.Exit(op.State, prof.ExitGuardValue, e.opcode(op.State))
	if !fits(dRel) {
		return false
	}
	if !e.unwind(dRel, journal.TrapFallback) {
		return false
	}
	e.a.Bind(relDone)

	// X15 is the pinned active-call-depth register, but Enter never mirrors
	// it in: a function with no CALL never reads it. This first read loads
	// journal.CellActive, which every native entry publishes (see
	// interp/jit.go's journalPtr); later reads in this compile keep trusting
	// the register, since this function's own STR keeps the two in step and
	// a callee that itself calls restores it symmetrically.
	active := e.pinTo(arm64.X15)
	e.a.Emit(arm64.LDR(active, ctrl, int16(journal.CellActive*8)))
	limit := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.LDR(limit, ctrl, int16(journal.CellCap*8)))
	e.a.Emit(arm64.CMP(active, limit))
	hasFrame := e.a.Label()
	e.a.Emit(arm64.BCondLabel(arm64.OpBCC, hasFrame))
	// The overflow deopt resumes CALL itself, exactly like the entry-absent
	// exit above, and registers no descriptor: it is not a speculation that
	// failed, it is a budget the interpreter must enforce itself.
	if !e.unwind(d, journal.TrapOverflow) {
		return false
	}
	e.a.Bind(hasFrame)
	e.a.Emit(arm64.ADDI(active, active, 1))
	e.a.Emit(arm64.STR(active, ctrl, int16(journal.CellActive*8)))

	// The callee reads its parameters off the VM stack, so these are the top
	// len(params) slots calleeBP below points it at.
	if !e.materialize(d.Slots, ctrl) {
		return false
	}

	bp := e.pin(scratchBP)
	nextSP := e.a.Reg(asm.RegTypeInt, asm.Width64)
	// d.SP still counts the callee's own slot: op.State predates CALL popping
	// anything, so one slot is excluded to land calleeBP on the first param.
	e.a.Emit(arm64.ADDI(nextSP, bp, uint16(d.SP-1)))
	oldBP, oldSP := e.scratch[scratchBP], e.scratch[scratchSP]
	// X26 is this frame's spill base; the callee's prologue repoints it, so
	// it is saved and restored around the BLR like BP/SP (see
	// internal/asm/arm64's frame.go).
	e.a.Emit(
		arm64.SUBI(arm64.SP, arm64.SP, 32),
		arm64.STP(oldBP, oldSP, arm64.SP, 0),
		arm64.STR(arm64.LR, arm64.SP, 16),
		arm64.STR(arm64.X26, arm64.SP, 24),
	)
	calleeBP := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.SUBI(calleeBP, nextSP, uint16(len(params))))
	e.a.Emit(arm64.MOV(e.pinTo(oldBP), calleeBP))

	calleeSP := calleeBP
	if locals := target.Slots(); len(locals) > 0 {
		calleeSP = e.a.Reg(asm.RegTypeInt, asm.Width64)
		e.a.Emit(arm64.ADDI(calleeSP, calleeBP, uint16(len(locals))))
	}
	e.a.Emit(arm64.MOV(e.pinTo(oldSP), calleeSP))
	e.a.Emit(
		arm64.STR(calleeBP, ctrl, int16(journal.CellBP*8)),
		arm64.STR(calleeSP, ctrl, int16(journal.CellSP*8)),
		arm64.MOV(arm64.X0, ctrl),
		arm64.BLR(entry),
	)
	e.a.Emit(arm64.LDR(arm64.X26, arm64.SP, 24))

	// ctrl never changes across the BLR, so re-pinning it is free: X14
	// already holds it. Keeping the old vreg live instead would force the
	// allocator to spill and reload it around the call.
	ctrl = e.pin(scratchCtrl)
	trap := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.LDR(trap, ctrl, int16(journal.CellTrap*8)))
	normal := e.a.Label()
	e.a.Emit(arm64.CBZLabel(trap, normal))

	// The trapped callee already published its own trap state; the caller
	// only appends its own frame record and returns.
	e.a.Emit(arm64.LDR(oldBP, arm64.SP, 0))
	rec := d.Frames[0]
	rec.IP++
	e.record(rec, ctrl, e.pin(scratchBP))
	e.a.Emit(
		arm64.LDR(arm64.LR, arm64.SP, 16),
		arm64.ADDI(arm64.SP, arm64.SP, 32),
		arm64.RET(),
	)
	e.a.Bind(normal)

	active = e.pinTo(arm64.X15)
	e.a.Emit(arm64.SUBI(active, active, 1))
	e.a.Emit(arm64.STR(active, ctrl, int16(journal.CellActive*8)))
	e.a.Emit(
		arm64.LDP(oldBP, oldSP, arm64.SP, 0),
		arm64.STR(oldBP, ctrl, int16(journal.CellBP*8)),
		arm64.STR(oldSP, ctrl, int16(journal.CellSP*8)),
		arm64.LDR(arm64.LR, arm64.SP, 16),
		arm64.ADDI(arm64.SP, arm64.SP, 32),
	)

	// Results come back in arm64.IntRets boxed, exactly as ret emits them: a
	// float needs the same unboxing load does for a slot, an i64 the same
	// sign-extract guardI64 uses for one, and anything narrower is already
	// its own raw view once narrowed.
	for idx, r := range rets {
		reg := e.pinTo(arm64.IntRets[idx])
		dst := e.c.Reg(op.Results[idx])
		switch ssa.TypeOf(r.Kind()) {
		case ssa.TypeI1, ssa.TypeI8, ssa.TypeI32:
			e.a.Emit(arm64.MOV(dst, narrow32(reg)))
		case ssa.TypeI64:
			e.a.Emit(arm64.SBFX(dst, reg, 0, types.VBits))
		case ssa.TypeF32:
			e.a.Emit(arm64.FMOV(dst, narrow32(reg)))
		case ssa.TypeF64:
			e.a.Emit(arm64.FMOV(dst, reg))
		default:
			return false
		}
	}
	e.spent = append(e.spent, op.State)
	return true
}

// callee resolves v to the constant function reference it names: an OpConst
// names one directly, and an OpGuardValue names one through the constant its
// second argument is guarded against (see frontend/walk.go's callee).
func (e *emitter) callee(v ssa.Value) (int, bool) {
	op, ok := e.c.Def(v)
	if !ok {
		return 0, false
	}
	switch op.Op {
	case ssa.OpConst:
		return op.Const.Ref(), true
	case ssa.OpGuardValue:
		if len(op.Args) != 2 {
			return 0, false
		}
		c, ok := e.c.Def(op.Args[1])
		if !ok || c.Op != ssa.OpConst {
			return 0, false
		}
		return c.Const.Ref(), true
	default:
		return 0, false
	}
}
