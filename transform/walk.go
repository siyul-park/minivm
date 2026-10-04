package transform

import (
	"slices"
	"unsafe"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

type walker struct {
	Module
	builder    *ssa.Builder
	block      int
	activation activation
	stack      []operand
	// closures is, per slot, the function of the closure this unit stored
	// there, 0 when unknown (see frame).
	closures []int

	ip     int
	before []operand
	state  ssa.Value
}

// newWalker returns a walker at the start of block, its stack the params of
// in's facts; it fails when a fact has no SSA representation.
func newWalker(m Module, b *ssa.Builder, block int, act activation, in frame) (*walker, bool) {
	stack := make([]operand, len(in.stack))
	for i, e := range in.stack {
		t, ok := typ(e.kind)
		if !ok {
			return nil, false
		}
		stack[i] = operand{value: b.Param(block, t), fact: e}
	}
	return &walker{Module: m, builder: b, block: block, activation: act, stack: stack, closures: slices.Clone(in.closures)}, true
}

func (w *walker) translate(s span) (ssa.Terminator, bool) {
	code := w.activation.function.Code
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
			if len(w.stack) < w.activation.returns() {
				return ssa.Terminator{}, false
			}
			return w.leave(ip), true
		case instr.RETURN_CALL:
			return w.tail(ip)
		case instr.CALL:
			if w.unseen() {
				return w.exit(ip), true
			}
		case instr.YIELD, instr.RESUME:
			return ssa.Terminator{}, false
		}
		if !w.instruction(inst) {
			return ssa.Terminator{}, false
		}
		ip += inst.Width()
	}
	if len(s.succs) > 0 {
		return ssa.Terminator{Op: ssa.OpJump}, true
	}
	if w.activation.address == 0 {
		return w.complete(s.end), true
	}
	return w.leave(s.end), true
}

func (w *walker) edges(s span, states []frame, ids []int) ([]ssa.Edge, bool) {
	if len(s.succs) == 0 {
		return nil, true
	}
	for _, succ := range s.succs {
		if len(states[succ].stack) != len(w.stack) {
			return nil, false
		}
	}
	for i := range w.stack {
		owned, borrowed := false, false
		for _, succ := range s.succs {
			if states[succ].stack[i].backing == backingStack {
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
		edges[i] = ssa.Edge{Block: ids[succ], Args: values(w.stack)}
	}
	return edges, true
}

func (w *walker) instruction(inst instr.Instruction) bool {
	operation := inst.Opcode()
	switch operation {
	case instr.NOP:
		return true
	case instr.UNREACHABLE:
		return w.emit(operation, 0, nil)

	case instr.LOCAL_GET:
		return w.load(ssa.SpaceLocal, int(inst.Operand(0)))
	case instr.UPVAL_GET:
		return w.load(ssa.SpaceUpval, int(inst.Operand(0)))
	case instr.GLOBAL_GET:
		return w.load(ssa.SpaceGlobal, int(inst.Operand(0)))
	case instr.LOCAL_SET, instr.LOCAL_TEE:
		if operation == instr.LOCAL_TEE && !w.dup() {
			return false
		}
		return w.store(ssa.SpaceLocal, int(inst.Operand(0)))
	case instr.GLOBAL_SET, instr.GLOBAL_TEE:
		if operation == instr.GLOBAL_TEE && !w.dup() {
			return false
		}
		return w.store(ssa.SpaceGlobal, int(inst.Operand(0)))
	case instr.UPVAL_SET:
		return w.store(ssa.SpaceUpval, int(inst.Operand(0)))

	case instr.CONST_GET:
		return w.fetch(int(inst.Operand(0)))
	case instr.I32_CONST:
		val := int32(inst.Operand(0))
		return w.constant(uint64(uint32(val)), fact{kind: types.KindI32, value: val, valueKnown: true})
	case instr.I64_CONST:
		val := int64(inst.Operand(0))
		return w.constant(uint64(val), fact{kind: types.KindI64})
	case instr.F32_CONST:
		return w.constant(uint64(uint32(inst.Operand(0))), fact{kind: types.KindF32})
	case instr.F64_CONST:
		return w.constant(inst.Operand(0), fact{kind: types.KindF64})
	case instr.REF_NULL:
		return w.constant(uint64(types.BoxedNull), fact{kind: types.KindRef})

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
		return w.emit(operation, 3, []fact{{kind: w.stack[n-2].kind}})

	case instr.ARRAY_GET, instr.ARRAY_DELETE:
		if len(w.stack) < 2 {
			return false
		}
		kind, ok := w.element(w.stack[len(w.stack)-2].fact)
		if !ok {
			return false
		}
		if operation == instr.ARRAY_GET {
			w.guard(len(w.stack)-2, ssa.Shape{Kind: kind})
		}
		return w.emit(operation, 2, []fact{{kind: kind}})
	case instr.ARRAY_LEN:
		if len(w.stack) < 1 {
			return false
		}

		if kind, ok := w.element(w.stack[len(w.stack)-1].fact); ok {
			w.guard(len(w.stack)-1, ssa.Shape{Kind: kind})
		}
		return w.emit(operation, 1, []fact{{kind: types.KindI32}})
	case instr.ARRAY_SET:
		if len(w.stack) < 3 {
			return false
		}
		kind := w.stack[len(w.stack)-1].kind
		if elem, ok := w.element(w.stack[len(w.stack)-3].fact); ok {
			if elem == types.KindRef {

				kind = types.KindRef
			} else if (elem == types.KindI1 || elem == types.KindI8) && kind == types.KindI32 {
				kind = elem
			}
		}
		if !kind.IsNumeric() && kind != types.KindRef {
			return false
		}
		w.guard(len(w.stack)-3, ssa.Shape{Kind: kind})
		return w.emit(operation, 3, nil)
	case instr.STRUCT_GET:
		if len(w.stack) < 2 {
			return false
		}
		kind, shape, ok := w.field(w.stack[len(w.stack)-2].fact, w.stack[len(w.stack)-1].fact)
		if !ok {
			return false
		}
		w.guard(len(w.stack)-2, shape)
		return w.emit(operation, 2, []fact{{kind: kind}})
	case instr.STRUCT_SET:
		if len(w.stack) < 3 {
			return false
		}
		w.guard(len(w.stack)-3, ssa.Shape{Struct: true})
		return w.emit(operation, 3, nil)
	case instr.REF_CAST:
		if len(w.stack) == 0 {
			return false
		}
		cast := holds(w.named(inst))
		cast.kind = w.stack[len(w.stack)-1].kind
		return w.emit(operation, 1, []fact{cast})
	case instr.MAP_GET, instr.MAP_LOOKUP:
		if len(w.stack) < 2 {
			return false
		}
		m := w.stack[len(w.stack)-2].mapType
		if m == nil {
			return false
		}
		results := []fact{{kind: m.ElemKind}}
		if operation == instr.MAP_LOOKUP {
			results = append(results, fact{kind: types.KindI1})
		}
		return w.emit(operation, 2, results)
	case instr.MAP_NEW_DEFAULT:
		if len(w.stack) == 0 {
			return false
		}
		mapType, _ := w.named(inst).(*types.MapType)
		return w.emit(operation, 1, []fact{{kind: types.KindRef, mapType: mapType}})

	case instr.CALL:
		if len(w.stack) == 0 {
			return false
		}
		at := len(w.stack) - 1
		target := w.callee(at)
		if target == nil {
			target = w.speculate(at)
		}
		var typ *types.FunctionType
		if target != nil {
			typ = target.Typ
		} else if callee := w.Callees[w.ip]; callee.Function == 0 {
			typ = callee.Type
		}
		if typ == nil {
			return false
		}
		results := make([]fact, len(typ.Returns))
		for i, t := range typ.Returns {
			results[i] = fact{kind: t.Kind()}
		}
		return w.emit(operation, 1+len(typ.Params), results)
	case instr.CLOSURE_NEW:
		if len(w.stack) == 0 {
			return false
		}
		capture := w.stack[len(w.stack)-1].fact
		if !capture.referenceKnown || capture.reference <= 0 {
			return false
		}
		target := w.Objects.function(capture.reference)
		if target == nil {
			return false
		}
		return w.emit(operation, 1+len(target.Captures), []fact{{kind: types.KindRef, closure: capture.reference}})
	case instr.STRUCT_NEW:
		record, ok := w.named(inst).(*types.StructType)
		if !ok {
			return false
		}
		return w.emit(operation, len(record.Fields), []fact{{kind: types.KindRef, structType: record}})
	case instr.ARRAY_NEW_DEFAULT:
		if len(w.stack) == 0 {
			return false
		}
		array, ok := w.named(inst).(*types.ArrayType)
		if !ok {
			return false
		}
		return w.emit(operation, 1, []fact{{kind: types.KindRef, arrayType: array}})
	case instr.ARRAY_NEW, instr.MAP_NEW, instr.ARRAY_APPEND:
		if len(w.stack) == 0 {
			return false
		}
		top := w.stack[len(w.stack)-1].fact
		if top.kind != types.KindI32 || !top.valueKnown || top.value < 0 {
			return false
		}
		count := int(top.value)
		switch operation {
		case instr.MAP_NEW:
			mapType, _ := w.named(inst).(*types.MapType)
			return w.emit(operation, 1+count*2, []fact{{kind: types.KindRef, mapType: mapType}})
		case instr.ARRAY_APPEND:
			return w.emit(operation, 2+count, []fact{{kind: types.KindRef}})
		default:
			return w.emit(operation, 1+count, []fact{{kind: types.KindRef}})
		}
	}

	effect := instr.TypeOf(operation)
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
	return w.emit(operation, len(effect.Pop), results)
}

func (w *walker) dup() bool {
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

// named is the type inst's type operand names, nil when it names none.
func (w *walker) named(inst instr.Instruction) types.Type {
	idx := int(inst.Operand(0))
	if idx >= len(w.Types) {
		return nil
	}
	return w.Types[idx]
}

// element resolves array's declared element kind, when known: the array's
// type is declared on the fact or resolved through a constant cell. A []any
// resolves to KindRef: its elements are Boxed words.
func (w *walker) element(array fact) (types.Kind, bool) {
	t := array.arrayType
	if t == nil && array.referenceKnown && array.reference > 0 {
		t = w.Objects[array.reference].Array
	}
	if t == nil {
		return 0, false
	}
	return t.ElemKind, true
}

func (w *walker) field(container, index fact) (types.Kind, ssa.Shape, bool) {
	record := container.structType
	if record == nil && container.referenceKnown && container.reference > 0 {
		record = w.Objects[container.reference].Struct
	}
	if record == nil || !index.valueKnown || index.value < 0 || int(index.value) >= len(record.Fields) {
		return 0, ssa.Shape{}, false
	}
	return record.Fields[index.value].Kind, ssa.Shape{Struct: true, Type: uintptr(unsafe.Pointer(record))}, true
}

// unseen reports whether the CALL at w.ip has no resolvable callee at a site
// that has never run: native code never reaches it, so it ends in an exit.
func (w *walker) unseen() bool {
	if len(w.stack) == 0 || w.callee(len(w.stack)-1) != nil {
		return false
	}
	_, seen := w.Callees[w.ip]
	return !seen
}

// speculate admits an unresolved CALL's callee from the unit's recorded
// feedback at this ip (see Module.Callees): the one function observed there,
// for a non-owned ref operand only (an owned one would leak its own count on
// substitution). It declines by returning nil. On success it guards the
// operand against the admitted constant and replaces the stack entry with
// it, so the CALL proceeds exactly as a resolved one; a closure callee keeps
// its operand, guarded to be a closure over the admitted function.
func (w *walker) speculate(at int) *types.Function {
	callee, ok := w.Callees[w.ip]
	if !ok {
		return nil
	}
	ref := callee.Function
	target := w.Objects.function(ref)
	if target == nil || target.Typ == nil {
		return nil
	}
	o := w.stack[at]
	if o.kind != types.KindRef || o.backing == backingStack {
		return nil
	}
	if callee.Closure {
		w.guard(at, closure(ref, target))
		w.stack[at].closure = ref
		return target
	}
	c := w.builder.Value(ssa.TypeRef)
	w.builder.Add(w.block, ssa.Operation{Op: ssa.OpConst, Const: uint64(types.BoxRef(ref)), Results: []ssa.Value{c}})
	guarded := w.builder.Value(ssa.TypeRef)
	w.builder.Add(w.block, ssa.Operation{
		Op:      ssa.OpGuardValue,
		Args:    []ssa.Value{o.value, c},
		State:   w.deopt(),
		Results: []ssa.Value{guarded},
	})
	w.stack[at] = operand{value: c, fact: fact{kind: types.KindRef, backing: backingConst, reference: ref, referenceKnown: true}}
	return target
}

func (w *walker) load(space ssa.Space, index int) bool {
	slot, out, ok := w.slot(space, index)
	if !ok {
		return false
	}
	t, ok := typ(out.kind)
	if !ok {
		return false
	}
	if space == ssa.SpaceLocal {
		out.closure = w.closures[index]
	}
	value := w.builder.Value(t)
	w.builder.Add(w.block, ssa.Operation{Op: ssa.OpLoad, Slot: slot, Results: []ssa.Value{value}})
	if out.kind == types.KindI64 {
		guarded := w.builder.Value(t)
		w.builder.Add(w.block, ssa.Operation{
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

func (w *walker) fetch(index int) bool {
	if index >= len(w.Constants) {
		return false
	}
	boxed := w.Constants[index]
	out, word := fact{kind: boxed.Kind()}, boxed.Word()
	if out.kind == types.KindRef {
		out.backing, out.reference, out.referenceKnown = backingConst, boxed.Ref(), true
		if obj, ok := w.Objects[boxed.Ref()]; ok && obj.I64 != nil {
			out, word = fact{kind: types.KindI64}, uint64(*obj.I64)
		}
	}
	return w.constant(word, out)
}

func (w *walker) emit(opcode instr.Opcode, pops int, results []fact) bool {
	if len(w.stack) < pops {
		return false
	}
	effect := instr.TypeOf(opcode)
	named := pops
	if effect.Pop != nil || effect.Push != nil {
		named = len(effect.Pop)
	}
	if named > pops {
		return false
	}
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
		out[i] = w.builder.Value(t)
	}

	adopted := Adopts(opcode, pops, len(results))
	var borrows []bool
	var shape ssa.Shape
	switch {
	case opcode.Writes(instr.Frame):
		borrows, shape = w.call()
	case adopted > 0:
		w.own(len(w.stack) - 1)
	}
	args := values(w.stack[len(w.stack)-named:])
	consumed := append([]operand(nil), w.stack[len(w.stack)-pops:]...)
	w.stack = w.stack[:len(w.stack)-pops]

	operation := ssa.Operation{Op: ssa.OpExec, Code: opcode, Shape: shape, Args: args, State: w.deopt(), Results: out}
	w.builder.Add(w.block, operation)

	for i, r := range results {
		w.push(out[i], r)
	}

	if opcode == instr.SELECT && results[0].kind == types.KindRef {
		w.retain(out[0])
	}
	for i := 0; i < len(consumed)-adopted; i++ {
		w.release(consumed[i])
	}

	for i, borrowed := range borrows {
		if borrowed {
			w.release(consumed[i])
		}
	}
	return true
}

// call adopts every stack entry a CALL pops except a callee and an argument
// in a borrowed position (Borrows) backed by a local or a constant: the
// caller's own local cannot change during the call, and the constant pool is
// immortal. A global- or upvalue-backed borrowed argument or callee is
// adopted here, since the callee may overwrite that cell, and released by
// emit after the call instead of by the callee. It returns the resolved
// target's Borrows and, for a closure callee, the closure Shape the CALL
// carries. A CALL of no resolved target adopts every entry and carries the
// function type its site's callees share: nothing is lent or borrowed.
func (w *walker) call() ([]bool, ssa.Shape) {
	top := len(w.stack) - 1
	target := w.callee(top)
	if target == nil {
		w.adopt()
		return nil, ssa.Shape{Type: uintptr(unsafe.Pointer(w.Callees[w.ip].Type))}
	}
	var shape ssa.Shape
	if ref := w.stack[top].closure; ref != 0 {
		shape = closure(ref, target)
	}
	borrows := Borrows(target)
	base := top - len(borrows)
	for i := range w.stack {
		if i == top && w.lent(i) {
			continue
		}
		if i >= base && i < top && borrows[i-base] && w.lent(i) {
			continue
		}
		w.own(i)
	}
	return borrows, shape
}

// closure is the Shape of a closure over target, the function at ref, as
// CLOSURE_NEW builds it: of target's type, holding its captures.
func closure(ref int, target *types.Function) ssa.Shape {
	return ssa.Shape{Function: ref, Type: uintptr(unsafe.Pointer(target.Typ)), Captures: len(target.Captures)}
}

// lent reports whether stack entry i's own backing survives a call unaided:
// a local slot the caller owns, or a constant, neither of which the call can
// change or free out from under it.
func (w *walker) lent(i int) bool {
	o := w.stack[i]
	return o.kind == types.KindRef && (o.backing == backingLocal || o.backing == backingConst)
}

func (w *walker) complete(ip int) ssa.Terminator {
	w.begin(ip)
	w.adopt()
	return ssa.Terminator{Op: ssa.OpComplete, Args: values(w.stack), State: w.deopt()}
}

func (w *walker) leave(ip int) ssa.Terminator {
	w.begin(ip)
	n := min(w.activation.returns(), len(w.stack))
	args := make([]ssa.Value, 0, n)
	for i := len(w.stack) - n; i < len(w.stack); i++ {
		w.own(i)
		args = append(args, w.stack[i].value)
	}
	return ssa.Terminator{Op: ssa.OpReturn, Args: args, State: w.deopt()}
}

// tail ends a RETURN_CALL: a self tail call loops back to the function's
// first span, any other leaves native execution.
func (w *walker) tail(ip int) (ssa.Terminator, bool) {
	if len(w.stack) == 0 {
		return ssa.Terminator{}, false
	}
	target := w.callee(len(w.stack) - 1)
	if target == nil || len(w.stack) < 1+len(target.Typ.Params) {
		return ssa.Terminator{}, false
	}
	if !w.reuses(target) {
		return w.exit(ip), true
	}
	return w.loop(ip), true
}

// callee resolves the function a CALL at operand at calls: a constant
// function, or the function of a closure this unit built (fact.closure).
func (w *walker) callee(at int) *types.Function {
	o := w.stack[at]
	ref := o.closure
	if ref == 0 && o.referenceKnown {
		ref = o.reference
	}
	if ref <= 0 {
		return nil
	}
	target := w.Objects.function(ref)
	if target == nil || target.Typ == nil {
		return nil
	}
	return target
}

// reuses reports whether the RETURN_CALL at the stack's top re-enters this
// unit's own function with the frame it already has: a plain function, not a
// closure, with every argument on the stack and no slot a store could box
// wide, since a deopt part-way through the stores could not resume threaded.
func (w *walker) reuses(target *types.Function) bool {
	callee := w.stack[len(w.stack)-1]
	if w.activation.address == 0 || callee.closure != 0 || !callee.referenceKnown || callee.reference != w.activation.address ||
		target != w.activation.function || len(target.Captures) > 0 || len(w.stack) != 1+len(target.Typ.Params) {
		return false
	}
	for _, slot := range w.activation.slots {
		if slot.Kind() == types.KindI64 {
			return false
		}
		if _, ok := typ(slot.Kind()); !ok {
			return false
		}
	}
	return true
}

// loop lowers a self tail call as threaded frame reuse does: the arguments
// replace the parameters, every other local is zeroed, each store releasing
// the reference it overwrites, and control returns to the function's first
// span. The stores share the call's resume state, which no store can use.
func (w *walker) loop(ip int) ssa.Terminator {
	w.begin(ip)
	top := len(w.stack) - 1
	w.release(w.stack[top])
	w.stack = w.stack[:top]
	for p := len(w.activation.function.Typ.Params) - 1; p >= 0; p-- {
		w.store(ssa.SpaceLocal, p)
	}
	for slot := len(w.activation.function.Typ.Params); slot < len(w.activation.slots); slot++ {
		kind := w.activation.slots[slot].Kind()
		word := uint64(0)
		if kind == types.KindRef {
			word = uint64(types.BoxedNull)
		}
		w.constant(word, fact{kind: kind})
		w.store(ssa.SpaceLocal, slot)
	}
	return ssa.Terminator{Op: ssa.OpJump}
}

func (w *walker) release(o operand) {
	if o.kind != types.KindRef || o.backing != backingStack {
		return
	}
	w.builder.Add(w.block, ssa.Operation{Op: ssa.OpRelease, Args: []ssa.Value{o.value}, State: w.deopt()})
}

func (w *walker) store(space ssa.Space, index int) bool {
	if len(w.stack) == 0 {
		return false
	}
	slot, out, ok := w.slot(space, index)
	if !ok {
		return false
	}
	top := len(w.stack) - 1
	if w.stack[top].kind == types.KindRef {
		w.own(top)
		w.detach(out.backing, out.offset)
	}
	if space == ssa.SpaceLocal {
		w.closures[index] = w.stack[top].closure
	}
	w.builder.Add(w.block, ssa.Operation{
		Op:    ssa.OpStore,
		Slot:  slot,
		Args:  []ssa.Value{w.stack[top].value},
		State: w.deopt(),
	})
	w.stack = w.stack[:top]
	return true
}

func (w *walker) slot(space ssa.Space, index int) (ssa.Slot, fact, bool) {
	slot := ssa.Slot{Space: space, Index: index}
	var out fact
	switch space {
	case ssa.SpaceLocal:
		if index >= len(w.activation.slots) {
			return slot, out, false
		}
		out = holds(w.activation.slots[index])
		out.backing, out.offset = backingLocal, index
	case ssa.SpaceUpval:
		if index >= len(w.activation.function.Captures) {
			return slot, out, false
		}
		out = holds(w.activation.function.Captures[index])
		out.backing, out.offset = backingUpval, index
	default:
		if index >= len(w.Globals) {
			return slot, out, false
		}
		out = fact{kind: w.Globals[index], backing: backingGlobal, offset: index}
	}
	return slot, out, true
}

func (w *walker) detach(from backing, offset int) {
	for i := range w.stack {
		if w.stack[i].backing == from && w.stack[i].offset == offset {
			w.own(i)
		}
	}
}

func (w *walker) constant(word uint64, out fact) bool {
	t, ok := typ(out.kind)
	if !ok {
		return false
	}
	value := w.builder.Value(t)
	w.builder.Add(w.block, ssa.Operation{Op: ssa.OpConst, Const: word, Results: []ssa.Value{value}})
	w.push(value, out)
	return true
}

func (w *walker) push(value ssa.Value, out fact) {
	w.stack = append(w.stack, operand{value: value, fact: out})
}

func (w *walker) exit(ip int) ssa.Terminator {
	w.begin(ip)
	w.adopt()
	return ssa.Terminator{Op: ssa.OpExit, State: w.deopt()}
}

func (w *walker) adopt() {
	for i := range w.stack {
		w.own(i)
	}
}

func (w *walker) own(at int) {
	o := &w.stack[at]
	if o.kind != types.KindRef || o.backing == backingStack {
		return
	}
	w.retain(o.value)
	o.backing, o.offset = backingStack, 0
	if at < len(w.before) {
		w.before[at] = *o
		w.state = ssa.NoValue
	}
}

func (w *walker) retain(value ssa.Value) {
	w.builder.Add(w.block, ssa.Operation{Op: ssa.OpRetain, Args: []ssa.Value{value}})
}

func (w *walker) begin(ip int) {
	w.ip, w.state = ip, ssa.NoValue
	w.before = append(w.before[:0], w.stack...)
}

func (w *walker) guard(at int, shape ssa.Shape) {
	if w.stack[at].kind != types.KindRef || shape.Function == 0 && w.Refuted[w.ip] {
		return
	}
	value := w.builder.Value(ssa.TypeRef)
	w.builder.Add(w.block, ssa.Operation{
		Op:      ssa.OpGuardShape,
		Shape:   shape,
		Args:    []ssa.Value{w.stack[at].value},
		State:   w.deopt(),
		Results: []ssa.Value{value},
	})
	w.stack[at].value = value
}

func (w *walker) deopt() ssa.Value {
	if w.state != ssa.NoValue {
		return w.state
	}
	stack := make([]ssa.Operand, len(w.before))
	for i, o := range w.before {
		stack[i] = ssa.Operand{Value: o.value, Owned: o.kind == types.KindRef && o.backing == backingStack}
	}
	w.state = w.builder.Value(ssa.TypeState)
	w.builder.Add(w.block, ssa.Operation{
		Op:      ssa.OpState,
		Frames:  []ssa.Frame{{Address: w.activation.address, IP: w.ip, Returns: w.activation.returns(), Stack: stack}},
		Results: []ssa.Value{w.state},
	})
	return w.state
}

func values(stack []operand) []ssa.Value {
	out := make([]ssa.Value, len(stack))
	for i, o := range stack {
		out[i] = o.value
	}
	return out
}
