package transform

import (
	"unsafe"

	"github.com/siyul-park/minivm/instr"
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
	fr    frame
	stack []operand

	ip    int
	pre   []operand
	state ssa.Value
}

// Guard identities the translation admits a heap cell through: one per
// concrete array element kind, plus one for any struct. Bytecode emission
// erases every guard it meets (see emit.go), so these exist only to tell two
// guards over different containers apart from two guards over the same one
// within the SSA a pass over one function sees; the concrete value carries no
// meaning past that.
const (
	shapeArrayI1 uintptr = iota + 1
	shapeArrayI8
	shapeArrayI32
	shapeArrayI64
	shapeArrayF32
	shapeArrayF64
	shapeArrayRef
	shapeStruct
)

// run fills the block with s and returns the terminator it ends on, carrying no
// edges: which blocks those name is the caller's to resolve, because a span
// knows its successors only as spans.
func (w *walk) adopt() {
	for i := range w.stack {
		w.own(i)
	}
}

func (w *walk) detach(from backing, offset int) {
	for i := range w.stack {
		if w.stack[i].backing == from && w.stack[i].offset == offset {
			w.own(i)
		}
	}
}

func (w *walk) own(at int) {
	o := &w.stack[at]
	if o.kind != types.KindRef || o.backing == backingStack {
		return
	}
	w.retain(o.value)
	o.backing, o.offset = backingStack, 0
	if at < len(w.pre) {
		w.pre[at] = *o
		w.state = ssa.NoValue
	}
}

func (w *walk) retain(value ssa.Value) {
	w.b.Add(w.block, ssa.Operation{Op: ssa.OpRetain, Args: []ssa.Value{value}})
}

func (w *walk) release(o operand) {
	if o.kind != types.KindRef || o.backing != backingStack {
		return
	}
	w.b.Add(w.block, ssa.Operation{Op: ssa.OpRelease, Args: []ssa.Value{o.value}, State: w.deopt()})
}

func (w *walk) dup() bool {
	if len(w.stack) == 0 {
		return false
	}
	top := w.stack[len(w.stack)-1]
	if top.kind == types.KindRef && top.backing == backingStack {
		w.retain(top.value)
	}
	w.stack = append(w.stack, top)
	return true
}

func (w *walk) begin(ip int) {
	w.ip, w.state = ip, ssa.NoValue
	w.pre = append(w.pre[:0], w.stack...)
}

func (w *walk) deopt() ssa.Value {
	if w.state != ssa.NoValue {
		return w.state
	}
	w.state = w.b.Value(ssa.TypeState)
	w.b.Add(w.block, ssa.Operation{
		Op:      ssa.OpState,
		Frames:  []ssa.Frame{{Addr: w.fr.addr, IP: w.ip, Returns: w.fr.returns(), Stack: deoptOperands(w.pre)}},
		Results: []ssa.Value{w.state},
	})
	return w.state
}

func (w *walk) push(value ssa.Value, out fact) {
	w.stack = append(w.stack, operand{value: value, fact: out})
}

func (w *walk) run(s span) (ssa.Terminator, bool) {
	code := w.fr.fn.Code
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
		case instr.YIELD, instr.RESUME:
			// A suspension ends execution at the opcode itself: the
			// interpreter performs the real suspend and resumes threaded, so
			// the span carries no successor (see span.suspend).
			return w.suspend(ip), true
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
	if w.fr.addr == 0 {
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
			if states[succ][i].backing == backingStack {
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
		if op == instr.LOCAL_TEE && !w.dup() {
			return false
		}
		return w.store(ssa.SpaceLocal, int(inst.Operand(0)))
	case instr.GLOBAL_SET, instr.GLOBAL_TEE:
		if op == instr.GLOBAL_TEE && !w.dup() {
			return false
		}
		return w.store(ssa.SpaceGlobal, int(inst.Operand(0)))
	case instr.UPVAL_SET:
		return w.store(ssa.SpaceUpval, int(inst.Operand(0)))

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
		return w.dup()
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
		kind, tok, ok := w.elem(w.stack[len(w.stack)-2].fact)
		if !ok {
			return false
		}
		if op == instr.ARRAY_GET {
			w.guard(len(w.stack)-2, ssa.Shape{Itab: tok})
		}
		return w.exec(op, 2, []fact{{kind: kind}})
	case instr.ARRAY_SET:
		// The element shape comes from the value's own kind rather than from
		// the container, unlike ARRAY_GET: a write has no result to type, so
		// what it needs from the guard is only that the container's shape
		// agrees with what the value being stored already is.
		if len(w.stack) < 3 {
			return false
		}
		tok, ok := elemToken(w.stack[len(w.stack)-1].kind)
		if !ok {
			return false
		}
		w.guard(len(w.stack)-3, ssa.Shape{Itab: tok})
		return w.exec(op, 3, nil)
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
	case instr.STRUCT_SET:
		// Unlike STRUCT_GET, a write needs no field kind resolved ahead of
		// time: the store's runtime kind check is against the value's own
		// already-known SSA kind, so the guard only needs a struct shape to
		// admit.
		if len(w.stack) < 3 {
			return false
		}
		w.guard(len(w.stack)-3, ssa.Shape{Itab: shapeStruct})
		return w.exec(op, 3, nil)
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
		target := w.objects.function(capture.ref)
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

// elemToken resolves the guard identity one array element kind is stored
// under.
func elemToken(kind types.Kind) (uintptr, bool) {
	switch kind {
	case types.KindI1:
		return shapeArrayI1, true
	case types.KindI8:
		return shapeArrayI8, true
	case types.KindI32:
		return shapeArrayI32, true
	case types.KindI64:
		return shapeArrayI64, true
	case types.KindF32:
		return shapeArrayF32, true
	case types.KindF64:
		return shapeArrayF64, true
	case types.KindRef:
		return shapeArrayRef, true
	default:
		return 0, false
	}
}

// elem resolves the element kind and guard identity one array access is
// compiled against: the operand's declared array type, which answers only in
// a call-free function. It is a hint the shape guard verifies before any
// access, so a slot declared as an array that currently holds null or a
// differently shaped array deopts instead of being read.
func (w *walk) elem(array fact) (types.Kind, uintptr, bool) {
	if !w.declared || array.atyp == nil || array.atyp.ElemKind == instr.KindAny {
		return 0, 0, false
	}
	tok, ok := elemToken(array.atyp.ElemKind)
	return array.atyp.ElemKind, tok, ok
}

// field resolves a struct field's kind and the shape the access reading it is
// admitted through: the one a container carrying a struct type answers for a
// known in-bounds constant index.
func (w *walk) field(container, index fact) (types.Kind, ssa.Shape, bool) {
	record := w.record(container)
	if record == nil || !index.valKnown || index.val < 0 || int(index.val) >= len(record.Fields) {
		return 0, ssa.Shape{}, false
	}
	return record.Fields[index.val].Kind, ssa.Shape{Itab: shapeStruct, Typ: uintptr(unsafe.Pointer(record))}, true
}

// record resolves the struct type a container carries: the one its declared
// type or a ref.cast states, or the one a constant cell was resolved to carry.
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
// at. Everything reading the operand after that - this walk, and a lowering
// resolving the call's target - reads a value that certainly holds that
// reference.
//
// The reference is the whole answer, so a call whose operand names anything
// this translation does not resolve to a function is left unplanned: a host
// function, a coroutine, and a closure allocated at runtime all name no
// function here.
func (w *walk) callee(at int) (int, *types.Function) {
	o := w.stack[at]
	if !o.refKnown || o.ref <= 0 {
		return 0, nil
	}
	target := w.objects.function(o.ref)
	if target == nil || target.Typ == nil {
		return 0, nil
	}
	return o.ref, target
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

// store writes the top operand into a slot, which releases whatever it
// replaces, and always consumes that operand. A ref hands its retain to the
// slot, so a borrowed one is owned first and every other operand borrowed
// from that slot is owned before its content changes underneath it; a store
// back over itself is exactly the case detach also owns, so the backend's
// release of the overwritten word never has to special-case it. A tee
// duplicates the operand before calling this (see perform), so the surviving
// stack copy is a second owned operand rather than something this function
// has to keep alive itself.
func (w *walk) store(space ssa.Space, index int) bool {
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
	}
	w.b.Add(w.block, ssa.Operation{
		Op:    ssa.OpStore,
		Slot:  slot,
		Args:  []ssa.Value{w.stack[top].value},
		State: w.deopt(),
	})
	w.stack = w.stack[:top]
	return true
}

// addressed resolves the storage a slot opcode names and what is known about
// the value in it. A local's deferred reference count is named by its own
// slot index.
func (w *walk) addressed(space ssa.Space, index int) (ssa.Slot, fact, bool) {
	fr := w.fr
	slot := ssa.Slot{Space: space, Index: index}
	var out fact
	switch space {
	case ssa.SpaceLocal:
		if index >= len(fr.slots) {
			return slot, out, false
		}
		out = holds(fr.slots[index])
		out.backing, out.offset = backingLocal, index
	case ssa.SpaceUpval:
		if index >= len(fr.fn.Captures) {
			return slot, out, false
		}
		out = holds(fr.fn.Captures[index])
		out.backing, out.offset = backingUpval, index
	default:
		if index >= len(w.globals) {
			return slot, out, false
		}
		out = fact{kind: w.globals[index], backing: backingGlobal, offset: index}
	}
	return slot, out, true
}

// pool pushes a constant. A ref constant is an ownership-neutral marker whose
// retain stays with the pool, and the reference it carries is what resolves the
// container it accesses or the function it calls.
func (w *walk) pool(index int) bool {
	if index >= len(w.constants) {
		return false
	}
	boxed := w.constants[index]
	out := fact{kind: boxed.Kind()}
	if out.kind == types.KindRef {
		out.backing = backingConst
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

	bridged := bridgeable(op)
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
	// boxed 49-bit payload (see its own doc comment for which and why), and
	// ssa.Divides names the ones that can fault on a zero divisor instead.
	// Both guard through this same state - w.pre as of this instruction's own
	// start (see begin/deopt) - so the flush is both operands, still
	// unpopped, boxing normally on the cold path since each is already
	// proven in range by its own producer.
	if bridged || op.Writes(instr.Frame) || (op.Reads(instr.Heap) && op.Writes(instr.Heap)) ||
		ssa.OverflowsI64(op) || ssa.Divides(op) {
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

// tail ends the function on a tail call, which retires this frame. No
// operation of this IR states that and no edge of this graph leads there - the
// frame the block was translated in is gone, and the results the new
// activation hands back are its own - so execution ends here and the
// interpreter performs the call from the operand stack the exit hands it.
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

// exit abandons execution, resuming the interpreter at ip. The interpreter
// adopts the operand stack it is handed, so every reference still borrowed
// from storage is owned first.
func (w *walk) exit(ip int) ssa.Terminator {
	w.begin(ip)
	w.adopt()
	return ssa.Terminator{Op: ssa.OpExit, State: w.deopt()}
}

// suspend ends execution on a suspension point, resuming the interpreter at
// the opcode's own IP. Like exit it hands the interpreter an adopted operand
// stack; unlike exit the threaded continuation runs past the opcode, so the
// span after it is planned for facts but never emitted (see span.suspend).
func (w *walk) suspend(ip int) ssa.Terminator {
	w.begin(ip)
	w.adopt()
	return ssa.Terminator{Op: ssa.OpSuspend, State: w.deopt()}
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

// returns is how many results the function this walk translates hands back.
func (w *walk) returns() int {
	return w.fr.returns()
}
