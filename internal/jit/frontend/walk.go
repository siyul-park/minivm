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
	b      *ssa.Builder
	block  int
	frames []frame
	stack  []operand

	// seen is what a recording observed about the instruction being
	// translated: the container shape it ran against, the address the call it
	// performed entered, and the value it produced. It stays zero for a plan
	// built from bytecode alone, which resolves the same facts from constants
	// and declared types instead.
	seen jit.Step

	ip    int
	pre   []operand
	state ssa.Value
}

// run fills the block with s and returns the terminator it ends on, carrying no
// edges: which blocks those name is the caller's to resolve, because a span
// knows its successors only as spans.
func (w *walk) run(s span) (ssa.Terminator, bool) {
	code := w.frame().fn.Code
	for ip := s.start; ip < s.end; {
		inst := instr.Instruction(code[ip:])
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
		case instr.RETURN_CALL:
			return w.tail(ip)
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
	if w.frame().addr == 0 {
		return w.complete(), true
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

// join hands this block's operands to a successor whose parameters are already
// fixed. A reference the successor holds owned is owned here first; one it
// expects to borrow from storage this path owns cannot be handed over at all,
// so the recording is left unplanned rather than retained on a path that never
// releases it. Every other fact is a speculation the successor was laid out
// against and reads its parameters through, so a path that does not carry it
// cannot enter there either: unlike a fixpoint over a control-flow graph, a
// recording is laid out once and has no join to widen at.
func (w *walk) join(params []operand) ([]ssa.Value, bool) {
	if len(params) != len(w.stack) {
		return nil, false
	}
	args := make([]ssa.Value, len(w.stack))
	for i := range w.stack {
		want := params[i].fact
		if want.kind == types.KindRef && want.backing == jit.BackingStack {
			w.own(i)
		}
		got := w.stack[i].fact
		if got.kind != types.KindRef {
			// Only a reference derives a count from where it was loaded, so
			// where a scalar came from is no reason to refuse the edge.
			want.backing, want.offset = got.backing, got.offset
		}
		if changed, ok := want.merge(got); !ok || changed {
			return nil, false
		}
		args[i] = w.stack[i].value
	}
	return args, true
}

// perform translates one instruction. The cases below are the opcodes whose
// effect instr cannot state on its own - a slot, a compile-time value, a stack
// shuffle, an arity read off the stack, or a kind only the snapshot resolves;
// every other opcode is performed from its declared stack effect.
func (w *walk) perform(inst instr.Instruction) bool {
	op := inst.Opcode()
	switch op {
	case instr.NOP:
		return true
	case instr.UNREACHABLE:
		// instr states no stack effect for it, but it is not a no-op: reaching
		// it raises. The IR performs the operation so a backend has to answer
		// for it, instead of compiling a function that runs straight past it.
		return w.exec(op, 0, nil)

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
		shape, ok := w.elem(w.stack[len(w.stack)-2].fact)
		if !ok {
			return false
		}
		if op == instr.ARRAY_GET {
			w.guard(len(w.stack)-2, ssa.Shape{Itab: shape.Itab})
		}
		return w.exec(op, 2, []fact{{kind: shape.Kind}})
	case instr.STRUCT_GET:
		if len(w.stack) < 2 {
			return false
		}
		kind, shape, ok := w.field(w.stack[len(w.stack)-2].fact, w.stack[len(w.stack)-1].fact)
		if !ok {
			return false
		}
		w.guard(len(w.stack)-2, shape)
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

	case instr.CALL:
		if len(w.stack) == 0 {
			return false
		}
		_, target := w.callee(len(w.stack) - 1)
		if target == nil {
			return false
		}
		results := make([]fact, len(target.Typ.Returns))
		for i, t := range target.Typ.Returns {
			results[i] = fact{kind: t.Kind()}
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

// elem resolves the element shape one array access is compiled against: the
// container a recording observed, the cell the snapshot resolved for a known
// container, or the operand's declared array type, which answers only in a
// call-free function. All three are hints the shape guard verifies before any
// access, so a slot declared as an array that currently holds null or a
// differently shaped array deopts instead of being read.
func (w *walk) elem(array fact) (jit.ElemShape, bool) {
	if shape, ok := jit.ElemShapeByItab(w.seen.Shape.Itab); ok {
		return shape, true
	}
	if array.refKnown && array.ref > 0 {
		return jit.ElemShapeByItab(w.objects[array.ref].Array)
	}
	if w.declared && array.atyp != nil && array.atyp.ElemKind != instr.KindAny {
		return jit.ElemShapeByKind(array.atyp.ElemKind)
	}
	return jit.ElemShape{}, false
}

// field resolves a struct field's kind and the shape the access reading it is
// admitted through: the field a recording observed, or the one a container
// carrying a struct type answers for a known in-bounds constant index.
func (w *walk) field(container, index fact) (types.Kind, ssa.Shape, bool) {
	if w.seen.Shape.Itab != 0 {
		kind := w.seen.Seen.Kind()
		if _, ok := typ(kind); !ok {
			return 0, ssa.Shape{}, false
		}
		return kind, ssa.Shape{Itab: w.seen.Shape.Itab, Typ: w.seen.Shape.Typ, Host: w.seen.Shape.Field}, true
	}
	record := w.record(container)
	if record == nil || !index.valKnown || index.val < 0 || int(index.val) >= len(record.Fields) {
		return 0, ssa.Shape{}, false
	}
	return record.Fields[index.val].Kind, ssa.Shape{Itab: jit.HeapStruct, Typ: uintptr(unsafe.Pointer(record))}, true
}

// record resolves the struct type a container carries: the one its declared
// type or a ref.cast states, or the one the snapshot recorded for a constant
// cell.
func (w *walk) record(container fact) *types.StructType {
	if container.styp != nil {
		return container.styp
	}
	if container.refKnown && container.ref > 0 {
		return w.objects[container.ref].Typ
	}
	return nil
}

// callee resolves the function a call enters and pins its operand to the
// reference naming it, answering with the address that function is published
// at. A constant operand already names one; a recorded operand is a runtime
// value, so a guard admitting only the reference the recording observed is what
// makes it a compile-time fact. Everything reading the operand after that -
// this walk, and a lowering resolving the call's target - reads a value that
// certainly holds that reference, which is the one question a static call and a
// speculated one both answer through.
//
// The reference is the whole answer, so a call whose operand names anything the
// snapshot does not resolve to a function is left unplanned: a host function, a
// coroutine, and a closure allocated at runtime all name no function here.
func (w *walk) callee(at int) (int, *types.Function) {
	o := w.stack[at]
	ref, observed := o.ref, false
	if !o.refKnown {
		if o.kind != types.KindRef || w.seen.Seen.Kind() != types.KindRef {
			return 0, nil
		}
		ref, observed = w.seen.Seen.Ref(), true
	}
	if ref <= 0 {
		return 0, nil
	}
	target := w.objects.Function(ref)
	if target == nil || target.Typ == nil {
		return 0, nil
	}
	if observed {
		want := w.b.Value(ssa.TypeRef)
		w.b.Add(w.block, ssa.Operation{Op: ssa.OpConst, Const: types.BoxRef(ref), Results: []ssa.Value{want}})
		value := w.b.Value(ssa.TypeRef)
		w.b.Add(w.block, ssa.Operation{
			Op:      ssa.OpGuardValue,
			Args:    []ssa.Value{o.value, want},
			State:   w.deopt(),
			Results: []ssa.Value{value},
		})
		w.stack[at].value = value
	}
	return ref, target
}

// load pushes what a slot holds. A ref takes no retain and records the slot its
// count is deferred to, so a container loop pays no retain and release per
// element.
func (w *walk) load(space ssa.Space, index int) bool {
	slot, out, ok := w.addressed(space, index)
	if !ok {
		return false
	}
	t, ok := typ(out.kind)
	if !ok {
		return false
	}
	value := w.b.Value(t)
	w.b.Add(w.block, ssa.Operation{Op: ssa.OpLoad, Slot: slot, Results: []ssa.Value{value}})
	if out.kind == types.KindI64 {
		guarded := w.b.Value(t)
		w.b.Add(w.block, ssa.Operation{
			Op:      ssa.OpGuardKind,
			Args:    []ssa.Value{value},
			State:   w.deopt(),
			Results: []ssa.Value{guarded},
		})
		value = guarded
	}
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
	slot, out, ok := w.addressed(space, index)
	if !ok {
		return false
	}
	top := len(w.stack) - 1
	if w.stack[top].kind == types.KindRef {
		w.own(top)
		w.detach(out.backing, out.offset)
		if !pop {
			w.retain(w.stack[top].value)
		}
	}
	w.b.Add(w.block, ssa.Operation{
		Op:    ssa.OpStore,
		Slot:  slot,
		Args:  []ssa.Value{w.stack[top].value},
		State: w.deopt(),
	})
	if pop {
		w.stack = w.stack[:top]
	}
	return true
}

// addressed resolves the storage a slot opcode names and what is known about
// the value in it. A local belongs to the innermost frame, so its deferred
// reference count is named by the absolute slot an inlined frame's own local
// sits at, which is what keeps two frames' local zero apart.
func (w *walk) addressed(space ssa.Space, index int) (ssa.Slot, fact, bool) {
	fr := w.frame()
	slot := ssa.Slot{Space: space, Index: index}
	var out fact
	switch space {
	case ssa.SpaceLocal:
		if index >= len(fr.slots) {
			return slot, out, false
		}
		slot.Base = fr.base
		out = holds(fr.slots[index])
		out.backing, out.offset = jit.BackingLocal, fr.base+index
	case ssa.SpaceUpval:
		if index >= len(fr.fn.Captures) {
			return slot, out, false
		}
		out = holds(fr.fn.Captures[index])
		out.backing, out.offset = jit.BackingUpval, index
	default:
		if index >= len(w.globals) {
			return slot, out, false
		}
		out = fact{kind: w.globals[index], backing: jit.BackingGlobal, offset: index}
	}
	return slot, out, true
}

// pool pushes a constant. A ref constant is an ownership-neutral marker whose
// retain stays with the pool, and the reference it carries is what resolves the
// container it accesses or the function it calls without a recorded trace.
func (w *walk) pool(index int) bool {
	if index >= len(w.constants) {
		return false
	}
	boxed := w.constants[index]
	out := fact{kind: boxed.Kind()}
	if out.kind == types.KindRef {
		out.backing = jit.BackingConst
		out.ref, out.refKnown = boxed.Ref(), true
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
	// ssa.OverflowsI64 names the opcodes that can carry a result past the
	// boxed 49-bit payload. The five that lower today (ADD, SUB, MUL, SHL,
	// SHR_U) guard it in their own arm64 lowering and exit through this
	// state on overflow - which is w.pre as of this instruction's own start
	// (see begin/deopt), so the flush is both operands, still unpopped,
	// boxing normally on the cold path since each is already proven in
	// range by its own producer. DIV_S, DIV_U, REM_S, and REM_U are also
	// named by OverflowsI64 but have no arm64 lowering yet.
	if bridged || op.Writes(instr.Frame) || (op.Reads(instr.Heap) && op.Writes(instr.Heap)) || ssa.OverflowsI64(op) {
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

// complete ends module code with the operands it leaves on the interpreter's
// operand stack, which is a module's result the way a function's is what it
// returns. A borrowed one is owned first, because the interpreter adopts what
// it finds there.
func (w *walk) complete() ssa.Terminator {
	w.adopt()
	return ssa.Terminator{Op: ssa.OpComplete, Args: values(w.stack)}
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

// tail ends the function on a tail call, which retires this frame and enters
// another one at the same stack floor. No operation of this IR states that and
// no edge of this graph leads there - the frame the block was translated in is
// gone, and the results the new activation hands back are its own - so native
// execution ends here and the interpreter performs the call from the operand
// stack the exit hands it.
//
// The callee is still resolved, though nothing lowers it: jit.StaticPlan
// refuses a function holding a call whose target the snapshot does not name or
// whose arguments are not on the stack, and this frontend plans no root that
// plan does not.
func (w *walk) tail(ip int) (ssa.Terminator, bool) {
	if len(w.stack) == 0 {
		return ssa.Terminator{}, false
	}
	_, target := w.callee(len(w.stack) - 1)
	if target == nil || len(w.stack) < 1+len(target.Typ.Params) {
		return ssa.Terminator{}, false
	}
	return w.exit(ip), true
}

// enter inlines one call: the callee reference is consumed, the arguments
// under it become the new frame's parameters, its remaining locals start
// cleared, and the caller records where it resumes once the frame returns.
//
// Only a callee whose every slot is scalar inlines. A fresh frame sits over
// whatever words the frame that last occupied that stack region left behind,
// and a slot write is a release of what it replaces, so filling a reference
// slot would drop a count this frame never took. Captures stay out for a
// second reason: an inlined frame reaches its upvalues through the closure
// reference it was called with, which a Slot cannot name.
func (w *walk) enter(addr, resume int, target *types.Function) bool {
	if target.Typ == nil || len(target.Captures) > 0 {
		return false
	}
	slots := target.Declared()
	params := len(target.Typ.Params)
	if len(w.stack) < params+1 {
		return false
	}
	for _, kind := range types.Kinds(slots) {
		switch kind {
		case types.KindI1, types.KindI8, types.KindI32, types.KindI64, types.KindF32, types.KindF64:
		default:
			return false
		}
	}

	w.release(w.stack[len(w.stack)-1])
	w.stack = w.stack[:len(w.stack)-1]
	caller := w.frame()
	caller.ip = resume
	base := caller.base + len(caller.slots) + (len(w.stack) - caller.origin) - params
	// The arguments move into the new frame before it becomes the innermost
	// one, so a deopt among them still resumes at the call in the caller, with
	// the operands the call started with.
	for i := params - 1; i >= 0; i-- {
		w.b.Add(w.block, ssa.Operation{
			Op:    ssa.OpStore,
			Slot:  ssa.Slot{Space: ssa.SpaceLocal, Base: base, Index: i},
			Args:  []ssa.Value{w.stack[len(w.stack)-1].value},
			State: w.deopt(),
		})
		w.stack = w.stack[:len(w.stack)-1]
	}
	for i := params; i < len(slots); i++ {
		kind := slots[i].Kind()
		zero := w.b.Value(ssa.TypeOf(kind))
		w.b.Add(w.block, ssa.Operation{Op: ssa.OpConst, Const: types.Zero(kind), Results: []ssa.Value{zero}})
		w.b.Add(w.block, ssa.Operation{
			Op:    ssa.OpStore,
			Slot:  ssa.Slot{Space: ssa.SpaceLocal, Base: base, Index: i},
			Args:  []ssa.Value{zero},
			State: w.deopt(),
		})
	}
	after := -1
	w.frames = append(w.frames, frame{
		fn: target, addr: addr, slots: slots,
		base: base, origin: len(w.stack), after: &after,
	})
	return true
}

// stitch closes an inlined frame: its results move onto the caller's operand
// stack, owned first because the caller adopts what it is handed, and every
// operand the retiring frame leaves under them is released. The frame holds no
// reference slot to release with it, which is what enter admits.
func (w *walk) stitch() bool {
	fr := w.frame()
	n := fr.returns()
	if len(w.stack)-fr.origin < n {
		return false
	}
	for i := len(w.stack) - n; i < len(w.stack); i++ {
		w.own(i)
	}
	results := append([]operand(nil), w.stack[len(w.stack)-n:]...)
	for _, o := range w.stack[fr.origin : len(w.stack)-n] {
		w.release(o)
	}
	w.stack = append(w.stack[:fr.origin], results...)
	w.frames = w.frames[:len(w.frames)-1]
	return true
}

// exit abandons native execution, resuming the interpreter at ip in the
// innermost frame. The interpreter adopts the operand stack it is handed, so
// every reference still borrowed from storage is owned first.
func (w *walk) exit(ip int) ssa.Terminator {
	w.begin(ip)
	w.adopt()
	return ssa.Terminator{Op: ssa.OpExit, State: w.deopt()}
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
// lives has changed. The retain lands on the snapshot a deopt from this
// instruction resumes with as well, because that is the same stack entry and
// the count the interpreter will release when it adopts it: every own runs
// before its instruction pops or pushes, so at names one entry in both.
//
// A state already materialized stays as it was and stops being this
// instruction's: it was emitted before the retain and resumes into a stack that
// did not hold it yet, which is exactly what its cold path retains. Everything
// after the retain resumes into a stack that does, so the next deopt
// materializes that one.
func (w *walk) own(at int) {
	o := &w.stack[at]
	if o.kind != types.KindRef || o.backing == jit.BackingStack {
		return
	}
	w.retain(o.value)
	o.backing, o.offset = jit.BackingStack, 0
	if at < len(w.pre) {
		w.pre[at] = *o
		w.state = ssa.NoValue
	}
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
// interpreter picks the opcode up, and each keeps the ownership it holds when
// the state materializes (see own).
func (w *walk) begin(ip int) {
	w.ip, w.state = ip, ssa.NoValue
	w.pre = append(w.pre[:0], w.stack...)
}

// deopt materializes the interpreter state the current instruction resumes
// into. One instruction needs at most one for as long as the operands it
// resumes with hold still, so a guard and the access it admits share it unless
// a retain between them changed what the stack owns (see own).
func (w *walk) deopt() ssa.Value {
	if w.state != ssa.NoValue {
		return w.state
	}
	frames := make([]ssa.Frame, len(w.frames))
	for i, fr := range w.frames {
		stack := w.pre[fr.origin:]
		ip := fr.ip
		if i+1 < len(w.frames) {
			stack = w.pre[fr.origin:w.frames[i+1].origin]
		} else {
			ip = w.ip
		}
		frames[i] = ssa.Frame{Addr: fr.addr, Base: fr.base, IP: ip, Returns: fr.returns(), Stack: operands(stack)}
	}
	w.state = w.b.Value(ssa.TypeState)
	w.b.Add(w.block, ssa.Operation{Op: ssa.OpState, Frames: frames, Results: []ssa.Value{w.state}})
	return w.state
}

// push adds one operand.
func (w *walk) push(value ssa.Value, out fact) {
	w.stack = append(w.stack, operand{value: value, fact: out})
}

// frame is the activation being translated, the innermost one.
func (w *walk) frame() *frame {
	return &w.frames[len(w.frames)-1]
}

// returns is how many results the innermost frame hands back.
func (w *walk) returns() int {
	return w.frame().returns()
}

// returns is how many results the function this frame runs hands back.
func (fr frame) returns() int {
	if fr.fn.Typ == nil {
		return 0
	}
	return len(fr.fn.Typ.Returns)
}
