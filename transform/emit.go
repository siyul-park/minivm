package transform

import (
	"math"
	"slices"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

// emitter writes one ssa.Function back out as bytecode. SSA carries dataflow
// and bytecode carries an operand stack, so the whole job is deciding where
// each value lives between its definition and its use: on the stack, where the
// translation that produced this function found it, or in a local, which is
// the only other place bytecode can keep one.
//
// A value stays on the stack when it is read exactly once, in the block that
// defines it, at the moment it is on top - which is every value a function
// that has not been transformed holds, because it came from a stack machine.
// Anything else takes a fresh local: a value read twice, one read in another
// block, and one an optimization moved out of stack order. Which values those
// are is not known before the stack is walked, so the walk names the value it
// could not reach, that value takes a local, and the walk runs again; every
// pass gives one more value a home, and a function where every value has one
// always emits.
//
// What it cannot write out, it declines: an operation whose bytecode carries
// an immediate operand the IR does not keep, a local slot past the operand
// width, a branch past the reach of its signed 16-bit offset, or an entry
// block that expects operands nothing hands it. A declined function is left
// exactly as it was, never emitted wrong.
type emitter struct {
	fn     *ssa.Function
	consts *pool
	// module reports whether this is top-level code, which ends by advancing
	// past its last instruction, may branch there, and may take no fresh
	// local: a module's locals sit on the very operand stack a caller reads
	// the program's results off, so one more of them is one more result.
	module bool
	base   int

	subst map[ssa.Value]ssa.Value
	ops   [][]ssa.Operation
	terms []ssa.Terminator
	uses  map[ssa.Value]int
	born  map[ssa.Value]int
	homed map[ssa.Value]bool
	home  map[ssa.Value]int

	added []types.Type
	code  []instr.Instruction
	// begin is where the block being emitted starts, which is as far back as
	// a peephole may reach: every earlier instruction is another block's, and
	// a branch may target the boundary between them.
	begin  int
	starts []int
	fixes  []fix
	stack  []ssa.Value
	blame  ssa.Value
}

// fix is one branch operand still to be resolved: the instruction holding it,
// which operand it is, and the block it names. A block past the last one is
// the offset one past the end of the code, which only top-level code reaches.
type fix struct {
	at      int
	operand int
	block   int
}

// slots is the pair of opcodes one storage space is read and written through,
// indexed by the space itself.
var slots = [...]struct{ read, write instr.Opcode }{
	ssa.SpaceLocal:  {instr.LOCAL_GET, instr.LOCAL_SET},
	ssa.SpaceGlobal: {instr.GLOBAL_GET, instr.GLOBAL_SET},
	ssa.SpaceUpval:  {instr.UPVAL_GET, instr.UPVAL_SET},
}

// emit returns the bytecode fn performs and the locals it had to allocate,
// counting from base, or ok=false when fn holds something bytecode cannot
// express.
func emit(fn *ssa.Function, consts *pool, module bool, base int) ([]byte, []types.Type, bool) {
	e := &emitter{fn: fn, consts: consts, module: module, base: base}
	if !e.read() {
		return nil, nil, false
	}
	// Each pass gives the one value the last could not reach a home, so the
	// walk runs at most once more than there are values to give one to.
	for range len(e.uses) + 1 {
		e.settle()
		if !e.assign() {
			return nil, nil, false
		}
		if e.walk() {
			code, ok := e.link()
			return code, e.added, ok
		}
		if e.blame == ssa.NoValue || e.homed[e.blame] {
			return nil, nil, false
		}
		e.homed[e.blame] = true
	}
	return nil, nil, false
}

// read collects what survives into bytecode. A guard, a retain, a release, and
// the interpreter state a deopt resumes into are what an optimizing compiler
// adds over the opcodes; none has a bytecode form, and a guard hands its
// operand straight back, so everything reading a guarded value reads the value
// it admitted instead.
func (e *emitter) read() bool {
	e.subst, e.uses, e.born, e.homed = map[ssa.Value]ssa.Value{}, map[ssa.Value]int{}, map[ssa.Value]int{}, map[ssa.Value]bool{}
	e.ops, e.terms = make([][]ssa.Operation, e.fn.Len()), make([]ssa.Terminator, e.fn.Len())

	for id := range e.fn.Len() {
		blk := e.fn.Block(id)
		for _, p := range blk.Params {
			e.born[p] = id
		}
		for _, op := range blk.Ops {
			if !e.take(id, op) {
				return false
			}
		}
		term := blk.Term
		term.Args = e.list(term.Args)
		if len(term.Edges) > 0 {
			edges := make([]ssa.Edge, len(term.Edges))
			for i, edge := range term.Edges {
				edge.Args = e.list(edge.Args)
				edges[i] = edge
			}
			term.Edges = edges
		}
		e.terms[id] = term
	}

	// A value read anywhere but where it was defined, or read more than once,
	// outlives the single stack slot its definition leaves behind.
	for id := range e.fn.Len() {
		for _, op := range e.ops[id] {
			e.count(id, op.Args)
		}
		e.count(id, operands(e.terms[id]))
	}
	for v, n := range e.uses {
		if n > 1 {
			e.homed[v] = true
		}
	}
	return true
}

// take keeps one operation, and reports false for one bytecode cannot spell
// again.
func (e *emitter) take(id int, op ssa.Operation) bool {
	switch op.Op {
	case ssa.OpGuardKind, ssa.OpGuardShape, ssa.OpGuardValue:
		e.subst[op.Results[0]] = e.resolve(op.Args[0])
		return true
	case ssa.OpGuardBounds, ssa.OpRetain, ssa.OpRelease, ssa.OpState:
		return true
	case ssa.OpExec, ssa.OpBridge:
		// An opcode with an immediate operand cannot be written back: the IR
		// resolves what that operand meant and keeps no way to spell the
		// operand itself again.
		if len(instr.TypeOf(op.Code).Widths) > 0 {
			return false
		}
	case ssa.OpConst:
	case ssa.OpLoad, ssa.OpStore:
		// A local of a frame the translation inlined counts from that frame's
		// own floor, which no LOCAL_* operand can name.
		if op.Slot.Base != 0 || int(op.Slot.Space) >= len(slots) {
			return false
		}
	default:
		return false
	}
	op.Args = e.list(op.Args)
	for _, v := range op.Results {
		e.born[v] = id
	}
	e.ops[id] = append(e.ops[id], op)
	return true
}

// count records one read of each value, homing any read from a block other
// than the one that defines it.
func (e *emitter) count(id int, vs []ssa.Value) {
	for _, v := range vs {
		e.uses[v]++
		if e.born[v] != id {
			e.homed[v] = true
		}
	}
}

// settle gives every parameter of a block a home once any of them needs one:
// they arrive as one operand stack, so nothing can reach past the top one to
// store it without storing the ones above it too.
func (e *emitter) settle() {
	for id := range e.fn.Len() {
		params := e.fn.Block(id).Params
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

// assign gives every homed value a local slot, in value order so the same
// function always emits the same code. It fails when a slot would not fit the
// one-byte operand every LOCAL_* opcode encodes it in.
func (e *emitter) assign() bool {
	e.home, e.added = map[ssa.Value]int{}, nil
	values := make([]ssa.Value, 0, len(e.homed))
	for v := range e.homed {
		values = append(values, v)
	}
	if e.module && len(values) > 0 {
		return false
	}
	slices.Sort(values)
	for _, v := range values {
		t, ok := declared(e.fn.Type(v))
		if !ok {
			return false
		}
		slot := e.base + len(e.added)
		if slot > math.MaxUint8 {
			return false
		}
		e.home[v] = slot
		e.added = append(e.added, t)
	}
	return true
}

// walk emits every block in turn, naming in blame the value it could not
// reach when a stack it walked did not hold what an operation wanted.
func (e *emitter) walk() bool {
	e.code, e.fixes, e.blame = nil, nil, ssa.NoValue
	e.starts = make([]int, e.fn.Len())
	for id := range e.fn.Len() {
		e.starts[id], e.begin = len(e.code), len(e.code)
		if !e.open(id) {
			return false
		}
		for _, op := range e.ops[id] {
			if !e.perform(op) {
				return false
			}
		}
		if !e.close(id) {
			return false
		}
	}
	return true
}

// open starts a block with the operands its predecessors leave on the stack.
// Homed parameters are stored top first, which is the only order the stack
// hands them over in.
func (e *emitter) open(id int) bool {
	params := e.fn.Block(id).Params
	e.stack = nil
	if id == 0 {
		// The entry is reached with an empty operand stack, so a parameter of
		// it names an operand nothing hands over.
		return len(params) == 0
	}
	if len(params) == 0 {
		return true
	}
	// settle has already made the choice the same for all of them.
	if !e.homed[params[0]] {
		e.stack = append(e.stack, params...)
		return true
	}
	for i := len(params) - 1; i >= 0; i-- {
		e.write(instr.New(instr.LOCAL_SET, uint64(e.home[params[i]])))
	}
	return true
}

// perform writes one operation: its operands onto the stack, the instruction
// that performs it, and its results wherever they live.
func (e *emitter) perform(op ssa.Operation) bool {
	if !e.want(op.Args) {
		return false
	}
	switch op.Op {
	case ssa.OpConst:
		inst, ok := e.constant(op.Const)
		if !ok {
			return false
		}
		e.write(inst)
	case ssa.OpLoad:
		e.write(instr.New(slots[op.Slot.Space].read, uint64(op.Slot.Index)))
	case ssa.OpStore:
		e.write(instr.New(slots[op.Slot.Space].write, uint64(op.Slot.Index)))
	default:
		e.write(instr.New(op.Code))
	}
	e.stack = e.stack[:len(e.stack)-len(op.Args)]
	return e.keep(op.Results)
}

// close ends a block on the instruction its terminator names, leaving the
// successor exactly the operands its parameters expect.
func (e *emitter) close(id int) bool {
	term := e.terms[id]
	switch term.Op {
	case ssa.OpReturn:
		// The frame teardown discards whatever sits under the results, so an
		// operand still on the stack under them needs no instruction.
		if !e.want(term.Args) {
			return false
		}
		e.write(instr.New(instr.RETURN))
	case ssa.OpComplete:
		if !e.module || !e.carry(term.Args) {
			return false
		}
		if id != e.fn.Len()-1 {
			e.branch(instr.BR, e.fn.Len())
		}
	case ssa.OpJump:
		if !e.carry(term.Edges[0].Args) {
			return false
		}
		e.leave(id, term.Edges[0].Block)
	case ssa.OpBranch, ssa.OpTable:
		if !e.uniform(term.Edges) || !e.carry(append(slices.Clone(term.Edges[0].Args), term.Args[0])) {
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

// want leaves args on top of the stack, in order. The longest run of them
// already there stays where it is; the rest are read back out of their homes,
// and a value with no home is the one the next pass has to give one.
func (e *emitter) want(args []ssa.Value) bool {
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
			e.blame = v
			return false
		}
		e.write(instr.New(instr.LOCAL_GET, uint64(slot)))
		e.stack = append(e.stack, v)
	}
	return true
}

// carry leaves args on the stack and nothing else, which is what a successor
// entered with its parameters as its whole operand stack is handed. An
// operand still underneath them belongs in a home instead.
func (e *emitter) carry(args []ssa.Value) bool {
	if !e.want(args) {
		return false
	}
	if len(e.stack) != len(args) {
		e.blame = e.stack[0]
		return false
	}
	return true
}

// keep settles an operation's results: a homed one is stored, an unread one
// dropped, and one the next operation reads stays where it is. Only the top
// of the stack can be stored or dropped, so a result that stays there hides
// every result under it.
func (e *emitter) keep(results []ssa.Value) bool {
	e.stack = append(e.stack, results...)
	for i := len(results) - 1; i >= 0; i-- {
		v := results[i]
		if !e.homed[v] && e.uses[v] > 0 {
			for _, under := range results[:i] {
				if e.homed[under] || e.uses[under] == 0 {
					e.blame = v
					return false
				}
			}
			return true
		}
		if e.homed[v] {
			e.write(instr.New(instr.LOCAL_SET, uint64(e.home[v])))
		} else {
			e.write(instr.New(instr.DROP))
		}
		e.stack = e.stack[:len(e.stack)-1]
	}
	return true
}

// uniform reports whether every edge hands its successor the same operands,
// which is what a branch out of one operand stack can do.
func (e *emitter) uniform(edges []ssa.Edge) bool {
	for _, edge := range edges[1:] {
		if !slices.Equal(edge.Args, edges[0].Args) {
			return false
		}
	}
	return true
}

// leave branches to next unless the block emitted after this one is already
// it.
func (e *emitter) leave(id, next int) {
	if next != id+1 {
		e.branch(instr.BR, next)
	}
}

// branch writes a branch whose operand is resolved once every block's offset
// is known.
func (e *emitter) branch(op instr.Opcode, block int) {
	e.write(instr.New(op, 0))
	e.fixes = append(e.fixes, fix{at: len(e.code) - 1, operand: 0, block: block})
}

// table writes a branch table, whose first operand is how many targets follow
// the one every table carries.
func (e *emitter) table(edges []ssa.Edge) {
	operands := make([]uint64, len(edges)+1)
	operands[0] = uint64(len(edges) - 1)
	e.write(instr.New(instr.BR_TABLE, operands...))
	for i, edge := range edges {
		e.fixes = append(e.fixes, fix{at: len(e.code) - 1, operand: i + 1, block: edge.Block})
	}
}

// constant writes a compile-time value: the opcode that spells it outright,
// or a read of the constant slot holding it, interning one when the pool has
// none. A reference the pool does not already hold names a cell only a
// running interpreter could allocate, and no bytecode spells it.
func (e *emitter) constant(c types.Boxed) (instr.Instruction, bool) {
	switch c.Kind() {
	case types.KindI32:
		return instr.New(instr.I32_CONST, uint64(uint32(c.I32()))), true
	case types.KindI64:
		return instr.New(instr.I64_CONST, uint64(c.I64())), true
	case types.KindF32:
		return instr.New(instr.F32_CONST, uint64(math.Float32bits(c.F32()))), true
	case types.KindF64:
		return instr.New(instr.F64_CONST, uint64(c)), true
	case types.KindRef:
		if c == types.BoxedNull {
			return instr.New(instr.REF_NULL), true
		}
	}
	index, ok := e.consts.index(c)
	if !ok || index > math.MaxUint16 {
		return nil, false
	}
	return instr.New(instr.CONST_GET, uint64(index)), true
}

// link resolves every branch operand against the offsets the emitted
// instructions ended up at, and fails when one no longer reaches its target
// within the signed 16-bit operand every branch encodes.
func (e *emitter) link() ([]byte, bool) {
	offsets := make([]int, len(e.code)+1)
	at := 0
	for i, inst := range e.code {
		offsets[i] = at
		at += inst.Width()
	}
	offsets[len(e.code)] = at

	for _, f := range e.fixes {
		target := at
		if f.block < e.fn.Len() {
			target = offsets[e.starts[f.block]]
		}
		delta := target - offsets[f.at] - e.code[f.at].Width()
		if delta < math.MinInt16 || delta > math.MaxInt16 {
			return nil, false
		}
		e.code[f.at].SetOperand(f.operand, uint64(delta))
	}
	return instr.Marshal(e.code), true
}

// write appends one instruction, folding a store the very next read reads
// back into the one opcode bytecode has for both: LOCAL_SET n, LOCAL_GET n is
// LOCAL_TEE n. A value stored into its home and used again straight away is
// what every homed result looks like, so the fold is what keeps a home from
// costing an instruction the bytecode it came from never spent.
func (e *emitter) write(inst instr.Instruction) {
	if inst.Opcode() == instr.LOCAL_GET && len(e.code) > e.begin {
		if last := e.code[len(e.code)-1]; last.Opcode() == instr.LOCAL_SET && last.Operand(0) == inst.Operand(0) {
			e.code[len(e.code)-1] = instr.New(instr.LOCAL_TEE, inst.Operand(0))
			return
		}
	}
	e.code = append(e.code, inst)
}

// resolve returns what a value reads back as once every guard between its
// definition and here is gone.
func (e *emitter) resolve(v ssa.Value) ssa.Value {
	for {
		at, ok := e.subst[v]
		if !ok {
			return v
		}
		v = at
	}
}

func (e *emitter) list(vs []ssa.Value) []ssa.Value {
	out := make([]ssa.Value, len(vs))
	for i, v := range vs {
		out[i] = e.resolve(v)
	}
	return out
}

// operands names every value a terminator reads, on its own and along its
// edges.
func operands(t ssa.Terminator) []ssa.Value {
	out := slices.Clone(t.Args)
	for _, edge := range t.Edges {
		out = append(out, edge.Args...)
	}
	return out
}

// declared is the type a local holding an SSA value is declared with. A
// reference takes the widest declaration, since the IR types every reference
// alike.
func declared(t ssa.Type) (types.Type, bool) {
	switch t {
	case ssa.TypeI1:
		return types.TypeI1, true
	case ssa.TypeI8:
		return types.TypeI8, true
	case ssa.TypeI32:
		return types.TypeI32, true
	case ssa.TypeI64:
		return types.TypeI64, true
	case ssa.TypeF32:
		return types.TypeF32, true
	case ssa.TypeF64:
		return types.TypeF64, true
	case ssa.TypeRef:
		return types.TypeAny, true
	default:
		return nil, false
	}
}
