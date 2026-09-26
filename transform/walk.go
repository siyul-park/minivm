package transform

import (
	"unsafe"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

type walker struct {
	facts
	builder    *ssa.Builder
	block      int
	activation activation
	stack      []operand

	ip     int
	before []operand
	state  ssa.Value
}

func (w *walker) adopt() {
	for i := range w.stack {
		w.own(i)
	}
}

func (w *walker) detach(from backing, offset int) {
	for i := range w.stack {
		if w.stack[i].backing == from && w.stack[i].offset == offset {
			w.own(i)
		}
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

func (w *walker) release(o operand) {
	if o.kind != types.KindRef || o.backing != backingStack {
		return
	}
	w.builder.Add(w.block, ssa.Operation{Op: ssa.OpRelease, Args: []ssa.Value{o.value}, State: w.deopt()})
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

func (w *walker) begin(ip int) {
	w.ip, w.state = ip, ssa.NoValue
	w.before = append(w.before[:0], w.stack...)
}

func (w *walker) deopt() ssa.Value {
	if w.state != ssa.NoValue {
		return w.state
	}
	w.state = w.builder.Value(ssa.TypeState)
	w.builder.Add(w.block, ssa.Operation{
		Op:      ssa.OpState,
		Frames:  []ssa.Frame{{Address: w.activation.address, IP: w.ip, Returns: w.activation.returns(), Stack: deopt(w.before)}},
		Results: []ssa.Value{w.state},
	})
	return w.state
}

func (w *walker) push(value ssa.Value, out fact) {
	w.stack = append(w.stack, operand{value: value, fact: out})
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

func (w *walker) edges(s span, states [][]fact, ids []int) ([]ssa.Edge, bool) {
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
		return w.pool(int(inst.Operand(0)))
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
		// The element kind is a best-effort hint: array.len needs no
		// declared type to translate, but native code needs one to pick a
		// representation, so guard only when the array's type is known.
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
				// A []any stores Boxed words of any kind: guard the container.
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
		cast := fact{kind: w.stack[len(w.stack)-1].kind}
		if idx := int(inst.Operand(0)); idx < len(w.types) {
			cast.structType, _ = w.types[idx].(*types.StructType)
			cast.arrayType, _ = w.types[idx].(*types.ArrayType)
		}
		return w.emit(operation, 1, []fact{cast})

	case instr.CALL:
		if len(w.stack) == 0 {
			return false
		}
		at := len(w.stack) - 1
		target := w.callee(at)
		if target == nil {
			target = w.speculate(at)
		}
		if target == nil {
			return false
		}
		results := make([]fact, len(target.Typ.Returns))
		for i, t := range target.Typ.Returns {
			results[i] = fact{kind: t.Kind()}
		}
		return w.emit(operation, 1+len(target.Typ.Params), results)
	case instr.CLOSURE_NEW:
		if len(w.stack) == 0 {
			return false
		}
		capture := w.stack[len(w.stack)-1].fact
		if !capture.referenceKnown || capture.reference <= 0 {
			return false
		}
		target := w.objects.function(capture.reference)
		if target == nil {
			return false
		}
		return w.emit(operation, 1+len(target.Captures), []fact{{kind: types.KindRef}})
	case instr.STRUCT_NEW:
		idx := int(inst.Operand(0))
		if idx >= len(w.types) {
			return false
		}
		record, ok := w.types[idx].(*types.StructType)
		if !ok {
			return false
		}
		return w.emit(operation, len(record.Fields), []fact{{kind: types.KindRef, structType: record}})
	case instr.ARRAY_NEW_DEFAULT:
		if len(w.stack) == 0 {
			return false
		}
		idx := int(inst.Operand(0))
		if idx >= len(w.types) {
			return false
		}
		array, ok := w.types[idx].(*types.ArrayType)
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
			return w.emit(operation, 1+count*2, []fact{{kind: types.KindRef}})
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

// element resolves array's declared element kind, when known: the array's
// type is declared on the fact or resolved through a constant cell. A []any
// resolves to KindRef: its elements are Boxed words.
func (w *walker) element(array fact) (types.Kind, bool) {
	t := array.arrayType
	if t == nil && array.referenceKnown && array.reference > 0 {
		t = w.objects[array.reference].Array
	}
	if t == nil {
		return 0, false
	}
	return t.ElemKind, true
}

func (w *walker) field(container, index fact) (types.Kind, ssa.Shape, bool) {
	record := w.record(container)
	if record == nil || !index.valueKnown || index.value < 0 || int(index.value) >= len(record.Fields) {
		return 0, ssa.Shape{}, false
	}
	return record.Fields[index.value].Kind, ssa.Shape{Struct: true, Type: uintptr(unsafe.Pointer(record))}, true
}

func (w *walker) record(container fact) *types.StructType {
	if container.structType != nil {
		return container.structType
	}
	if container.referenceKnown && container.reference > 0 {
		return w.objects[container.reference].Struct
	}
	return nil
}

func (w *walker) callee(at int) *types.Function {
	o := w.stack[at]
	if !o.referenceKnown || o.reference <= 0 {
		return nil
	}
	target := w.objects.function(o.reference)
	if target == nil || target.Typ == nil {
		return nil
	}
	return target
}

// speculate admits an unresolved CALL's callee from the unit's recorded
// feedback at this ip (see Module.Callees): the one function observed there,
// for a non-owned ref operand only (an owned one would leak its own count on
// substitution). It declines by returning nil. On success it guards the
// operand against the admitted constant and replaces the stack entry with
// it, so the CALL proceeds exactly as a resolved one.
func (w *walker) speculate(at int) *types.Function {
	ref, ok := w.callees[w.ip]
	if !ok {
		return nil
	}
	target := w.objects.function(ref)
	if target == nil || target.Typ == nil {
		return nil
	}
	o := w.stack[at]
	if o.kind != types.KindRef || o.backing == backingStack {
		return nil
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
	frame := w.activation
	slot := ssa.Slot{Space: space, Index: index}
	var out fact
	switch space {
	case ssa.SpaceLocal:
		if index >= len(frame.slots) {
			return slot, out, false
		}
		out = holds(frame.slots[index])
		out.backing, out.offset = backingLocal, index
	case ssa.SpaceUpval:
		if index >= len(frame.function.Captures) {
			return slot, out, false
		}
		out = holds(frame.function.Captures[index])
		out.backing, out.offset = backingUpval, index
	default:
		if index >= len(w.globals) {
			return slot, out, false
		}
		out = fact{kind: w.globals[index], backing: backingGlobal, offset: index}
	}
	return slot, out, true
}

func (w *walker) pool(index int) bool {
	if index >= len(w.constants) {
		return false
	}
	boxed := w.constants[index]
	out, word := fact{kind: boxed.Kind()}, ssa.Word(boxed)
	if out.kind == types.KindRef {
		out.backing, out.reference, out.referenceKnown = backingConst, boxed.Ref(), true
		if obj, ok := w.objects[boxed.Ref()]; ok && obj.I64 != nil {
			out, word = fact{kind: types.KindI64}, uint64(*obj.I64)
		}
	}
	return w.constant(word, out)
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

// adopts returns the number of popped operands transferred to the destination.
func adopts(code instr.Opcode, pops int) int {
	switch {
	case code.Writes(instr.Frame):
		return pops
	case code.Reads(instr.Heap) && code.Writes(instr.Heap):
		return 1
	default:
		return 0
	}
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

	adopted := adopts(opcode, pops)
	var borrows []bool
	switch {
	case opcode.Writes(instr.Frame):
		borrows = w.call()
	case adopted > 0:
		w.own(len(w.stack) - 1)
	}
	args := make([]ssa.Value, named)
	for i := range args {
		args[i] = w.stack[len(w.stack)-named+i].value
	}
	consumed := append([]operand(nil), w.stack[len(w.stack)-pops:]...)
	w.stack = w.stack[:len(w.stack)-pops]

	operation := ssa.Operation{Op: ssa.OpExec, Code: opcode, Args: args, State: w.deopt(), Results: out}
	w.builder.Add(w.block, operation)

	for i, r := range results {
		w.push(out[i], r)
	}
	for i := 0; i < len(consumed)-adopted; i++ {
		w.release(consumed[i])
	}
	// A CALL never adopts a borrowed argument, so the callee's OpReturn
	// never releases it either; the caller releases what it owned for the
	// call here instead.
	for i, borrowed := range borrows {
		if borrowed {
			w.release(consumed[i])
		}
	}
	return true
}

// call adopts every stack entry a CALL pops except a constant callee and an
// argument in a borrowed position (Borrows) backed by a local or a constant:
// the caller's own local cannot change during the call, and the constant
// pool is immortal. A global- or upvalue-backed borrowed argument is adopted
// here, since the callee may overwrite that cell, and released by emit after
// the call instead of by the callee. It returns the resolved target's
// Borrows: CALL reaches emit only after callee or speculate has resolved one.
func (w *walker) call() []bool {
	top := len(w.stack) - 1
	target := w.callee(top)
	borrows := Borrows(target)
	base := top - len(borrows)
	for i := range w.stack {
		if i == top && w.stack[i].backing == backingConst {
			continue
		}
		if i >= base && i < top && borrows[i-base] && w.lent(i) {
			continue
		}
		w.own(i)
	}
	return borrows
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
	args := make([]ssa.Value, len(w.stack))
	for i, o := range w.stack {
		args[i] = o.value
	}
	return ssa.Terminator{Op: ssa.OpComplete, Args: args, State: w.deopt()}
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

func (w *walker) tail(ip int) (ssa.Terminator, bool) {
	if len(w.stack) == 0 {
		return ssa.Terminator{}, false
	}
	target := w.callee(len(w.stack) - 1)
	if target == nil || len(w.stack) < 1+len(target.Typ.Params) {
		return ssa.Terminator{}, false
	}
	return w.exit(ip), true
}

func (w *walker) exit(ip int) ssa.Terminator {
	w.begin(ip)
	w.adopt()
	return ssa.Terminator{Op: ssa.OpExit, State: w.deopt()}
}

func (w *walker) guard(at int, shape ssa.Shape) {
	if w.stack[at].kind != types.KindRef {
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
