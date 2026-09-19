package arm64

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/backend"
	"github.com/siyul-park/minivm/internal/journal"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

// selfSave is selfCall's save-area size: bp and LR, one word each. call's
// area also saves SP and X26 because an external callee's own prologue
// repoints them; selfCall's callee is this same code, so neither moves.
const selfSave = 16

// call lowers CALL to a native BLR through the callee's natives slot, or -
// for a callee that is this same compile's own function, whose slot is not
// published yet - to selfCall's direct branch. It admits only a constant
// callee whose whole signature is scalar, and declines everything else to
// the plan pipeline, which still serves it.
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
	if !ok || ref <= 0 || ref > maxSlot {
		return false
	}
	if ref == e.c.Input().Address {
		return e.selfCall(op, ref)
	}
	target, params, rets, ok := e.signature(op, ref)
	if !ok {
		return false
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
	// A null entry redoes CALL threaded from op.State, whose frame still
	// holds the callee above every argument, so no marker needs
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

	if !e.unadopt(op, ctrl) {
		return false
	}
	if !e.reserve(ctrl, d) {
		return false
	}
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
	// X26 is this frame's spill base; the callee's own prologue repoints it,
	// so it is saved and restored around the BLR like BP/SP (see
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
	// already holds it.
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

	e.unreserve(ctrl)
	e.a.Emit(
		arm64.LDP(oldBP, oldSP, arm64.SP, 0),
		arm64.STR(oldBP, ctrl, int16(journal.CellBP*8)),
		arm64.STR(oldSP, ctrl, int16(journal.CellSP*8)),
		arm64.LDR(arm64.LR, arm64.SP, 16),
		arm64.ADDI(arm64.SP, arm64.SP, 32),
	)

	if !e.results(op, rets) {
		return false
	}
	e.spent = append(e.spent, op.State)
	return true
}

// selfRecursive reports whether any CALL names this function's own address.
// Enter sets recursive from it once, since addr's choice applies to the
// whole compile (see emit.go's addr).
func (e *emitter) selfRecursive() bool {
	fn := e.c.Func()
	addr := e.c.Input().Address
	for id := 0; id < fn.Len(); id++ {
		for _, op := range fn.Block(id).Ops {
			if op.Op != ssa.OpExec || op.Code != instr.CALL || len(op.Args) < 1 {
				continue
			}
			if ref, ok := e.callee(op.Args[len(op.Args)-1]); ok && ref == addr {
				return true
			}
		}
	}
	return false
}

// selfCall lowers a CALL whose constant callee is this compile's own
// function: a direct BL to block zero's own label, rather than call's BLR
// through a natives-table slot this compile has not published yet.
//
// It admits only an EntryFunction root with exactly one live frame. A loop
// root re-enters an already-live frame and never runs the zero-init prologue
// block zero's label sits behind (see emit.go's Enter). More than one live
// frame means a trace inlined another activation before this CALL, and that
// activation's locals, not the entry frame's, would take the callee's
// zero-init.
func (e *emitter) selfCall(op ssa.Operation, ref int) bool {
	if e.kind != jit.EntryFunction {
		return false
	}
	_, params, rets, ok := e.signature(op, ref)
	if !ok {
		return false
	}

	d := e.c.Exit(op.State, prof.ExitNone, prof.OpcodeNone)
	if !fits(d) || len(d.Frames) != 1 {
		return false
	}
	// d.SP still counts the callee's own slot, like call's nextSP: one slot
	// is excluded to land calleeBP on the first param.
	delta := d.SP - 1 - len(params)
	if delta < 0 || delta > maxSlot {
		return false
	}

	ctrl := e.pin(scratchCtrl)
	if !e.unadopt(op, ctrl) {
		return false
	}
	if !e.reserve(ctrl, d) {
		return false
	}
	if !e.materialize(d.Slots, ctrl) {
		return false
	}

	bp := e.pin(scratchBP)
	calleeBP := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.ADDI(calleeBP, bp, uint16(delta)))
	calleeBase := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(
		arm64.LSLI(calleeBase, calleeBP, 3),
		arm64.ADD(calleeBase, e.pin(scratchStack), calleeBase),
	)

	// The callee's own Enter never runs on this branch, so its declared
	// locals past its parameters need the zero-init a fresh external entry
	// gets from Enter's own loop.
	declared := e.c.Input().Function.Declared()
	zeros := map[types.Boxed]asm.VReg{}
	for idx := len(params); idx < len(declared); idx++ {
		zero := types.Zero(declared[idx].Kind())
		reg, ok := zeros[zero]
		if !ok {
			reg = e.a.Reg(asm.RegTypeInt, asm.Width64)
			e.a.Emit(arm64.LDI(reg, uint64(zero))...)
			zeros[zero] = reg
		}
		e.a.Emit(arm64.STR(reg, calleeBase, int16(idx*8)))
	}

	e.a.Emit(
		arm64.SUBI(arm64.SP, arm64.SP, selfSave),
		arm64.STR(bp, arm64.SP, 0),
		arm64.STR(arm64.LR, arm64.SP, 8),
	)
	newBP := e.pinTo(e.scratch[scratchBP])
	e.a.Emit(arm64.MOV(newBP, calleeBP))
	e.a.Emit(arm64.BLLabel(e.c.Block(0)))

	// ctrl and X15 never change across the BL: this callee reloads neither
	// from anywhere new, since its own Enter never runs.
	ctrl = e.pin(scratchCtrl)
	trap := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.LDR(trap, ctrl, int16(journal.CellTrap*8)))
	normal := e.a.Label()
	e.a.Emit(arm64.CBZLabel(trap, normal))

	// The trapped callee already published its own trap state; the caller
	// only restores its own bp, appends its own frame record, and returns.
	restored := e.pinTo(e.scratch[scratchBP])
	e.a.Emit(arm64.LDR(restored, arm64.SP, 0))
	rec := d.Frames[0]
	rec.IP++
	e.record(rec, ctrl, restored)
	e.a.Emit(
		arm64.LDR(arm64.LR, arm64.SP, 8),
		arm64.ADDI(arm64.SP, arm64.SP, selfSave),
		arm64.RET(),
	)
	e.a.Bind(normal)

	e.unreserve(ctrl)
	restored = e.pinTo(e.scratch[scratchBP])
	e.a.Emit(arm64.LDR(restored, arm64.SP, 0))
	e.a.Emit(
		arm64.LDR(arm64.LR, arm64.SP, 8),
		arm64.ADDI(arm64.SP, arm64.SP, selfSave),
	)

	if !e.results(op, rets) {
		return false
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

// signature resolves ref to the function it names and validates it against
// op's own call site: no captures, matching arity, and a whole scalar
// signature that fits the ABI return registers - the one shape both call and
// selfCall admit a callee through.
func (e *emitter) signature(op ssa.Operation, ref int) (*types.Function, []types.Type, []types.Type, bool) {
	target := e.c.Input().Objects.Function(ref)
	if target == nil || target.Typ == nil || len(target.Captures) > 0 {
		return nil, nil, nil, false
	}
	params := target.Typ.Params
	rets := target.Typ.Returns
	if len(op.Args) != len(params)+1 || len(op.Results) != len(rets) || len(rets) > len(arm64.IntRets) {
		return nil, nil, nil, false
	}
	for i, p := range params {
		if p.Kind() == types.KindRef || e.c.Func().Type(op.Args[i]) != ssa.TypeOf(p.Kind()) {
			return nil, nil, nil, false
		}
	}
	for _, r := range rets {
		if r.Kind() == types.KindRef {
			return nil, nil, nil, false
		}
	}
	return target, params, rets, true
}

// unadopt drops the callee's speculative retain: frontend/walk.go's adopt
// takes one ahead of every CALL for a deopt's flushed stack, but the
// threaded CONST_GET+CALL fusion takes no matching retain for a static
// callee, so native code gives it back.
func (e *emitter) unadopt(op ssa.Operation, ctrl asm.VReg) bool {
	fail := e.a.Label()
	e.drop(e.c.Reg(op.Args[len(op.Args)-1]), ctrl, fail)
	done := e.a.Label()
	e.a.Emit(arm64.BLabel(done))
	e.a.Bind(fail)
	d := e.c.Exit(op.State, prof.ExitGuardValue, e.opcode(op.State))
	if !fits(d) {
		return false
	}
	if !e.unwind(d, journal.TrapFallback) {
		return false
	}
	e.a.Bind(done)
	return true
}

// reserve admits the call only while journal.CellCap still allows another
// native frame, and holds one by incrementing journal.CellActive; unreserve
// releases it. X15 is read fresh here because Enter never mirrors it in for
// a function with no CALL.
func (e *emitter) reserve(ctrl asm.VReg, d backend.Deopt) bool {
	active := e.pinTo(arm64.X15)
	e.a.Emit(arm64.LDR(active, ctrl, int16(journal.CellActive*8)))
	limit := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.LDR(limit, ctrl, int16(journal.CellCap*8)))
	e.a.Emit(arm64.CMP(active, limit))
	has := e.a.Label()
	e.a.Emit(arm64.BCondLabel(arm64.OpBCC, has))
	if !e.unwind(d, journal.TrapOverflow) {
		return false
	}
	e.a.Bind(has)
	e.a.Emit(arm64.ADDI(active, active, 1))
	e.a.Emit(arm64.STR(active, ctrl, int16(journal.CellActive*8)))
	return true
}

func (e *emitter) unreserve(ctrl asm.VReg) {
	active := e.pinTo(arm64.X15)
	e.a.Emit(arm64.SUBI(active, active, 1))
	e.a.Emit(arm64.STR(active, ctrl, int16(journal.CellActive*8)))
}

// results binds a returning callee's ABI registers to op's own result
// values, boxed exactly as ret emits them for any BL or BLR that lands on
// it.
func (e *emitter) results(op ssa.Operation, rets []types.Type) bool {
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
	return true
}
