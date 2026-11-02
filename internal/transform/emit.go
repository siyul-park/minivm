package transform

import (
	"math"
	"slices"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

type emitter struct {
	function  *ssa.Function
	constants *pool
	module    bool
	base      int

	subst   map[ssa.Value]ssa.Value
	ops     [][]ssa.Operation
	terms   []ssa.Terminator
	uses    map[ssa.Value]int
	defined map[ssa.Value]int
	homed   map[ssa.Value]bool
	home    map[ssa.Value]int

	locals []types.Type
	code   []instr.Instruction
	floor  int
	starts []int
	fixes  []fix
	stack  []ssa.Value
	needed ssa.Value
	failed bool
}

type fix struct {
	at      int
	operand int
	block   int
}

var spaceOps = [...]struct{ read, write instr.Opcode }{
	ssa.SpaceLocal:  {instr.LOCAL_GET, instr.LOCAL_SET},
	ssa.SpaceGlobal: {instr.GLOBAL_GET, instr.GLOBAL_SET},
	ssa.SpaceUpval:  {instr.UPVAL_GET, instr.UPVAL_SET},
}

func emit(function *ssa.Function, constants *pool, module bool, base int) ([]byte, []types.Type, bool) {
	e := &emitter{function: function, constants: constants, module: module, base: base}
	if !e.collect() {
		return nil, nil, false
	}
	for range len(e.uses) + 1 {
		e.params()
		if !e.assign() {
			return nil, nil, false
		}
		walked := e.walk()
		if e.failed {
			return nil, nil, false
		}
		if walked {
			code, ok := e.link()
			return code, e.locals, ok
		}
		if e.needed == ssa.NoValue || e.homed[e.needed] {
			return nil, nil, false
		}
		e.homed[e.needed] = true
	}
	return nil, nil, false
}

func (e *emitter) collect() bool {
	e.subst, e.uses, e.defined, e.homed = map[ssa.Value]ssa.Value{}, map[ssa.Value]int{}, map[ssa.Value]int{}, map[ssa.Value]bool{}
	e.ops, e.terms = make([][]ssa.Operation, e.function.Len()), make([]ssa.Terminator, e.function.Len())

	for id := range e.function.Len() {
		currentBlock := e.function.Block(id)
		for _, p := range currentBlock.Params {
			e.defined[p] = id
		}
		for _, operation := range currentBlock.Operations {
			if !e.accept(id, operation) {
				return false
			}
		}
		term := currentBlock.Terminator
		term.Args = e.values(term.Args)
		if len(term.Edges) > 0 {
			edges := make([]ssa.Edge, len(term.Edges))
			for i, edge := range term.Edges {
				edge.Args = e.values(edge.Args)
				edges[i] = edge
			}
			term.Edges = edges
		}
		e.terms[id] = term
	}

	for id := range e.function.Len() {
		for _, operation := range e.ops[id] {
			e.count(id, operation.Args)
		}
		term := e.terms[id]
		e.count(id, term.Args)
		for _, edge := range term.Edges {
			e.count(id, edge.Args)
		}
	}
	for v, n := range e.uses {
		if n > 1 {
			e.homed[v] = true
		}
	}
	return true
}

func (e *emitter) accept(id int, operation ssa.Operation) bool {
	switch operation.Op {
	case ssa.OpGuardKind, ssa.OpGuardShape, ssa.OpGuardValue, ssa.OpSlice:
		e.subst[operation.Results[0]] = e.resolve(operation.Args[0])
		return true
	case ssa.OpGuardBounds, ssa.OpRetain, ssa.OpRelease, ssa.OpState:
		return true
	case ssa.OpExec:
		if len(instr.TypeOf(operation.Code).Widths) > 0 {
			return false
		}
	case ssa.OpConst:
	case ssa.OpLoad, ssa.OpStore:
		if operation.Slot.Base != 0 || int(operation.Slot.Space) >= len(spaceOps) {
			return false
		}
	default:
		return false
	}
	operation.Args = e.values(operation.Args)
	for _, v := range operation.Results {
		e.defined[v] = id
	}
	e.ops[id] = append(e.ops[id], operation)
	return true
}

func (e *emitter) count(id int, values []ssa.Value) {
	for _, v := range values {
		e.uses[v]++
		if e.defined[v] != id {
			e.homed[v] = true
		}
	}
}

func (e *emitter) params() {
	for id := range e.function.Len() {
		params := e.function.Block(id).Params
		homed := false
		for _, p := range params {
			homed = homed || e.homed[p]
		}
		if !homed {
			continue
		}
		for _, p := range params {
			e.homed[p] = true
		}
	}
}

func (e *emitter) assign() bool {
	e.home, e.locals = map[ssa.Value]int{}, nil
	values := make([]ssa.Value, 0, len(e.homed))
	for v := range e.homed {
		values = append(values, v)
	}
	if e.module && len(values) > 0 {
		return false
	}
	slices.Sort(values)
	for _, v := range values {
		t, ok := local(e.function.Type(v))
		if !ok {
			return false
		}
		slot := e.base + len(e.locals)
		if slot > math.MaxUint8 {
			return false
		}
		e.home[v] = slot
		e.locals = append(e.locals, t)
	}
	return true
}

func (e *emitter) walk() bool {
	e.code, e.fixes, e.needed = nil, nil, ssa.NoValue
	e.starts = make([]int, e.function.Len())
	for id := range e.function.Len() {
		e.starts[id], e.floor = len(e.code), len(e.code)
		if !e.open(id) {
			return false
		}
		for _, operation := range e.ops[id] {
			if !e.perform(operation) {
				return false
			}
		}
		if !e.close(id) {
			return false
		}
	}
	return true
}

func (e *emitter) open(id int) bool {
	params := e.function.Block(id).Params
	e.stack = nil
	if id == 0 {
		return len(params) == 0
	}
	if len(params) == 0 {
		return true
	}
	if !e.homed[params[0]] {
		e.stack = append(e.stack, params...)
		return true
	}
	for i := len(params) - 1; i >= 0; i-- {
		e.write(instr.LOCAL_SET, uint64(e.home[params[i]]))
	}
	return true
}

func (e *emitter) perform(operation ssa.Operation) bool {
	if !e.load(operation.Args) {
		return false
	}
	switch operation.Op {
	case ssa.OpConst:
		if !e.constant(e.function.Type(operation.Results[0]), operation.Const) {
			return false
		}
	case ssa.OpLoad:
		e.write(spaceOps[operation.Slot.Space].read, uint64(operation.Slot.Index))
	case ssa.OpStore:
		e.write(spaceOps[operation.Slot.Space].write, uint64(operation.Slot.Index))
	default:
		e.write(operation.Code)
	}
	e.stack = e.stack[:len(e.stack)-len(operation.Args)]
	return e.results(operation.Results)
}

func (e *emitter) close(id int) bool {
	term := e.terms[id]
	switch term.Op {
	case ssa.OpReturn:
		if !e.load(term.Args) {
			return false
		}
		e.write(instr.RETURN)
	case ssa.OpComplete:
		if !e.module || !e.carry(term.Args) {
			return false
		}
		if id != e.function.Len()-1 {
			e.branch(instr.BR, e.function.Len())
		}
	case ssa.OpJump:
		if !e.carry(term.Edges[0].Args) {
			return false
		}
		e.leave(id, term.Edges[0].Block)
	case ssa.OpBranch, ssa.OpTable:
		if !uniform(term.Edges) || !e.carry(append(slices.Clone(term.Edges[0].Args), term.Args[0])) {
			return false
		}
		e.stack = e.stack[:len(e.stack)-1]
		if term.Op == ssa.OpTable {
			e.table(term.Edges)
			return true
		}
		e.branch(instr.BR_IF, term.Edges[0].Block)
		e.leave(id, term.Edges[1].Block)
	default:
		return false
	}
	return true
}

func (e *emitter) carry(args []ssa.Value) bool {
	if !e.load(args) {
		return false
	}
	if len(e.stack) != len(args) {
		e.needed = e.stack[0]
		return false
	}
	return true
}

func (e *emitter) load(args []ssa.Value) bool {
	at := 0
	for n := min(len(args), len(e.stack)); n > 0; n-- {
		if slices.Equal(e.stack[len(e.stack)-n:], args[:n]) {
			at = n
			break
		}
	}
	for _, v := range args[at:] {
		slot, ok := e.home[v]
		if !ok {
			e.needed = v
			return false
		}
		e.write(instr.LOCAL_GET, uint64(slot))
		e.stack = append(e.stack, v)
	}
	return true
}

func (e *emitter) results(results []ssa.Value) bool {
	e.stack = append(e.stack, results...)
	for i := len(results) - 1; i >= 0; i-- {
		v := results[i]
		if !e.homed[v] && e.uses[v] > 0 {
			for _, under := range results[:i] {
				if e.homed[under] || e.uses[under] == 0 {
					e.needed = v
					return false
				}
			}
			return true
		}
		if e.homed[v] {
			e.write(instr.LOCAL_SET, uint64(e.home[v]))
		} else {
			e.write(instr.DROP)
		}
		e.stack = e.stack[:len(e.stack)-1]
	}
	return true
}

func (e *emitter) leave(id, next int) {
	if next != id+1 {
		e.branch(instr.BR, next)
	}
}

func (e *emitter) branch(operation instr.Opcode, block int) {
	e.write(operation, 0)
	e.fixes = append(e.fixes, fix{at: len(e.code) - 1, block: block})
}

func (e *emitter) table(edges []ssa.Edge) {
	operands := make([]uint64, len(edges)+1)
	operands[0] = uint64(len(edges) - 1)
	e.write(instr.BR_TABLE, operands...)
	for i, edge := range edges {
		e.fixes = append(e.fixes, fix{at: len(e.code) - 1, operand: i + 1, block: edge.Block})
	}
}

func (e *emitter) constant(t ssa.Type, w uint64) bool {
	var c types.Boxed
	switch t {
	case ssa.TypeI32:
		e.write(instr.I32_CONST, uint64(uint32(w)))
		return true
	case ssa.TypeI64:
		e.write(instr.I64_CONST, w)
		return true
	case ssa.TypeF32:
		e.write(instr.F32_CONST, uint64(uint32(w)))
		return true
	case ssa.TypeF64:
		e.write(instr.F64_CONST, w)
		return true
	case ssa.TypeRef:
		if w == uint64(types.BoxedNull) {
			e.write(instr.REF_NULL)
			return true
		}
		c = types.Boxed(w)
	case ssa.TypeI1:
		c = types.BoxI1(w != 0)
	case ssa.TypeI8:
		c = types.BoxI8(int8(w))
	}
	index, ok := e.constants.intern(c)
	if !ok || index > math.MaxUint16 {
		return false
	}
	e.write(instr.CONST_GET, uint64(index))
	return true
}

func (e *emitter) link() ([]byte, bool) {
	offsets := make([]int, len(e.code)+1)
	at := 0
	for i, inst := range e.code {
		offsets[i] = at
		at += inst.Width()
	}
	offsets[len(e.code)] = at

	for _, fix := range e.fixes {
		target := at
		if fix.block < e.function.Len() {
			target = offsets[e.starts[fix.block]]
		}
		delta := target - offsets[fix.at] - e.code[fix.at].Width()
		if delta < math.MinInt16 || delta > math.MaxInt16 {
			return nil, false
		}
		e.code[fix.at].SetOperand(fix.operand, uint64(delta))
	}
	return instr.Marshal(e.code), true
}

func (e *emitter) write(op instr.Opcode, operands ...uint64) {
	if op == instr.LOCAL_GET && len(e.code) > e.floor {
		if last := e.code[len(e.code)-1]; last.Opcode() == instr.LOCAL_SET && last.Operand(0) == operands[0] {
			e.code = e.code[:len(e.code)-1]
			op = instr.LOCAL_TEE
		}
	}
	inst, err := instr.New(op, operands...)
	if err != nil {
		e.failed = true
		return
	}
	e.code = append(e.code, inst)
}

func (e *emitter) values(vals []ssa.Value) []ssa.Value {
	out := make([]ssa.Value, len(vals))
	for i, v := range vals {
		out[i] = e.resolve(v)
	}
	return out
}

func (e *emitter) resolve(v ssa.Value) ssa.Value {
	for {
		at, ok := e.subst[v]
		if !ok {
			return v
		}
		v = at
	}
}

func uniform(edges []ssa.Edge) bool {
	for _, edge := range edges[1:] {
		if !slices.Equal(edge.Args, edges[0].Args) {
			return false
		}
	}
	return true
}

func local(t ssa.Type) (types.Type, bool) {
	switch t {
	case ssa.TypeI1:
		return types.TypeI1(), true
	case ssa.TypeI8:
		return types.TypeI8(), true
	case ssa.TypeI32:
		return types.TypeI32(), true
	case ssa.TypeI64:
		return types.TypeI64(), true
	case ssa.TypeF32:
		return types.TypeF32(), true
	case ssa.TypeF64:
		return types.TypeF64(), true
	case ssa.TypeRef:
		return types.TypeAny(), true
	default:
		return nil, false
	}
}
