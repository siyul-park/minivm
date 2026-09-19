package arm64

import (
	"slices"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/journal"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

// carriedLocal is one root-frame scalar whose register is authoritative until
// a native loop exits. slot remains the VM home committed by cold paths.
type carriedLocal struct {
	value value
	local int
	slot  int
}

// work is a deferred block whose branch point produced its symbolic state:
// ordinary VM stack slots are current, so the block re-enters at label with
// every ordinary local unloaded and every operand awaiting reload. Carried
// loop locals reconnect to their authoritative registers instead. If the
// branch returned from an inlined callee, tail keeps the caller path that must
// run after the deferred block stitches back into the caller frame.
type work struct {
	label  asm.Label
	block  int
	tail   []int
	values []value
	frames []activation
}

type flushMode uint8

const (
	flushSnapshot flushMode = iota
	flushCommit
)

const branchTableLimit = 32

// workLimit bounds work growth from learned continuations; existing work
// reuses its native label, while new states keep the deopt fallback.
const workLimit = 256

func (m machine) emitBlock(ctx *lowering, id int, tail []int) bool {
	if id < 0 || id >= len(ctx.blocks) {
		return false
	}
	block := ctx.blocks[id]
	if block.State != nil {
		ctx.values = ctx.values[:0]
		for _, slot := range block.State {
			ctx.values = append(ctx.values, value{kind: slot.Kind, ref: slot.Ref, backing: slot.Backing, slot: slot.Offset})
		}
		m.clearLocals(ctx)
		m.reload(ctx)
	}
	done, ok := m.steps(ctx, block.Steps)
	if !ok {
		return false
	}
	if done {
		return true
	}
	if block.Term.Kind == jit.TerminateFallthrough && len(tail) > 0 {
		return m.follow(ctx, tail)
	}
	return m.term(ctx, block, tail)
}

func (m machine) term(ctx *lowering, block jit.Block, tail []int) bool {
	switch block.Term.Kind {
	case jit.TerminateFallthrough:
		return true
	case jit.TerminateBranch:
		if len(block.Term.Edges) != 1 {
			return false
		}
		target := block.Term.Edges[0]
		if block.Term.Hot == 0 {
			return m.next(ctx, block.Anchor, target, tail, int(instr.BR))
		}
		if !m.flush(ctx, flushSnapshot) {
			return false
		}
		return m.path(ctx, block.Anchor, target, tail, int(instr.BR))
	case jit.TerminateBranchIf:
		return m.conditional(ctx, block, tail)
	case jit.TerminateBranchTable:
		return m.table(ctx, block, tail)
	case jit.TerminateReturn:
		if len(ctx.frames) > 1 {
			if !m.stitch(ctx, block.Term.IP) {
				return false
			}
			if len(tail) > 0 {
				return m.follow(ctx, tail)
			}
			if ctx.kind == jit.EntryModule {
				return m.complete(ctx)
			}
			return m.ret(ctx, block.Term.IP)
		}
		return m.ret(ctx, block.Term.IP)
	case jit.TerminateComplete:
		return m.complete(ctx)
	case jit.TerminateFallback:
		return m.exit(ctx, block.Term.IP, prof.ExitTraceCut, prof.OpcodeNone)
	case jit.TerminateBridge:
		return m.bridge(ctx, block.Term.IP)
	default:
		return false
	}
}

func (m machine) conditional(ctx *lowering, block jit.Block, tail []int) bool {
	if len(block.Term.Edges) != 2 || ctx.count() < 1 || !m.kinds(ctx, types.KindI32, 1) {
		return false
	}
	cond := ctx.pop()
	if block.Term.Hot >= 0 && block.Term.Hot < len(block.Term.Edges) {
		cold := 1 - block.Term.Hot
		clean := m.clean(ctx)
		if !clean && !m.flush(ctx, flushSnapshot) {
			return false
		}
		target := block.Term.Edges[cold]
		label, ok := m.label(ctx, target, appendTail(target.Tail, tail), int(instr.BR_IF))
		if !ok {
			return false
		}
		if block.Term.Hot == 1 {
			ctx.assembler.Emit(arm64.CBNZLabel(narrow32(cond.reg), label))
		} else {
			ctx.assembler.Emit(arm64.CBZLabel(narrow32(cond.reg), label))
		}
		return m.next(ctx, block.Anchor, block.Term.Edges[block.Term.Hot], tail, int(instr.BR_IF))
	}

	if !m.flush(ctx, flushSnapshot) {
		return false
	}
	taken := ctx.assembler.Label()
	ctx.assembler.Emit(arm64.CBNZLabel(narrow32(cond.reg), taken))
	if !m.path(ctx, block.Anchor, block.Term.Edges[1], tail, int(instr.BR_IF)) {
		return false
	}
	ctx.assembler.Bind(taken)
	return m.path(ctx, block.Anchor, block.Term.Edges[0], tail, int(instr.BR_IF))
}

func (m machine) next(ctx *lowering, from jit.Anchor, target jit.Edge, tail []int, opcode int) bool {
	tail = appendTail(target.Tail, tail)
	target.Tail = nil
	if target.Anchor.Addr == from.Addr && target.Anchor.IP <= from.IP {
		if !m.flush(ctx, flushCommit) {
			return false
		}
		if ctx.nativeLoop && target.Index == ctx.loopRoot && len(ctx.frames) == 1 && ctx.count() == 0 {
			if ctx.hoist.live {
				ctx.assembler.Emit(asm.Instruction{Op: asm.OpPseudoUse, Src1: asm.Virtual(ctx.hoist.dataPtr), Src2: asm.Virtual(ctx.hoist.n)})
			}
			return m.back(ctx, ctx.back, target.Anchor.IP)
		}
		return m.path(ctx, from, target, tail, opcode)
	}
	if target.Index == jit.NoBlock {
		reason := prof.ExitColdBranch
		if ctx.kind == jit.EntryLoop {
			reason = prof.ExitLoop
		}
		return m.exit(ctx, target.Anchor.IP, reason, opcode)
	}
	return m.emitBlock(ctx, target.Index, tail)
}

// clean reports whether a branch can skip the hot-path flush: no live operand
// or dirty local will be reloaded from VM stack slots later in the trace.
func (m machine) clean(ctx *lowering) bool {
	if ctx.count() != 0 {
		return false
	}
	for fi := range ctx.frames {
		f := &ctx.frames[fi]
		for idx := range f.state {
			if f.state[idx]&localDirty != 0 && m.carried(ctx, f.base+idx) == nil {
				return false
			}
		}
	}
	return true
}

func (m machine) table(ctx *lowering, block jit.Block, tail []int) bool {
	if len(block.Term.Edges) == 0 || len(block.Term.Edges)-1 > branchTableLimit || ctx.count() < 1 || !m.kinds(ctx, types.KindI32, 1) {
		return false
	}
	cond := ctx.pop()
	value := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.ANDI(value, cond.reg, maskI32))
	if !m.flush(ctx, flushSnapshot) {
		return false
	}
	labels := make([]asm.Label, len(block.Term.Edges))
	for idx := range labels {
		labels[idx] = ctx.assembler.Label()
	}
	for idx := 0; idx < len(labels)-1; idx++ {
		ctx.assembler.Emit(arm64.CMPI(value, uint16(idx)))
		ctx.assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, labels[idx]))
	}
	ctx.assembler.Emit(arm64.BLabel(labels[len(labels)-1]))
	for idx, label := range labels {
		ctx.assembler.Bind(label)
		if !m.path(ctx, block.Anchor, block.Term.Edges[idx], tail, int(instr.BR_TABLE)) {
			return false
		}
	}
	return true
}

func (m machine) follow(ctx *lowering, tail []int) bool {
	if len(tail) == 0 {
		return true
	}
	if !m.flush(ctx, flushSnapshot) {
		return false
	}
	id := tail[0]
	if id < 0 || id >= len(ctx.blocks) || !ctx.blocks[id].Tail {
		return false
	}
	values, frames := ctx.snapshot()
	label, _ := m.schedule(ctx, work{block: id, tail: tail[1:], values: values, frames: frames}, 0)
	ctx.assembler.Emit(arm64.BLabel(label))
	return true
}

func (m machine) path(ctx *lowering, from jit.Anchor, target jit.Edge, tail []int, opcode int) bool {
	tail = appendTail(target.Tail, tail)
	target.Tail = nil
	label, ok := m.label(ctx, target, tail, opcode)
	if !ok {
		return false
	}
	if target.Anchor.Addr == from.Addr && target.Anchor.IP <= from.IP {
		return m.back(ctx, label, target.Anchor.IP)
	}
	ctx.assembler.Emit(arm64.BLabel(label))
	return true
}

// back decrements the safepoint budget and continues at label while work remains.
// Native loops keep the budget in a register; chained loops update its VM slot.
func (m machine) back(ctx *lowering, label asm.Label, resume int) bool {
	a := ctx.assembler
	vCtrl := ctx.pin(scratchCtrl)
	budget := ctx.budget
	if budget.Width() == asm.WidthUndefined {
		budget = a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDR(budget, vCtrl, int16(journal.CellBudget*8)))
	}
	a.Emit(arm64.SUBI(budget, budget, 1))
	if ctx.budget.Width() == asm.WidthUndefined {
		a.Emit(arm64.STR(budget, vCtrl, int16(journal.CellBudget*8)))
	}
	a.Emit(arm64.CBNZLabel(budget, label))
	if ctx.budget.Width() != asm.WidthUndefined {
		a.Emit(arm64.STR(budget, vCtrl, int16(journal.CellBudget*8)))
	}
	if !m.commitCarried(ctx) {
		return false
	}
	m.trapFlushed(ctx, journal.TrapYield, resume, -1)
	return true
}

func (m machine) label(ctx *lowering, target jit.Edge, tail []int, opcode int) (asm.Label, bool) {
	if target.Index == jit.NoBlock {
		reason := prof.ExitColdBranch
		if ctx.kind == jit.EntryLoop {
			reason = prof.ExitLoop
		}
		return ctx.queueExit(nil, target.Anchor.IP, reason, opcode), true
	}
	if target.Index < 0 || target.Index >= len(ctx.blocks) {
		return 0, false
	}
	block := ctx.blocks[target.Index]
	if block.State != nil {
		return ctx.labels[target.Index], true
	}
	if m.marked(ctx) {
		return ctx.queueExit(nil, target.Anchor.IP, prof.ExitColdBranch, opcode), true
	}
	values, frames := ctx.snapshot()
	label, ok := m.schedule(ctx, work{block: target.Index, tail: tail, values: values, frames: frames}, workLimit)
	if !ok {
		return ctx.queueExit(nil, target.Anchor.IP, prof.ExitColdBranch, opcode), true
	}
	return label, true
}

func (m machine) schedule(ctx *lowering, next work, limit int) (asm.Label, bool) {
	for _, prior := range ctx.work {
		if m.same(prior, next) {
			return prior.label, true
		}
	}
	if limit > 0 && len(ctx.work) >= limit {
		return 0, false
	}
	next.label = ctx.assembler.Label()
	ctx.work = append(ctx.work, next)
	return next.label, true
}

// same reports whether two work items resume the same canonical state.
// Scheduling resets register allocation and transient local flags.
func (m machine) same(a, b work) bool {
	if a.block != b.block || !slices.Equal(a.tail, b.tail) || !slices.Equal(a.values, b.values) || len(a.frames) != len(b.frames) {
		return false
	}
	for i := range a.frames {
		x, y := a.frames[i], b.frames[i]
		if x.addr != y.addr || x.base != y.base || x.opBase != y.opBase || x.end != y.end || x.returns != y.returns ||
			len(x.locals) != len(y.locals) || len(x.upvals) != len(y.upvals) {
			return false
		}
	}
	return true
}

// marked reports whether a constant ref marker blocks deferred continuation
// reload. reload() already reconstructs a slot-backed deferred value
// correctly (flush wrote its boxed content to the operand's VM stack slot
// with no retain, and backing/slot survive the round trip unchanged), so only
// jit.BackingConst — whose compile-time fn/ref identity a plain reload cannot
// reconstruct — must force a branch to fall back to queueExit instead of a
// learned continuation.
func (m machine) marked(ctx *lowering) bool {
	for _, v := range ctx.values {
		if v.backing == jit.BackingConst {
			return true
		}
	}
	return false
}

// reload pulls operands back from VM stack slots after a call or continuation.
func (m machine) reload(ctx *lowering) {
	a := ctx.assembler
	if len(ctx.values) == 0 {
		return
	}
	vStack := ctx.pin(scratchStack)
	addr := m.base(ctx, vStack)
	for j := range ctx.values {
		v := &ctx.values[j]
		if v.backing == jit.BackingConst {
			continue
		}
		reg := a.Reg(asm.RegTypeInt, asm.Width64)
		a.Emit(arm64.LDR(reg, addr, int16(ctx.slot(j)*8)))
		v.reg = reg
		if v.kind != types.KindRef {
			if v.kind == types.KindI64 {
				v.reg = m.sign64(ctx, reg)
			}
			v.raw = true
		}
	}
}

// flush writes dirty locals and live operands to their VM stack slots in
// boxed form. Snapshot flushes remember fixed local homes so later guards do
// not repeat unchanged local stores; definitions clear that mark. Operands are
// always stored because stack operations may move them to a different home.
func (m machine) flush(ctx *lowering, mode flushMode) bool {
	// A committing flush transfers each jit.BackingStack ref's retain to the VM
	// stack, but it has no cold stub to re-take a deferred ref's retain: the
	// loop-backedge yield can reach the threaded interpreter directly. Reject
	// any live deferred ref up front, before emitting or mutating anything, so
	// a loop-carried deferred ref keeps the trace threaded.
	if mode == flushCommit {
		for _, v := range ctx.values {
			if v.kind == types.KindRef && v.backing != jit.BackingStack {
				return false
			}
		}
	}
	a := ctx.assembler
	var addr asm.VReg
	for fi := range ctx.frames {
		f := &ctx.frames[fi]
		for idx := range f.kinds {
			if f.state[idx]&localDirty == 0 {
				continue
			}
			if m.carried(ctx, f.base+idx) != nil {
				continue
			}
			if f.state[idx]&localStored == 0 {
				boxed, ok := m.box(ctx, f.locals[idx])
				if !ok {
					return false
				}
				if addr.Width() == asm.WidthUndefined {
					addr = m.base(ctx, ctx.pin(scratchStack))
				}
				a.Emit(arm64.STR(boxed, addr, int16((f.base+idx)*8)))
				f.state[idx] |= localStored
			}
			if mode == flushCommit {
				f.state[idx] &^= localDirty | localStored
			}
		}
	}
	// A jit.BackingStack ref carries the retain taken when it was pushed, so
	// committing it transfers that edge to the stack. A non-commit flush writes
	// each deferred ref boxed WITHOUT a retain; a following retainDeferred or exit stub
	// re-takes it before the flushed copy reaches the interpreter (see retainDeferred /
	// emitExits). The commit pre-scan above already rejected any deferred
	// backing, so those cases only run on a non-commit flush.
	for j, v := range ctx.values {
		if addr.Width() == asm.WidthUndefined {
			addr = m.base(ctx, ctx.pin(scratchStack))
		}
		switch v.backing {
		case jit.BackingStack:
			boxed, ok := m.box(ctx, v)
			if !ok {
				return false
			}
			a.Emit(arm64.STR(boxed, addr, int16(ctx.slot(j)*8)))
		case jit.BackingConst:
			boxed := a.Reg(asm.RegTypeInt, asm.Width64)
			a.Emit(arm64.LDI(boxed, uint64(types.BoxRef(v.ref)))...)
			a.Emit(arm64.STR(boxed, addr, int16(ctx.slot(j)*8)))
		default:
			a.Emit(arm64.STR(v.reg, addr, int16(ctx.slot(j)*8)))
		}
	}
	return true
}

// carry loads each eligible root-frame local before the loop label and keeps
// its register authoritative until a cold handoff commits it to the VM slot.
func (m machine) carry(ctx *lowering, locals []int, ip int) bool {
	regs := [...]asm.PReg{arm64.X19, arm64.X20, arm64.X21, arm64.X22, arm64.X23, arm64.X24, arm64.X25}
	if len(locals) > len(regs) {
		return false
	}
	f := ctx.frame()
	for idx, local := range locals {
		if local < 0 || local >= len(f.kinds) || !m.loadLocal(ctx, f, local, ip) {
			return false
		}
		pinned := ctx.pinTo(regs[idx])
		ctx.assembler.Emit(arm64.MOV(pinned, f.locals[local].reg))
		f.locals[local].reg = pinned
		ctx.carried = append(ctx.carried, carriedLocal{
			value: f.locals[local],
			local: local,
			slot:  f.base + local,
		})
	}
	return true
}

// commitCarried writes the authoritative loop registers to their VM homes.
// Callers place it only on paths that return control to the interpreter.
func (m machine) commitCarried(ctx *lowering) bool {
	if len(ctx.carried) == 0 {
		return true
	}
	addr := m.base(ctx, ctx.pin(scratchStack))
	for _, carried := range ctx.carried {
		boxed, ok := m.box(ctx, carried.value)
		if !ok {
			return false
		}
		ctx.assembler.Emit(arm64.STR(boxed, addr, int16(carried.slot*8)))
	}
	return true
}

func (machine) carried(ctx *lowering, slot int) *carriedLocal {
	for idx := range ctx.carried {
		if ctx.carried[idx].slot == slot {
			return &ctx.carried[idx]
		}
	}
	return nil
}

// clearLocals invalidates ordinary local caches while preserving root-loop
// registers whose values no longer come from their VM slots.
func (m machine) clearLocals(ctx *lowering) {
	for idx := range ctx.frames {
		clear(ctx.frames[idx].state)
	}
	if len(ctx.frames) == 0 {
		return
	}
	f := &ctx.frames[0]
	for _, carried := range ctx.carried {
		f.locals[carried.local] = carried.value
		f.state[carried.local] = localLoaded | localDirty
	}
}

// hoist derives the plan's loop-invariant container once per native entry:
// the container local is loaded, tag- and itab-guarded against an empty-stack
// exit resuming at the loop header, and its slice header lands in registers
// that stay live across the loop back-edge. hoistable rejects every call, and
// OpPseudoUse extends both live ranges through the backward branch. The
// prologue takes no retain: the local slot keeps carrying the container's
// refcount, and the exit snapshot is empty. Unsupported layouts decline
// silently and every access keeps its per-op derivation.
func (m machine) hoist(ctx *lowering, h jit.Hoist, resume int) bool {
	switch h.Want {
	case jit.HeapArrayI1, jit.HeapArrayI8, jit.HeapArrayI32, jit.HeapArrayI64, jit.HeapArrayF32, jit.HeapArrayF64:
	default:
		return true
	}
	f := ctx.frame()
	slot := f.base + h.Local
	if h.Local >= len(f.kinds) || f.kinds[h.Local] != types.KindRef || slot > jit.MaxHoistSlot {
		return true
	}
	fail, ok := m.sideExit(ctx, ctx.values, resume, prof.ExitGuardShape, ctx.opcode(resume))
	if !ok {
		return false
	}
	addr := m.base(ctx, ctx.pin(scratchStack))
	ref := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
	ctx.assembler.Emit(arm64.LDR(ref, addr, int16(slot*8)))
	_, itab, data := m.guardHeap(ctx, ref, fail)
	m.guardItab(ctx, itab, h.Want, fail)
	ctx.hoist.dataPtr, ctx.hoist.n = m.sliceHeader(ctx, data, 0)
	ctx.hoist.slot, ctx.hoist.want, ctx.hoist.live = slot, h.Want, true
	return true
}

func appendTail(steps, tail []int) []int {
	if len(steps) == 0 {
		return tail
	}
	if len(tail) == 0 {
		return steps
	}
	return append(append([]int(nil), steps...), tail...)
}
