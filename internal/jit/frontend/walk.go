package frontend

import (
	"unsafe"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

// walk translates one span into one block: it decodes each instruction, emits
// the operations that perform it, and carries the abstract operand stack the
// next instruction reads. One walk serves both passes - the fixpoint keeps only
// the facts it leaves behind, the build keeps the block it filled.
type walk struct {
	facts
	b     *ssa.Builder
	block int
	stack []operand

	ip    int
	pre   []ssa.Value
	state ssa.Value
}

// run fills the block with s and returns the terminator it ends on, carrying no
// edges: which blocks those name is the caller's to resolve, because a span
// knows its successors only as spans.
func (w *walk) run(s span) (ssa.Terminator, bool) {
	for ip := s.start; ip < s.end; {
		inst := instr.Instruction(w.fn.Code[ip:])
		w.begin(ip)
		switch inst.Opcode() {
		case instr.BR:
			return ssa.Terminator{Op: ssa.OpJump}, true
		case instr.BR_IF, instr.BR_TABLE:
			if len(w.stack) == 0 {
				return ssa.Terminator{}, false
			}
			args := []ssa.Value{w.stack[len(w.stack)-1].value}
			w.stack = w.stack[:len(w.stack)-1]
			if inst.Opcode() == instr.BR_TABLE {
				return ssa.Terminator{Op: ssa.OpTable, Args: args}, true
			}
			return ssa.Terminator{Op: ssa.OpBranch, Args: args}, true
		case instr.RETURN:
			if len(w.stack) < w.returns() {
				return ssa.Terminator{}, false
			}
			return w.leave(), true
		}
		if !w.perform(inst) {
			return ssa.Terminator{}, false
		}
		ip += inst.Width()
	}
	// A span that leaves anywhere continues there: the next block's, or the one
	// a bridge resumes into. Otherwise the code has run out, which ends module
	// code by advancing past it and ends a function by returning.
	if len(s.succs) > 0 {
		return ssa.Terminator{Op: ssa.OpJump}, true
	}
	if w.addr == 0 {
		return ssa.Terminator{Op: ssa.OpComplete}, true
	}
	return w.leave(), true
}

// edges resolves a span's successors into the edges its terminator names, each
// handing the successor's parameters the operands live here. A reference the
// successor holds owned is owned here first; two successors that disagree about
// one operand cannot both be served from a single block, so the function is
// left unplanned rather than retained on a path that never releases it.
func (w *walk) edges(s span, states [][]fact, ids []int) ([]ssa.Edge, bool) {
	if len(s.succs) == 0 {
		return nil, true
	}
	for _, succ := range s.succs {
		if len(states[succ]) != len(w.stack) {
			return nil, false
		}
	}
	for i := range w.stack {
		owned, borrowed := false, false
		for _, succ := range s.succs {
			if states[succ][i].backing == jit.BackingStack {
				owned = true
			} else {
				borrowed = true
			}
		}
		if owned && borrowed {
			return nil, false
		}
		if owned {
			w.own(i)
		}
	}
	edges := make([]ssa.Edge, len(s.succs))
	for i, succ := range s.succs {
		args := make([]ssa.Value, len(w.stack))
		for j, o := range w.stack {
			args[j] = o.value
		}
		edges[i] = ssa.Edge{Block: ids[succ], Args: args}
	}
	return edges, true
}

// perform translates one instruction. The cases below are the opcodes whose
// effect instr cannot state on its own - a slot, a compile-time value, a stack
// shuffle, an arity read off the stack, or a kind only the snapshot resolves;
// every other opcode is performed from its declared stack effect.
func (w *walk) perform(inst instr.Instruction) bool {
	op := inst.Opcode()
	switch op {
	case instr.NOP, instr.UNREACHABLE:
		return true

	case instr.LOCAL_GET:
		return w.load(ssa.SpaceLocal, int(inst.Operand(0)))
	case instr.UPVAL_GET:
		return w.load(ssa.SpaceUpval, int(inst.Operand(0)))
	case instr.GLOBAL_GET:
		return w.load(ssa.SpaceGlobal, int(inst.Operand(0)))
	case instr.LOCAL_SET, instr.LOCAL_TEE:
		return w.store(ssa.SpaceLocal, int(inst.Operand(0)), op == instr.LOCAL_SET)
	case instr.GLOBAL_SET, instr.GLOBAL_TEE:
		return w.store(ssa.SpaceGlobal, int(inst.Operand(0)), op == instr.GLOBAL_SET)
	case instr.UPVAL_SET:
		return w.store(ssa.SpaceUpval, int(inst.Operand(0)), true)

	case instr.CONST_GET:
		return w.pool(int(inst.Operand(0)))
	case instr.I32_CONST:
		val := int32(inst.Operand(0))
		return w.constant(types.BoxI32(val), fact{kind: types.KindI32, val: val, valKnown: true})
	case instr.I64_CONST:
		val := int64(inst.Operand(0))
		boxed := types.BoxI64(val)
		if boxed.I64() != val {
			return false
		}
		return w.constant(boxed, fact{kind: types.KindI64})
	case instr.F32_CONST:
		return w.constant(types.Box(uint64(uint32(inst.Operand(0))), types.KindF32), fact{kind: types.KindF32})
	case instr.F64_CONST:
		return w.constant(types.Boxed(inst.Operand(0)), fact{kind: types.KindF64})
	case instr.REF_NULL:
		return w.constant(types.BoxedNull, fact{kind: types.KindRef})

	case instr.DUP:
		// A borrowed duplicate still borrows its own backing storage; an owned
		// one takes a retain of its own, because two stack copies release twice.
		if len(w.stack) == 0 {
			return false
		}
		top := w.stack[len(w.stack)-1]
		if top.kind == types.KindRef && top.backing == jit.BackingStack {
			w.retain(top.value)
		}
		w.stack = append(w.stack, top)
		return true
	case instr.SWAP:
		if len(w.stack) < 2 {
			return false
		}
		n := len(w.stack)
		w.stack[n-1], w.stack[n-2] = w.stack[n-2], w.stack[n-1]
		return true
	case instr.DROP:
		if len(w.stack) == 0 {
			return false
		}
		w.release(w.stack[len(w.stack)-1])
		w.stack = w.stack[:len(w.stack)-1]
		return true
	case instr.SELECT:
		if len(w.stack) < 3 {
			return false
		}
		n := len(w.stack)
		if w.stack[n-2].kind != w.stack[n-3].kind {
			return false
		}
		return w.exec(op, 3, []fact{{kind: w.stack[n-2].kind}})

	case instr.ARRAY_GET, instr.ARRAY_DELETE:
		if len(w.stack) < 2 {
			return false
		}
		kind, ok := w.elem(w.stack[len(w.stack)-2].fact)
		if !ok {
			return false
		}
		if op == instr.ARRAY_GET {
			if shape, ok := jit.ElemShapeByKind(kind); ok {
				w.guard(len(w.stack)-2, ssa.Shape{Itab: shape.Itab})
			}
		}
		return w.exec(op, 2, []fact{{kind: kind}})
	case instr.STRUCT_GET:
		if len(w.stack) < 2 {
			return false
		}
		kind, ok := w.field(w.stack[len(w.stack)-2].fact, w.stack[len(w.stack)-1].fact)
		if !ok {
			return false
		}
		if record := w.record(w.stack[len(w.stack)-2].fact); record != nil {
			w.guard(len(w.stack)-2, ssa.Shape{Itab: jit.HeapStruct, Typ: uintptr(unsafe.Pointer(record))})
		}
		return w.exec(op, 2, []fact{{kind: kind}})
	case instr.REF_CAST:
		// A successful cast validates the operand's declared type in place and
		// leaves the same value; only what is known about it narrows.
		if len(w.stack) == 0 {
			return false
		}
		cast := fact{kind: w.stack[len(w.stack)-1].kind}
		if idx := int(inst.Operand(0)); idx < len(w.decl) {
			cast.styp, _ = w.decl[idx].(*types.StructType)
			cast.atyp, _ = w.decl[idx].(*types.ArrayType)
		}
		return w.exec(op, 1, []fact{cast})

	case instr.CALL, instr.RETURN_CALL:
		if len(w.stack) == 0 {
			return false
		}
		callee := w.stack[len(w.stack)-1].fact
		if !callee.calleeKnown || callee.callee <= 0 {
			return false
		}
		target := w.objects.Function(callee.callee)
		if target == nil || target.Typ == nil {
			return false
		}
		var results []fact
		if op == instr.CALL {
			results = make([]fact, len(target.Typ.Returns))
			for i, t := range target.Typ.Returns {
				results[i] = fact{kind: t.Kind()}
			}
		}
		return w.exec(op, 1+len(target.Typ.Params), results)
	case instr.CLOSURE_NEW:
		// A closure pops its captures plus the function reference below them, an
		// arity only a resolved heap function states.
		if len(w.stack) == 0 {
			return false
		}
		capture := w.stack[len(w.stack)-1].fact
		if !capture.refKnown || capture.ref <= 0 {
			return false
		}
		target := w.objects.Function(capture.ref)
		if target == nil {
			return false
		}
		return w.exec(op, 1+len(target.Captures), []fact{{kind: types.KindRef}})
	case instr.STRUCT_NEW:
		// A struct pops one value per field of its declared type, an arity that
		// is on neither the stack nor the operand.
		idx := int(inst.Operand(0))
		if idx >= len(w.decl) {
			return false
		}
		record, ok := w.decl[idx].(*types.StructType)
		if !ok {
			return false
		}
		return w.exec(op, len(record.Fields), []fact{{kind: types.KindRef, styp: record}})
	case instr.ARRAY_NEW, instr.MAP_NEW, instr.ARRAY_APPEND:
		// These read their count as a runtime i32 on top of the values they take,
		// so their effect is knowable only where that count is a known constant.
		if len(w.stack) == 0 {
			return false
		}
		top := w.stack[len(w.stack)-1].fact
		if top.kind != types.KindI32 || !top.valKnown || top.val < 0 {
			return false
		}
		count := int(top.val)
		switch op {
		case instr.MAP_NEW:
			return w.exec(op, 1+count*2, []fact{{kind: types.KindRef}})
		case instr.ARRAY_APPEND:
			// The array below the values is never popped, so it survives - as a
			// fresh operand, because the bridge handed the old one away.
			return w.exec(op, 2+count, []fact{{kind: types.KindRef}})
		default:
			return w.exec(op, 1+count, []fact{{kind: types.KindRef}})
		}
	}

	effect := instr.TypeOf(op)
	if effect.Pop == nil && effect.Push == nil {
		return false
	}
	results := make([]fact, len(effect.Push))
	for i, kind := range effect.Push {
		if kind == instr.KindAny {
			return false
		}
		results[i] = fact{kind: types.Kind(kind)}
	}
	return w.exec(op, len(effect.Pop), results)
}

// load pushes what a slot holds. A ref takes no retain and records the slot its
// count is deferred to, so a container loop pays no retain and release per
// element.
func (w *walk) load(space ssa.Space, index int) bool {
	var out fact
	switch space {
	case ssa.SpaceLocal:
		if index >= len(w.slots) {
			return false
		}
		out = holds(w.slots[index])
	case ssa.SpaceUpval:
		if index >= len(w.fn.Captures) {
			return false
		}
		out = holds(w.fn.Captures[index])
	default:
		if index >= len(w.globals) {
			return false
		}
		out = fact{kind: w.globals[index]}
	}
	out.backing, out.offset = backing(space), index
	t, ok := typ(out.kind)
	if !ok {
		return false
	}
	value := w.b.Value(t)
	w.b.Add(w.block, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: space, Index: index}, Results: []ssa.Value{value}})
	w.push(value, out)
	return true
}

// store writes the top operand into a slot, which releases whatever it replaces.
// A ref hands its retain to the slot, so a borrowed one is owned first and every
// other operand borrowed from that slot is owned before its content changes
// underneath it. A tee leaves the value on the stack, which needs a retain of
// its own.
func (w *walk) store(space ssa.Space, index int, pop bool) bool {
	if len(w.stack) == 0 {
		return false
	}
	top := len(w.stack) - 1
	if w.stack[top].kind == types.KindRef {
		w.own(top)
		w.detach(backing(space), index)
		if !pop {
			w.retain(w.stack[top].value)
		}
	}
	w.b.Add(w.block, ssa.Operation{
		Op:    ssa.OpStore,
		Slot:  ssa.Slot{Space: space, Index: index},
		Args:  []ssa.Value{w.stack[top].value},
		State: w.deopt(),
	})
	if pop {
		w.stack = w.stack[:top]
	}
	return true
}

// pool pushes a constant. A ref constant is an ownership-neutral marker whose
// retain stays with the pool, and one naming a heap function is a call target
// the backend can reach without a recorded trace.
func (w *walk) pool(index int) bool {
	if index >= len(w.constants) {
		return false
	}
	boxed := w.constants[index]
	out := fact{kind: boxed.Kind()}
	if out.kind == types.KindRef {
		out.backing = jit.BackingConst
		out.ref, out.refKnown = boxed.Ref(), true
		if out.ref > 0 && w.objects.Function(out.ref) != nil {
			out.callee, out.calleeKnown = out.ref, true
		}
	}
	return w.constant(boxed, out)
}

// constant pushes a compile-time value.
func (w *walk) constant(boxed types.Boxed, out fact) bool {
	t, ok := typ(out.kind)
	if !ok {
		return false
	}
	value := w.b.Value(t)
	w.b.Add(w.block, ssa.Operation{Op: ssa.OpConst, Const: boxed, Results: []ssa.Value{value}})
	w.push(value, out)
	return true
}

// exec performs op over the top pops operands, leaving results in their place.
// An opcode the backend cannot lower bridges instead, running in the interpreter
// and resuming here.
//
// Ownership follows what the operation does with a reference: one it hands to
// storage that outlives the operand - a container it stores into, a callee, the
// interpreter's own flushed stack - is owned first and never released here; one
// it merely reads is released when this stack copy is what owned it. A produced
// reference is owned, which is what a retained element or a returned value is.
func (w *walk) exec(op instr.Opcode, pops int, results []fact) bool {
	if len(w.stack) < pops {
		return false
	}
	// instr states a fixed arity for array.new that only approximates its real
	// one, so the arguments follow what it declares and the operands beyond them
	// reach the interpreter through the deopt state a bridge always carries.
	effect := instr.TypeOf(op)
	named := pops
	if effect.Pop != nil || effect.Push != nil {
		named = len(effect.Pop)
	}
	if named > pops {
		return false
	}
	// An operand of a kind the opcode cannot pop leaves the function unplanned.
	// program.Verify rejects such bytecode before it ever runs, so this only
	// declines to build an operation that could not be typed anyway. i1 and i8
	// stand wherever the i32 they compute as is wanted.
	for i, want := range effect.Pop {
		got := instr.Kind(w.stack[len(w.stack)-1-i].kind)
		if want != instr.KindAny && got.Repr() != want.Repr() {
			return false
		}
	}
	out := make([]ssa.Value, len(results))
	for i, r := range results {
		t, ok := typ(r.kind)
		if !ok {
			return false
		}
		out[i] = w.b.Value(t)
	}

	bridged := jit.Bridgeable(op)
	adopted := 0
	switch {
	case bridged || op.Writes(instr.Frame):
		// The flushed operand stack is what a bridge and a call hand over, and
		// whoever receives it adopts every reference on it.
		w.adopt()
		adopted = pops
	case op.Reads(instr.Heap) && op.Writes(instr.Heap):
		w.own(len(w.stack) - 1)
		adopted = 1
	}
	args := make([]ssa.Value, named)
	for i := range args {
		args[i] = w.stack[len(w.stack)-named+i].value
	}
	consumed := append([]operand(nil), w.stack[len(w.stack)-pops:]...)
	w.stack = w.stack[:len(w.stack)-pops]

	kind := ssa.OpExec
	if bridged {
		kind = ssa.OpBridge
	}
	operation := ssa.Operation{Op: kind, Code: op, Args: args, Results: out}
	if bridged || op.Writes(instr.Frame) || (op.Reads(instr.Heap) && op.Writes(instr.Heap)) {
		operation.State = w.deopt()
	}
	w.b.Add(w.block, operation)

	for i, r := range results {
		w.push(out[i], r)
	}
	for i := 0; i < len(consumed)-adopted; i++ {
		w.release(consumed[i])
	}
	return true
}

// leave ends the function with the results its type declares. A borrowed one is
// owned first, because the caller adopts what it is handed.
func (w *walk) leave() ssa.Terminator {
	n := w.returns()
	if n > len(w.stack) {
		n = len(w.stack)
	}
	args := make([]ssa.Value, 0, n)
	for i := len(w.stack) - n; i < len(w.stack); i++ {
		w.own(i)
		args = append(args, w.stack[i].value)
	}
	return ssa.Terminator{Op: ssa.OpReturn, Args: args}
}

// guard admits only a container of the shape the plan resolved for it, so the
// access after it may load through that shape. It refines the operand in place:
// everything after reads the guarded value, while the state the guard resumes
// into still names the operands the opcode started with.
func (w *walk) guard(at int, shape ssa.Shape) {
	if w.stack[at].kind != types.KindRef || shape == (ssa.Shape{}) {
		return
	}
	value := w.b.Value(ssa.TypeRef)
	w.b.Add(w.block, ssa.Operation{
		Op:      ssa.OpGuardShape,
		Shape:   shape,
		Args:    []ssa.Value{w.stack[at].value},
		State:   w.deopt(),
		Results: []ssa.Value{value},
	})
	w.stack[at].value = value
}

// adopt owns every live reference before control leaves native code with the
// operand stack flushed: the interpreter and a callee both adopt what they find
// there, and would otherwise release a reference this frame never retained.
func (w *walk) adopt() {
	for i := range w.stack {
		w.own(i)
	}
}

// detach owns every operand still borrowed from the slot about to be
// overwritten. A borrowed operand left alone would keep pointing at a slot whose
// content no longer matches what it observed.
func (w *walk) detach(from jit.Backing, offset int) {
	for i := range w.stack {
		if w.stack[i].backing == from && w.stack[i].offset == offset {
			w.own(i)
		}
	}
}

// own takes the retain that moves a borrowed reference's ownership onto the
// operand stack. What is known about the value survives: only where its count
// lives has changed.
func (w *walk) own(at int) {
	o := &w.stack[at]
	if o.kind != types.KindRef || o.backing == jit.BackingStack {
		return
	}
	w.retain(o.value)
	o.backing, o.offset = jit.BackingStack, 0
}

// retain adds the reference count a new owner holds.
func (w *walk) retain(value ssa.Value) {
	w.b.Add(w.block, ssa.Operation{Op: ssa.OpRetain, Args: []ssa.Value{value}})
}

// release drops the count an operand owned, and does nothing for one that
// borrowed it from storage still holding its own.
func (w *walk) release(o operand) {
	if o.kind != types.KindRef || o.backing != jit.BackingStack {
		return
	}
	w.b.Add(w.block, ssa.Operation{Op: ssa.OpRelease, Args: []ssa.Value{o.value}, State: w.deopt()})
}

// begin starts one instruction, recording the operands a deopt from it resumes
// with. They are the operands before it ran, because that is where the
// interpreter picks the opcode up.
func (w *walk) begin(ip int) {
	w.ip, w.state = ip, ssa.NoValue
	w.pre = make([]ssa.Value, len(w.stack))
	for i, o := range w.stack {
		w.pre[i] = o.value
	}
}

// deopt materializes the interpreter state the current instruction resumes
// into. One instruction needs at most one, so a guard and the access it admits
// share it.
func (w *walk) deopt() ssa.Value {
	if w.state != ssa.NoValue {
		return w.state
	}
	w.state = w.b.Value(ssa.TypeState)
	w.b.Add(w.block, ssa.Operation{
		Op:      ssa.OpState,
		Frames:  []ssa.Frame{{Addr: w.addr, IP: w.ip, Returns: w.returns(), Stack: w.pre}},
		Results: []ssa.Value{w.state},
	})
	return w.state
}

// push adds one operand.
func (w *walk) push(value ssa.Value, out fact) {
	w.stack = append(w.stack, operand{value: value, fact: out})
}

// returns is how many results the function hands back.
func (w *walk) returns() int {
	if w.fn.Typ == nil {
		return 0
	}
	return len(w.fn.Typ.Returns)
}

// backing is where a value loaded from space derives its reference count.
func backing(space ssa.Space) jit.Backing {
	switch space {
	case ssa.SpaceGlobal:
		return jit.BackingGlobal
	case ssa.SpaceUpval:
		return jit.BackingUpval
	default:
		return jit.BackingLocal
	}
}
