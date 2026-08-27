package arm64

import (
	"unsafe"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/journal"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

// activation mirrors one interpreter frame the trace inlined. Locals live in
// registers; loaded marks which have been pulled from the VM stack and dirty
// marks which must be written back before native code gives up control.
type activation struct {
	end    int
	kinds  []types.Kind
	upvals []types.Kind
	locals []value
	state  []localState

	addr     int
	base     int
	opBase   int
	upvalRef int
	resume   int
	returns  int
}

type localState uint8

const (
	localLoaded localState = 1 << iota
	localDirty
	localStored
)

// closureUpvs is the byte offset of a closure's upvalue slice header, used by
// upvalBase to reach a captured frame's upvalue storage.
const closureUpvs = int(unsafe.Offsetof(types.Closure{}.Upvals))

// newActivation builds the activation for one inlined frame: base is the
// frame's VM stack floor and opBase is the operand-stack index its own
// operands start at.
func newActivation(addr int, fn *types.Function, base, opBase int) activation {
	kinds := fn.Slots()
	upvals := types.Kinds(fn.Captures)
	returns := 0
	if fn.Typ != nil {
		returns = len(fn.Typ.Returns)
	}
	return activation{
		end:     len(fn.Code),
		kinds:   kinds,
		upvals:  upvals,
		locals:  make([]value, len(kinds)),
		state:   make([]localState, len(kinds)),
		addr:    addr,
		base:    base,
		opBase:  opBase,
		returns: returns,
	}
}

// isLoadedAt reports whether local idx currently lives in a register. It is the
// one test for that: locals holds whichever register a value was last
// materialized into, and a native call clears the state without clearing that
// name, so a reader that consults locals alone sees a register the callee has
// since overwritten (see docs/jit-internals.md).
func (f *activation) isLoadedAt(idx int) bool {
	return f.state[idx]&localLoaded != 0
}

func (l lowerer) directCall(ctx *lowering, op jit.Step) bool {
	target := jit.Resolve(ctx.module, ctx.heap, op.Callee)
	if target == nil || target.Typ == nil || ctx.count() < 1 {
		return false
	}
	params := len(target.Typ.Params)
	if ctx.count() < params+1 || op.Callee > 4095 || !l.checkReturns(target) {
		return false
	}

	marker := ctx.pop()
	if marker.fn != op.Callee || !l.checkArgs(ctx, target, params) {
		ctx.push(marker)
		return false
	}
	// The BLR slot path cannot serve a call to the function being emitted: it
	// has published no slot yet. So a self callee either takes selfCall or is
	// rejected outright - falling through would compute the callee frame from a
	// stack that still carries the marker.
	if op.Callee == ctx.addr {
		if ctx.kind == jit.EntryFunction && len(ctx.frames) == 1 && len(target.Captures) == 0 &&
			l.selfCall(ctx, op, target, params) {
			return true
		}
		ctx.push(marker)
		return false
	}

	a := ctx.assembler
	vCtrl := ctx.pin(scratchCtrl)
	natives := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(natives, vCtrl, int16(journal.CellNatives*8)))
	callee := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(callee, natives, int16(op.Callee*8)))
	ready := a.Label()
	a.Emit(arm64.CBNZLabel(callee, ready))
	// The threaded fallback re-executes the whole CALL, so the exit has to see
	// the marker this lowering already consumed.
	ctx.push(marker)
	if !l.exit(ctx, op.IP, prof.ExitTerminalOp, int(op.Op)) {
		return false
	}
	a.Bind(ready)
	ctx.pop()

	// A real BLR hands this caller's flushed operand stack to the interpreter
	// when the callee traps (the unwind adopts the caller frames), and passes
	// each ref argument to the callee as an owned param it releases on RETURN.
	// Either way a deferred ref must own its retain before the flush, or the
	// interpreter/callee would release a reference this trace never took.
	// Post-call consumers are emitted after this mutation, so they observe
	// jit.BackingStack and release normally.
	if !l.ownRefs(ctx) {
		return false
	}
	if !l.flush(ctx, flushSnapshot) {
		return false
	}

	active := ctx.pinTo(arm64.X15)
	limit := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(limit, vCtrl, int16(journal.CellCap*8)))
	a.Emit(arm64.CMP(active, limit))
	hasFrame := a.Label()
	a.Emit(arm64.BCondLabel(arm64.OpBCC, hasFrame))
	l.overflow(ctx, op)
	a.Bind(hasFrame)
	a.Emit(arm64.ADDI(active, active, 1))
	a.Emit(arm64.STR(active, vCtrl, int16(journal.CellActive*8)))

	vBP := ctx.pin(scratchBP)
	nextSP := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADDI(nextSP, vBP, uint16(ctx.sp())))
	oldBP := ctx.scratch[scratchBP]
	oldSP := ctx.scratch[scratchSP]
	// X26 is this activation's spill base. The callee is entered at its own
	// offset zero, so it runs the frame prologue and repoints X26 at its own
	// frame; on return that frame is dead. Save and restore X26 around the
	// call - the 32-byte save area already has the room - or every spill
	// reload after the call would address a dead frame. A self-call (BL to
	// ctx.head) needs no such save: it shares this activation's frame, and
	// that stream cannot spill at all, because its backward branch to head
	// disables the spill frame (see asm/rewriter.go backEdge).
	a.Emit(
		arm64.SUBI(arm64.SP, arm64.SP, 32),
		arm64.STP(oldBP, oldSP, arm64.SP, 0),
		arm64.STR(arm64.LR, arm64.SP, 16),
		arm64.STR(arm64.X26, arm64.SP, 24),
	)
	calleeBP := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.SUBI(calleeBP, nextSP, uint16(params)))
	a.Emit(arm64.MOV(ctx.pinTo(oldBP), calleeBP))

	localKinds := target.Slots()
	calleeSP := calleeBP
	if len(localKinds) > 0 {
		calleeSP = a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ADDI(calleeSP, calleeBP, uint16(len(localKinds))))
	}
	a.Emit(arm64.MOV(ctx.pinTo(oldSP), calleeSP))
	a.Emit(
		arm64.STR(calleeBP, vCtrl, int16(journal.CellBP*8)),
		arm64.STR(calleeSP, vCtrl, int16(journal.CellSP*8)),
		arm64.MOV(arm64.X0, vCtrl),
		arm64.BLR(callee),
	)
	a.Emit(arm64.LDR(arm64.X26, arm64.SP, 24))

	vCtrl = ctx.pin(scratchCtrl)
	trap := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(trap, vCtrl, int16(journal.CellTrap*8)))
	normal := a.Label()
	a.Emit(arm64.CBZLabel(trap, normal), arm64.LDR(oldBP, arm64.SP, 0))
	l.unwind(ctx, vCtrl, op.IP+1)
	a.Emit(
		arm64.LDR(arm64.LR, arm64.SP, 16),
		arm64.ADDI(arm64.SP, arm64.SP, 32),
		arm64.RET(),
	)
	a.Bind(normal)

	active = ctx.pinTo(arm64.X15)
	a.Emit(arm64.SUBI(active, active, 1))
	a.Emit(arm64.STR(active, vCtrl, int16(journal.CellActive*8)))
	a.Emit(
		arm64.LDP(oldBP, oldSP, arm64.SP, 0),
		arm64.STR(oldBP, vCtrl, int16(journal.CellBP*8)),
		arm64.STR(oldSP, vCtrl, int16(journal.CellSP*8)),
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
	l.clearLocals(ctx)
	l.reload(ctx)
	for idx, typ := range rets {
		out := typ.Kind()
		ctx.push(value{reg: regs[idx], kind: out, raw: out != types.KindRef})
	}
	return true
}

// call lowers a recorded CALL. The callee marker must resolve to an observed
// function ref: a self-call becomes a framed native BL into this trace's own
// head, and non-self callees inline as fused frames the deopt path can rebuild.
func (l lowerer) call(ctx *lowering, op jit.Step) bool {
	if ctx.count() < 1 {
		return false
	}
	v := ctx.values[len(ctx.values)-1]
	if v.kind != types.KindRef {
		return false
	}
	target := jit.Resolve(ctx.module, ctx.heap, op.Callee)
	if target == nil || target.Typ == nil {
		return false
	}
	closureRef := 0
	params := len(target.Typ.Params)
	if v.backing == jit.BackingConst {
		if v.fn != op.Callee {
			return false
		}
		if v.ref != op.Callee {
			closureRef = v.ref
		}
	} else {
		if op.Seen.Kind() != types.KindRef {
			return false
		}
		wantRef := op.Seen.Ref()
		closureRef = wantRef
		if wantRef < 0 || wantRef >= len(ctx.heap) {
			return false
		}
		if cl, ok := ctx.heap[wantRef].(*types.Closure); ok {
			if int(cl.Fn) != op.Callee {
				return false
			}
		} else if wantRef != op.Callee {
			return false
		}
		pre := ctx.pre()
		fail, ok := l.sideExit(ctx, pre, op.IP, prof.ExitGuardValue, int(op.Op))
		if !ok {
			return false
		}
		want := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDI(want, uint64(types.BoxRef(wantRef)))...)
		ctx.assembler.Emit(arm64.CMP(v.reg, want))
		ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBNE, fail))
		if v.backing == jit.BackingStack {
			l.releaseBox(ctx, v.reg, pre, op.IP)
		}
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
	if op.Callee == ctx.addr && !l.checkReturns(target) {
		return false
	}
	ctx.pop()
	if ctx.count() < params || !l.checkArgs(ctx, target, params) {
		return false
	}
	if op.Callee == ctx.addr {
		return l.selfCall(ctx, op, target, params)
	}
	if len(ctx.frames) >= 4 {
		return false
	}

	base := ctx.sp() - params
	vStack := ctx.pin(scratchStack)
	addr := l.base(ctx, vStack)
	for k := 0; k < params; k++ {
		// This inlines the callee's own activation directly onto the VM
		// stack, so a deferred argument must own its retain here: its backing slot
		// slot is unrelated storage that may change independently of this
		// new frame's local.
		boxed, ok := l.own(ctx, &ctx.values[len(ctx.values)-params+k])
		if !ok {
			return false
		}
		ctx.assembler.Emit(arm64.STR(boxed, addr, int16((base+k)*8)))
	}

	f := ctx.frame()
	f.resume = op.IP + 1
	frame := newActivation(op.Callee, target, base, len(ctx.values)-params)
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
			frame.state[k] = frame.state[k]&^localStored | localLoaded | localDirty
		}
	}
	ctx.frames = append(ctx.frames, frame)
	return true
}

// selfCall lowers recursion through the function entry.
func (l lowerer) selfCall(ctx *lowering, op jit.Step, target *types.Function, params int) bool {
	if ctx.kind != jit.EntryFunction || len(ctx.frames) != 1 || len(target.Captures) > 0 || !l.checkReturns(target) {
		return false
	}

	a := ctx.assembler
	// Own deferred refs before the callee can mutate their backing storage.
	if !l.ownRefs(ctx) {
		return false
	}
	if !l.flush(ctx, flushCommit) {
		return false
	}

	vCtrl := ctx.pin(scratchCtrl)
	active := ctx.pinTo(arm64.X15)
	budget := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(budget, vCtrl, int16(journal.CellCap*8)))
	a.Emit(arm64.CMP(active, budget))
	hasFrame := a.Label()
	a.Emit(arm64.BCondLabel(arm64.OpBCC, hasFrame))
	l.overflow(ctx, op)
	a.Bind(hasFrame)

	a.Emit(
		arm64.ADDI(active, active, 1),
		arm64.STR(active, vCtrl, int16(journal.CellActive*8)),
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
	if n := len(target.Slots()); n > 0 {
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
	a.Emit(arm64.LDR(trap, vCtrl, int16(journal.CellTrap*8)))
	normal := a.Label()
	a.Emit(
		arm64.CBZLabel(trap, normal),
		arm64.LDR(oldBP, arm64.SP, 0),
	)
	l.unwind(ctx, vCtrl, op.IP+1)
	a.Emit(
		arm64.LDR(arm64.LR, arm64.SP, 16),
		arm64.ADDI(arm64.SP, arm64.SP, 32),
		arm64.RET(),
	)
	a.Bind(normal)

	active = ctx.pinTo(arm64.X15)
	a.Emit(arm64.SUBI(active, active, 1))
	a.Emit(arm64.STR(active, vCtrl, int16(journal.CellActive*8)))
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
		clear(f.state)
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
		out := typ.Kind()
		ctx.push(value{reg: regs[k], kind: out, raw: out != types.KindRef})
	}
	return true
}

// checkReturns verifies every return kind fits the registers a native call
// hands results back in.
func (l lowerer) checkReturns(target *types.Function) bool {
	for _, typ := range target.Typ.Returns {
		switch typ.Kind() {
		case types.KindI1, types.KindI8, types.KindI32, types.KindI64, types.KindF32, types.KindF64, types.KindRef:
		default:
			return false
		}
	}
	return true
}

// tailLoop lowers a tail call back to the trace anchor as a native loop
// back-edge: the new arguments become the anchor entry frame's params, the
// other locals reset, everything commits to the VM stack, and iterate branches
// to the head (or yields when the safepoint budget runs out). Constant stack
// depth — no BL, no journal.CellActive — so self/mutual tail recursion never grows.
func (l lowerer) tailLoop(ctx *lowering, op jit.Step) bool {
	target, params, ok := l.tailTarget(ctx, op)
	if !ok {
		return false
	}
	args := make([]value, params)
	for k := params - 1; k >= 0; k-- {
		args[k] = ctx.pop()
	}
	// A tail call stands in return position: no operands survive besides the
	// arguments just consumed. The anchor frame has opBase 0, so ctx.count() == 0
	// means ctx.values is empty here and no deferred operand needs owning; the
	// arguments are owned into the new frame by initLocals() below.
	if ctx.count() != 0 {
		return false
	}
	old := ctx.frame()
	f := newActivation(ctx.addr, target, 0, 0)
	refs, ok := l.guardFrame(ctx, old, op.IP)
	if !ok {
		return false
	}
	if !l.initLocals(ctx, &f, args) {
		return false
	}
	l.releaseFrame(ctx, refs)
	ctx.frames = append(ctx.frames[:0], f)
	if !l.flush(ctx, flushCommit) {
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
func (l lowerer) tailMorph(ctx *lowering, op jit.Step) bool {
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
	// The innermost activation is retired in place. Its operands are gone
	// (count() == 0); outer-frame operands are outside this activation's local
	// ownership. initLocals() first acquires any argument ownership that the new
	// frame needs, then releaseFrame drops the retiring activation.
	f := newActivation(op.Callee, target, base, len(ctx.values))
	f.resume = op.IP + 1
	refs, ok := l.guardFrame(ctx, old, op.IP)
	if !ok {
		return false
	}
	if !l.initLocals(ctx, &f, args) {
		return false
	}
	l.releaseFrame(ctx, refs)
	ctx.frames[len(ctx.frames)-1] = f
	return true
}

// tailTarget resolves a recorded tail call's compile-time function target and
// consumes the funcref marker, emitting the runtime funcref guard for a
// non-constant ref (mirrors call's guard). Tail calls carry no closure upvals,
// so a captured target is rejected; the trace stays threaded. On success the
// top params operands are the validated arguments, still live.
func (l lowerer) tailTarget(ctx *lowering, op jit.Step) (*types.Function, int, bool) {
	if ctx.count() < 1 {
		return nil, 0, false
	}
	v := ctx.values[len(ctx.values)-1]
	if v.kind != types.KindRef {
		return nil, 0, false
	}
	target := jit.Resolve(ctx.module, ctx.heap, op.Callee)
	if target == nil || target.Typ == nil || len(target.Captures) > 0 {
		return nil, 0, false
	}
	params := len(target.Typ.Params)
	if v.backing == jit.BackingConst {
		if v.fn != op.Callee || v.ref != op.Callee {
			return nil, 0, false
		}
	} else {
		if op.Seen.Kind() != types.KindRef {
			return nil, 0, false
		}
		wantRef := op.Seen.Ref()
		if wantRef != op.Callee || wantRef < 0 || wantRef >= len(ctx.heap) {
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
		if !l.exit(ctx, op.IP, prof.ExitTerminalOp, int(op.Op)) {
			return nil, 0, false
		}
		ctx.assembler.Bind(ok)
		pre := ctx.pre()
		if v.backing == jit.BackingStack {
			l.releaseBox(ctx, v.reg, pre, op.IP)
		}
	}
	ctx.pop()
	if ctx.count() < params || !l.checkArgs(ctx, target, params) {
		return nil, 0, false
	}
	return target, params, true
}

// checkArgs verifies the top params operands match the callee's parameter kinds.
func (l lowerer) checkArgs(ctx *lowering, target *types.Function, params int) bool {
	kinds := target.Slots()
	if len(kinds) < params {
		return false
	}
	for k := 0; k < params; k++ {
		v := ctx.values[len(ctx.values)-params+k]
		if v.kind != kinds[k] {
			return false
		}
		if v.kind == types.KindRef {
			if v.backing == jit.BackingConst {
				return false
			}
			continue
		}
		if !v.raw {
			return false
		}
	}
	return true
}

// zeroLocals clears the entry frame's non-parameter locals, matching the clear
// threaded CALL performs before it transfers control. The callee owns this, not
// its callers: a frame opens on stack space an earlier frame may have left
// populated, and every entry path - the Go wrapper, directCall's BLR, and
// selfCall's BL to head - arrives here with bp already pointing at the new
// frame. Skipping it hands the callee stale boxed words, so its first LOCAL_SET
// releases a ref it never owned and RETURN teardown releases the rest.
//
// Only a whole-function entry may do this. A loop plan re-enters a frame whose
// locals are live, and module code has no caller to have cleared them.
func (l lowerer) zeroLocals(ctx *lowering) {
	if ctx.kind != jit.EntryFunction || len(ctx.frames) == 0 {
		return
	}
	kinds := ctx.frames[0].kinds
	if len(kinds) <= ctx.params {
		return
	}
	a := ctx.assembler
	base := l.base(ctx, ctx.pin(scratchStack))
	for idx := ctx.params; idx < len(kinds); idx++ {
		reg := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDI(reg, uint64(types.Zero(kinds[idx])))...)
		a.Emit(arm64.STR(reg, base, int16(idx*8)))
	}
}

// initLocals fills frame f with call arguments in its parameter slots and
// a raw zero in every remaining local, matching threaded tail()/CALL's clear.
// Each slot is loaded and dirty so the next flush commits it to the VM stack.
func (l lowerer) initLocals(ctx *lowering, f *activation, args []value) bool {
	for k := range args {
		// This becomes a new frame's own tracked local, so a deferred ref
		// argument must own its retain: the new backing slot is unrelated storage
		// from the one it deferred to.
		if args[k].kind == types.KindRef && args[k].backing != jit.BackingStack {
			if _, ok := l.own(ctx, &args[k]); !ok {
				return false
			}
		}
		f.locals[k] = args[k]
		f.state[k] = f.state[k]&^localStored | localLoaded | localDirty
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
			f.state[k] = f.state[k]&^localStored | localLoaded | localDirty
		}
	}
	return true
}

// stitch retires an inlined frame at its RETURN: the top return values
// land where the interpreter would put them — on the caller's operand stack.
func (l lowerer) stitch(ctx *lowering, ip int) bool {
	f := ctx.frame()
	if ctx.count() < f.returns {
		return false
	}
	rets := append([]value(nil), ctx.values[len(ctx.values)-f.returns:]...)
	refs, ok := l.guardFrame(ctx, f, ip)
	if !ok {
		return false
	}
	// A return backed by one of the callee's locals must acquire an independent
	// retain before that local is released with the rest of the retiring frame.
	for i := range rets {
		v := &rets[i]
		if v.kind != types.KindRef {
			continue
		}
		if (v.backing == jit.BackingLocal && v.slot >= f.base) || v.backing == jit.BackingUpval {
			if _, ok := l.own(ctx, v); !ok {
				return false
			}
		}
	}
	l.releaseFrame(ctx, refs)
	ctx.values = ctx.values[:f.opBase]
	ctx.frames = ctx.frames[:len(ctx.frames)-1]
	ctx.values = append(ctx.values, rets...)
	return true
}

// ret closes the entry frame: boxed returns land at the frame base for
// the Go wrapper and in the ABI return registers for native callers.
func (l lowerer) ret(ctx *lowering, ip int) bool {
	if ctx.count() < ctx.returns {
		return false
	}
	a := ctx.assembler
	f := ctx.frame()
	// A native entry frame owns every ref local/parameter until RETURN. Retain
	// deferred return values before guardFrame reads their refcount: guardFrame
	// deopts when rc <= pending, and a return backed by a frame-owned local
	// (rc == pending, e.g. a freshly allocated node held only by that local) would
	// spuriously fail the guard unless this retain lands first. Taking it here
	// makes rc == pending + 1, so the guard passes and the following
	// releaseFrame's decrement leaves the exact one live reference the caller now
	// owns.
	for idx := 0; idx < ctx.returns; idx++ {
		if _, ok := l.own(ctx, &ctx.values[len(ctx.values)-ctx.returns+idx]); !ok {
			return false
		}
	}
	refs, ok := l.guardFrame(ctx, f, ip)
	if !ok {
		return false
	}
	l.releaseFrame(ctx, refs)
	vStack := ctx.pin(scratchStack)
	addr := l.base(ctx, vStack)
	for idx := 0; idx < ctx.returns; idx++ {
		// The entry frame is ending; the owned return is written into the
		// caller-visible result slot after the frame's local refs are released.
		boxed, ok := l.box(ctx, ctx.values[len(ctx.values)-ctx.returns+idx])
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

// guardFrame checks that an activation can be released without deopt.
func (l lowerer) guardFrame(ctx *lowering, f *activation, ip int) ([]asm.VReg, bool) {
	addrs := make([]asm.VReg, 0, len(f.kinds))
	var stack asm.VReg
	a := ctx.assembler
	for i, kind := range f.kinds {
		if kind != types.KindRef {
			continue
		}
		var ref asm.VReg
		if f.isLoadedAt(i) {
			var ok bool
			ref, ok = l.box(ctx, f.locals[i])
			if !ok {
				return nil, false
			}
		} else {
			if stack.Width() == asm.WidthUndefined {
				stack = l.base(ctx, ctx.pin(scratchStack))
			}
			ref = a.Reg(asm.RegTypeInt, asm.Width64)
			a.Emit(arm64.LDR(ref, stack, int16((f.base+i)*8)))
		}
		addr := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.ANDI(addr, ref, maskI32))
		addrs = append(addrs, addr)
	}
	if len(addrs) == 0 {
		return nil, true
	}
	pre := ctx.pre()
	fail, ok := l.sideExit(ctx, pre, ip, prof.ExitGuardValue, ctx.opcode(ip))
	if !ok {
		return nil, false
	}
	rcBase := l.rcBase(ctx)
	for i, addr := range addrs {
		skip := a.Label()
		pending := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.CMPI(addr, 0), arm64.BCondLabel(arm64.OpBEQ, skip))
		a.Emit(arm64.LDI(pending, 1)...)
		for j := 0; j < i; j++ {
			a.Emit(arm64.CMP(addr, addrs[j]))
			noMatch := a.Label()
			a.Emit(arm64.BCondLabel(arm64.OpBNE, noMatch), arm64.ADDI(pending, pending, 1))
			a.Bind(noMatch)
		}
		rc := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDRR(rc, rcBase, addr), arm64.CMP(rc, pending), arm64.BCondLabel(arm64.OpBLE, fail))
		a.Bind(skip)
	}
	return addrs, true
}

// releaseFrame drops refs after guardFrame succeeds.
func (l lowerer) releaseFrame(ctx *lowering, addrs []asm.VReg) {
	if len(addrs) == 0 {
		return
	}
	rcBase := l.rcBase(ctx)
	a := ctx.assembler
	for _, addr := range addrs {
		skip := a.Label()
		a.Emit(arm64.CMPI(addr, 0), arm64.BCondLabel(arm64.OpBEQ, skip))
		rc := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDRR(rc, rcBase, addr), arm64.SUBI(rc, rc, 1), arm64.STRR(rc, rcBase, addr))
		a.Bind(skip)
	}
}

// complete finishes top-level module code: live locals and operands are boxed
// back to the VM stack, SP is published, and the wrapper marks the frame done.
func (l lowerer) complete(ctx *lowering) bool {
	if !l.flush(ctx, flushSnapshot) {
		return false
	}
	if !l.commitCarried(ctx) {
		return false
	}
	// The wrapper preserves this top-level operand stack on journal.TrapNone (see
	// start()), and the interpreter adopts each stack ref as owned, so a
	// deferred ref left on the stack at module end must re-take its retain.
	l.retainDeferred(ctx)
	a := ctx.assembler
	vCtrl := ctx.pin(scratchCtrl)
	vBP := ctx.pin(scratchBP)
	sp := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADDI(sp, vBP, uint16(ctx.sp())))
	a.Emit(arm64.STR(sp, vCtrl, int16(journal.CellSP*8)))
	l.report(ctx, vCtrl, journal.TrapNone, ctx.frame().end)
	a.Emit(
		arm64.RET(),
	)
	return true
}

// iterate spends one unit of the safepoint budget at a loop back-edge:
// decrement journal.CellBudget and branch to the loop head while budget remains,
// otherwise yield to the safepoint at the header. The caller has already
// committed loop-carried locals to the VM stack.
func (l lowerer) iterate(ctx *lowering, header int) bool {
	a := ctx.assembler
	vCtrl := ctx.pin(scratchCtrl)
	budget := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDR(budget, vCtrl, int16(journal.CellBudget*8)))
	a.Emit(arm64.SUBI(budget, budget, 1))
	a.Emit(arm64.STR(budget, vCtrl, int16(journal.CellBudget*8)))
	a.Emit(arm64.CBNZLabel(budget, ctx.back))
	return l.trap(ctx, journal.TrapYield, header, prof.ExitNone, prof.OpcodeNone)
}

// overflow surfaces a frame-budget overflow: the consumed callee marker
// is rematerialized and retained so the rebuilt interpreter state owns the
// reference the threaded CALL expects on top of the stack.
func (l lowerer) overflow(ctx *lowering, op jit.Step) {
	a := ctx.assembler
	vCtrl := ctx.pin(scratchCtrl)
	vStack := ctx.pin(scratchStack)
	addr := l.base(ctx, vStack)

	boxed := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.LDI(boxed, uint64(types.BoxRef(op.Callee)))...)
	l.retain(ctx, op.Callee)
	a.Emit(arm64.STR(boxed, addr, int16(ctx.sp()*8)))

	vBP := ctx.pin(scratchBP)
	sp := a.Reg(asm.RegTypeInt, asm.Width64)
	a.Emit(arm64.ADDI(sp, vBP, uint16(ctx.sp()+1)))
	a.Emit(arm64.STR(sp, vCtrl, int16(journal.CellSP*8)))
	l.unwind(ctx, vCtrl, op.IP)
	l.report(ctx, vCtrl, journal.TrapOverflow, op.IP)
	a.Emit(
		arm64.RET(),
	)
}

// loadLocal materializes local idx from the VM stack on first use. A
// declared i32 or f64 local is unboxed for free: the boxed i32 keeps its
// value in the low lane and a boxed f64 is its own bit pattern. The narrow
// integer locals (i8, i1) share the i32 representation, so they load the same
// way and keep their kind.
func (l lowerer) loadLocal(ctx *lowering, f *activation, idx, ip int) bool {
	if f.isLoadedAt(idx) {
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
	f.state[idx] |= localLoaded
	return true
}

func (l lowerer) upvalGet(ctx *lowering, op jit.Step) bool {
	f := ctx.frame()
	idx := int(op.Args[0])
	if idx >= len(f.upvals) {
		return false
	}
	kind := f.upvals[idx]
	base := l.upvalBase(ctx)
	dst := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(dst, base, int16(idx*8)))
	if kind == types.KindI64 {
		if !l.guardI64(ctx, dst, op.IP) {
			return false
		}
		dst = l.sign64(ctx, dst)
	}
	if kind == types.KindRef {
		ctx.push(value{reg: dst, kind: kind, backing: jit.BackingUpval, slot: idx})
		return true
	}
	ctx.push(value{reg: dst, kind: kind, raw: true})
	return true
}

func (l lowerer) upvalSet(ctx *lowering, op jit.Step) bool {
	f := ctx.frame()
	idx := int(op.Args[0])
	if idx >= len(f.upvals) || ctx.count() < 1 {
		return false
	}
	kind := f.upvals[idx]
	vp := &ctx.values[len(ctx.values)-1]
	if vp.kind != kind {
		return false
	}
	var boxed asm.VReg
	var ok bool
	deferred := kind == types.KindRef && vp.backing != jit.BackingStack
	boxed, ok = l.box(ctx, *vp)
	if !ok {
		return false
	}
	base := l.upvalBase(ctx)
	if kind == types.KindRef {
		pre := ctx.pre()
		old := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDR(old, base, int16(idx*8)))
		l.releaseOverwritten(ctx, old, boxed, deferred, pre, op.IP)
		if _, ok := l.own(ctx, vp); !ok {
			return false
		}
		if !l.detach(ctx, jit.BackingUpval, idx) {
			return false
		}
	}
	ctx.assembler.Emit(arm64.STR(boxed, base, int16(idx*8)))
	ctx.pop()
	return true
}

func (l lowerer) upvalBase(ctx *lowering) asm.VReg {
	f := ctx.frame()
	if f.upvalRef > 0 {
		heap := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.assembler.Emit(arm64.LDR(heap, ctx.pin(scratchCtrl), int16(journal.CellHeap*8)))
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
	ctx.assembler.Emit(arm64.LDR(base, ctx.pin(scratchCtrl), int16(journal.CellUpvals*8)))
	return base
}
