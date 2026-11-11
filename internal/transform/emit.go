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
	declared  []types.Type
	captures  []types.Type

	subst    map[ssa.Value]ssa.Value
	ops      [][]ssa.Operation
	terms    []ssa.Terminator
	uses     map[ssa.Value]int
	defined  map[ssa.Value]int
	homed    map[ssa.Value]bool
	home     map[ssa.Value]int
	source   map[ssa.Value]ssa.Operation
	remat    map[ssa.Value]bool
	loops    map[int]bool
	liveIn   []map[ssa.Value]bool
	placed   bool
	order    []int
	after    []int
	settled  map[ssa.Value]bool
	spare    map[ssa.Value]bool
	produced map[ssa.Value]bool
	used     map[ssa.Value]int
	refs     map[ssa.Value]types.Type

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

var inverses = map[instr.Opcode]instr.Opcode{
	instr.I32_EQ: instr.I32_NE, instr.I32_NE: instr.I32_EQ,
	instr.I32_LT_S: instr.I32_GE_S, instr.I32_GE_S: instr.I32_LT_S,
	instr.I32_GT_S: instr.I32_LE_S, instr.I32_LE_S: instr.I32_GT_S,
	instr.I32_LT_U: instr.I32_GE_U, instr.I32_GE_U: instr.I32_LT_U,
	instr.I32_GT_U: instr.I32_LE_U, instr.I32_LE_U: instr.I32_GT_U,
	instr.I64_EQ: instr.I64_NE, instr.I64_NE: instr.I64_EQ,
	instr.I64_LT_S: instr.I64_GE_S, instr.I64_GE_S: instr.I64_LT_S,
	instr.I64_GT_S: instr.I64_LE_S, instr.I64_LE_S: instr.I64_GT_S,
	instr.I64_LT_U: instr.I64_GE_U, instr.I64_GE_U: instr.I64_LT_U,
	instr.I64_GT_U: instr.I64_LE_U, instr.I64_LE_U: instr.I64_GT_U,
	instr.F32_EQ: instr.F32_NE, instr.F32_NE: instr.F32_EQ,
	instr.F64_EQ: instr.F64_NE, instr.F64_NE: instr.F64_EQ,
}

var spaceOps = [...]struct{ read, write instr.Opcode }{
	ssa.SpaceLocal:  {instr.LOCAL_GET, instr.LOCAL_SET},
	ssa.SpaceGlobal: {instr.GLOBAL_GET, instr.GLOBAL_SET},
	ssa.SpaceUpval:  {instr.UPVAL_GET, instr.UPVAL_SET},
}

func emit(function *ssa.Function, constants *pool, module bool, source *types.Function) ([]byte, []types.Type, bool) {
	e := &emitter{function: function, constants: constants, module: module, declared: source.Declared(), captures: source.Captures}
	if !e.collect() {
		return nil, nil, false
	}
	e.layout()
	e.liveness()
	for range len(e.uses) + 1 {
		e.params()
		if !e.provision() {
			return nil, nil, false
		}
		walked := e.walk()
		if e.failed {
			return nil, nil, false
		}
		if walked {
			if !e.assign() || !e.walk() || e.failed {
				return nil, nil, false
			}
			code, ok := e.link()
			return code, e.locals, ok
		}
		if e.spare[e.needed] {
			delete(e.spare, e.needed)
			e.homed[e.needed] = true
			continue
		}
		if _, ok := e.source[e.needed]; ok {
			if e.remat[e.needed] {
				return nil, nil, false
			}
			e.remat[e.needed] = true
			continue
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
	e.source, e.remat = map[ssa.Value]ssa.Operation{}, map[ssa.Value]bool{}
	e.spare, e.produced = map[ssa.Value]bool{}, map[ssa.Value]bool{}
	stored := e.stored()
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
			if operation.Op == ssa.OpConst || (operation.Op == ssa.OpLoad && operation.Slot.Space == ssa.SpaceLocal && stored != nil && !stored[operation.Slot.Index]) {
				e.source[operation.Results[0]] = operation
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
	e.headers()
	e.refs = e.refTypes()
	for v, n := range e.uses {
		switch _, ok := e.source[v]; {
		case n <= 1:
		case ok:
			e.remat[v] = true
		case e.produced[v] && !e.homed[v]:
			e.spare[v] = true
		default:
			e.homed[v] = true
		}
	}
	return true
}

// refTypes returns the static type of each ref value whose definition
// guarantees one, which a local holding it can then declare; a ref left out is
// any. A load has its slot's declared type, a constant its cell's type, a
// block param the type all its incoming values share, and an array slice the
// type of the array it slices. A value with none of these takes the type of
// the local slots it is stored to when every store sits in its defining block
// ahead of every other use, so each use reads the value as the slot narrowed
// it; a store on another path never types it.
//
// Params and slices are solved together as a lattice: unknown below every
// type, a type below any. A value is the join of its sources, a param of its
// incoming values and a slice of its array, and a source that is not a load,
// constant, stored value, param or slice is any. Values only rise, so the
// least fixpoint is the answer whatever order they are visited in, and a value
// still unknown at the end is any.
func (e *emitter) refTypes() map[ssa.Value]types.Type {
	fixed, sources := map[ssa.Value]types.Type{}, map[ssa.Value][]ssa.Value{}
	type store struct {
		at int
		t  types.Type
	}
	stores, other := map[ssa.Value][]store{}, map[ssa.Value]bool{}
	for id, ops := range e.ops {
		for i, operation := range ops {
			switch {
			case operation.Op == ssa.OpExec && operation.Code == instr.ARRAY_SLICE:
				sources[operation.Results[0]] = []ssa.Value{operation.Args[0]}
			case operation.Op == ssa.OpLoad && e.function.Type(operation.Results[0]) == ssa.TypeRef:
				if t := e.slotType(operation.Slot); t != nil {
					fixed[operation.Results[0]] = t
				}
			case operation.Op == ssa.OpConst && e.function.Type(operation.Results[0]) == ssa.TypeRef:
				if t := e.constants.typeOf(types.Boxed(operation.Const)); t != nil {
					fixed[operation.Results[0]] = t
				}
			case operation.Op == ssa.OpStore && operation.Slot.Space == ssa.SpaceLocal && operation.Slot.Index < len(e.declared):
				if v := operation.Args[0]; e.produced[v] && e.function.Type(v) == ssa.TypeRef && e.defined[v] == id {
					stores[v] = append(stores[v], store{i, e.declared[operation.Slot.Index]})
				} else {
					other[v] = true
				}
			}
		}
	}
	for _, term := range e.terms {
		for _, edge := range term.Edges {
			for i, param := range e.function.Block(edge.Block).Params {
				if e.function.Type(param) == ssa.TypeRef {
					sources[param] = append(sources[param], edge.Args[i])
				}
			}
		}
	}
	for v, list := range stores {
		if _, ok := fixed[v]; ok || other[v] || sources[v] != nil {
			continue
		}
		last, t := 0, list[0].t
		for _, s := range list {
			last = max(last, s.at)
			if !s.t.Equals(t) {
				t = types.TypeAny()
			}
		}
		early := false
		for _, operation := range e.ops[e.defined[v]][:last] {
			if operation.Op != ssa.OpStore && slices.Contains(operation.Args, v) {
				early = true
			}
		}
		if !early {
			fixed[v] = t
		}
	}

	solved := map[ssa.Value]types.Type{}
	for changed := true; changed; {
		changed = false
		for v, args := range sources {
			var joined types.Type
			for _, arg := range args {
				t := types.TypeAny()
				if _, ok := sources[arg]; ok {
					t = solved[arg]
				} else if f, ok := fixed[arg]; ok {
					t = f
				}
				switch {
				case t == nil:
				case joined == nil:
					joined = t
				case !joined.Equals(t):
					joined = types.TypeAny()
				}
			}
			if joined != nil && (solved[v] == nil || !solved[v].Equals(joined)) {
				solved[v], changed = joined, true
			}
		}
	}

	refs := fixed
	for v := range sources {
		refs[v] = types.TypeAny()
		if t := solved[v]; t != nil {
			refs[v] = t
		}
	}
	return refs
}

// slotType returns the declared type of slot, or nil when none is known.
func (e *emitter) slotType(slot ssa.Slot) types.Type {
	var declared []types.Type
	switch slot.Space {
	case ssa.SpaceLocal:
		declared = e.declared
	case ssa.SpaceGlobal:
		declared = e.constants.prog.Globals
	case ssa.SpaceUpval:
		declared = e.captures
	}
	if slot.Index < 0 || slot.Index >= len(declared) {
		return nil
	}
	return declared[slot.Index]
}

// headers marks the blocks that only jumps reach and a later block jumps back
// to. Their params live in locals, written by each jump that enters them, so
// no value travels on the operand stack across a back edge.
func (e *emitter) headers() {
	e.loops = map[int]bool{}
	reached, back := map[int]bool{}, map[int]bool{}
	for id, term := range e.terms {
		for _, edge := range term.Edges {
			if term.Op != ssa.OpJump {
				reached[edge.Block] = true
			}
			if edge.Block <= id {
				back[edge.Block] = true
			}
		}
	}
	for id := range back {
		if !reached[id] && len(e.function.Block(id).Params) > 0 {
			e.loops[id] = true
		}
	}
}

// stored reports the local slots some operation may write, or nil when an
// opcode writes locals the IR does not name.
func (e *emitter) stored() map[int]bool {
	stored := map[int]bool{}
	for id := range e.function.Len() {
		for _, operation := range e.function.Block(id).Operations {
			switch {
			case operation.Op == ssa.OpStore && operation.Slot.Space == ssa.SpaceLocal:
				stored[operation.Slot.Index] = true
			case operation.Op == ssa.OpExec && operation.Code.Writes(instr.Local):
				return nil
			}
		}
	}
	return stored
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
		e.produced[v] = true
	}
	e.ops[id] = append(e.ops[id], operation)
	return true
}

func (e *emitter) count(id int, values []ssa.Value) {
	for _, v := range values {
		e.uses[v]++
		if e.defined[v] == id {
			continue
		}
		if _, ok := e.source[v]; ok {
			e.remat[v] = true
		} else {
			e.homed[v] = true
		}
	}
}

func (e *emitter) params() {
	for id := range e.function.Len() {
		params := e.function.Block(id).Params
		homed := e.loops[id]
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

// provision gives every homed value a placeholder local: whether a walk
// finds a value out of stack order does not depend on which local it has, so
// the retries that discover the homed set skip the costly assignment. The
// placeholders say nothing about which values share a local, so a walk before
// assign skips only moves that cannot change.
func (e *emitter) provision() bool {
	if e.module && len(e.homed) > 0 {
		return false
	}
	e.placed = false
	e.home = make(map[ssa.Value]int, len(e.homed))
	for v := range e.homed {
		e.home[v] = 0
	}
	return true
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

	conflicts, hints := e.conflicts(), e.hints()
	slots := e.free()
	holders := map[int][]ssa.Value{}
	for _, v := range values {
		t, ok := local(e.function.Type(v))
		if !ok {
			return false
		}
		if ref, ok := e.refs[v]; ok {
			t = ref
		}
		fits := func(s int) bool {
			return e.slot(s).Equals(t) && !slices.ContainsFunc(holders[s], func(w ssa.Value) bool { return conflicts[v][w] })
		}
		slot := slices.IndexFunc(slots, func(s int) bool {
			home, ok := e.home[hints[v]]
			return ok && s == home && fits(s)
		})
		if slot < 0 {
			slot = slices.IndexFunc(slots, fits)
		}
		if slot < 0 {
			slots = append(slots, len(e.declared)+len(e.locals))
			slot = len(slots) - 1
			if slots[slot] > math.MaxUint8 {
				return false
			}
			e.locals = append(e.locals, t)
		}
		slot = slots[slot]
		e.home[v] = slot
		holders[slot] = append(holders[slot], v)
	}
	e.placed = true
	return true
}

// hints maps each value a jump passes to a loop header to the param it
// becomes: sharing the param's local makes the move a no-op.
func (e *emitter) hints() map[ssa.Value]ssa.Value {
	hints := map[ssa.Value]ssa.Value{}
	for _, term := range e.terms {
		if term.Op != ssa.OpJump || !e.loops[term.Edges[0].Block] {
			continue
		}
		for i, param := range e.function.Block(term.Edges[0].Block).Params {
			if _, ok := hints[term.Edges[0].Args[i]]; !ok {
				hints[term.Edges[0].Args[i]] = param
			}
		}
	}
	return hints
}

// free returns the declared local slots no emitted operation reads or writes:
// promotion left them unused, so a homed value may take one.
func (e *emitter) free() []int {
	touched := map[int]bool{}
	for _, ops := range e.ops {
		for _, operation := range ops {
			switch {
			case operation.Op == ssa.OpExec && (operation.Code.Reads(instr.Local) || operation.Code.Writes(instr.Local)):
				return nil
			case operation.Op == ssa.OpStore && operation.Slot.Space == ssa.SpaceLocal,
				operation.Op == ssa.OpLoad && operation.Slot.Space == ssa.SpaceLocal && e.uses[operation.Results[0]] > 0:
				touched[operation.Slot.Index] = true
			}
		}
	}
	var slots []int
	for slot := range e.declared {
		if !touched[slot] {
			slots = append(slots, slot)
		}
	}
	return slots
}

// conflicts returns, for each homed value, the homed values that are live
// where it is written; two such values cannot share a local.
func (e *emitter) conflicts() map[ssa.Value]map[ssa.Value]bool {
	conflicts := map[ssa.Value]map[ssa.Value]bool{}
	link := func(a, b ssa.Value) {
		if a == b {
			return
		}
		for _, v := range [2][2]ssa.Value{{a, b}, {b, a}} {
			if conflicts[v[0]] == nil {
				conflicts[v[0]] = map[ssa.Value]bool{}
			}
			conflicts[v[0]][v[1]] = true
		}
	}
	for id := range e.liveIn {
		e.live(id, func(v ssa.Value) bool { return e.homed[v] }, link)
	}
	return conflicts
}

// liveness computes, once, the values live on entering each block: every
// value a homing can ever name, which is every one but the rematerialized.
func (e *emitter) liveness() {
	e.liveIn = make([]map[ssa.Value]bool, e.function.Len())
	for id := range e.liveIn {
		e.liveIn[id] = map[ssa.Value]bool{}
	}
	tracked := func(v ssa.Value) bool {
		_, ok := e.source[v]
		return !ok
	}
	for changed := true; changed; {
		changed = false
		for id := len(e.liveIn) - 1; id >= 0; id-- {
			live := e.live(id, tracked, nil)
			if len(live) != len(e.liveIn[id]) {
				e.liveIn[id], changed = live, true
			}
		}
	}
}

// live returns the values keep admits that are live on entering block id,
// and links every such value written in the block to the values live as it is
// written.
func (e *emitter) live(id int, keep func(ssa.Value) bool, link func(a, b ssa.Value)) map[ssa.Value]bool {
	live := map[ssa.Value]bool{}
	use := func(values []ssa.Value) {
		for _, v := range values {
			if keep(v) {
				live[v] = true
			}
		}
	}
	define := func(values []ssa.Value) {
		for _, v := range values {
			if link == nil || !keep(v) {
				continue
			}
			for w := range live {
				link(v, w)
			}
			for _, w := range values {
				if keep(w) {
					link(v, w)
				}
			}
		}
		for _, v := range values {
			delete(live, v)
		}
	}

	term := e.terms[id]
	for _, edge := range term.Edges {
		for v := range e.liveIn[edge.Block] {
			if keep(v) {
				live[v] = true
			}
		}
	}
	use(term.Args)
	for _, edge := range term.Edges {
		use(edge.Args)
	}
	for i := len(e.ops[id]) - 1; i >= 0; i-- {
		operation := e.ops[id][i]
		define(operation.Results)
		use(operation.Args)
	}
	define(e.function.Block(id).Params)
	return live
}

func (e *emitter) walk() bool {
	e.code, e.fixes, e.needed = nil, nil, ssa.NoValue
	e.used, e.settled = map[ssa.Value]int{}, map[ssa.Value]bool{}
	e.starts = make([]int, e.function.Len())
	for _, id := range e.order {
		e.starts[id], e.floor = len(e.code), len(e.code)
		if !e.open(id) {
			return false
		}
		for i, operation := range e.ops[id] {
			if operation.Op != ssa.OpStore && operation.Op != ssa.OpExec && (e.remat[operation.Results[0]] || e.uses[operation.Results[0]] == 0) {
				continue
			}
			if !e.perform(operation) {
				return false
			}
			e.settle(id, i)
		}
		if !e.close(id) {
			return false
		}
	}
	return true
}

// layout orders the blocks for emission: block 0 first, then, after a branch,
// the successor the code falls through to, else the lowest block not yet
// placed. Of a branch's two successors, the one with the nearer path back to
// the branch's own block follows it, so a loop's body does not sit behind an
// exit; the taken edge qualifies only when the comparison has an exact
// inverse. A block that ends the module goes last.
func (e *emitter) layout() {
	n := e.function.Len()
	placed := make([]bool, n)
	e.order, e.after = nil, make([]int, n)
	for len(e.order) < n {
		next := -1
		if len(e.order) > 0 {
			next = e.following(e.order[len(e.order)-1], placed)
		}
		for id := range n {
			if next < 0 && !placed[id] && (len(e.order) == 0 || e.terms[id].Op != ssa.OpComplete) {
				next = id
			}
		}
		for id := range n {
			if next < 0 && !placed[id] {
				next = id
			}
		}
		placed[next] = true
		e.order = append(e.order, next)
	}
	for i, id := range e.order {
		e.after[id] = -1
		if i+1 < n {
			e.after[id] = e.order[i+1]
		}
	}
}

// following returns the block to place after id, or -1 when id does not end
// in a branch with such a successor.
func (e *emitter) following(id int, placed []bool) int {
	term := e.terms[id]
	if term.Op != ssa.OpBranch || len(term.Edges) != 2 {
		return -1
	}
	candidates := []int{term.Edges[1].Block}
	if last := len(e.ops[id]) - 1; last >= 0 && len(term.Args) == 1 && len(e.ops[id][last].Results) == 1 && e.ops[id][last].Results[0] == term.Args[0] {
		if _, ok := inverses[e.ops[id][last].Code]; ok && e.ops[id][last].Op == ssa.OpExec {
			candidates = append(candidates, term.Edges[0].Block)
		}
	}
	best, nearest := -1, 0
	for _, c := range candidates {
		if placed[c] || e.terms[c].Op == ssa.OpComplete {
			continue
		}
		// The distance from c back to id, in blocks, or 0 when none leads back.
		distance, seen, frontier := 0, map[int]bool{c: true}, []int{c}
		for depth := 1; len(frontier) > 0 && distance == 0; depth++ {
			var next []int
			for _, b := range frontier {
				for _, edge := range e.terms[b].Edges {
					if edge.Block == id {
						distance = depth
					}
					if !seen[edge.Block] {
						seen[edge.Block] = true
						next = append(next, edge.Block)
					}
				}
			}
			frontier = next
		}
		if best < 0 || distance > 0 && (nearest == 0 || distance < nearest) || distance == nearest && c < best {
			best, nearest = c, distance
		}
	}
	return best
}

func (e *emitter) open(id int) bool {
	params := e.function.Block(id).Params
	e.stack = nil
	if id == 0 {
		return len(params) == 0
	}
	if len(params) == 0 || e.loops[id] {
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

// settle stores operation's result to the loop param it feeds as soon as it is
// computed, so the code runs the producer and its store back to back, when
// nothing computed after it reads that param's local. The final walk only: a
// placeholder home says nothing about which values share a local.
func (e *emitter) settle(id, index int) {
	term := e.terms[id]
	if !e.placed || term.Op != ssa.OpJump || !e.loops[term.Edges[0].Block] {
		return
	}
	edge, operation := term.Edges[0], e.ops[id][index]
	if len(operation.Results) != 1 || len(e.stack) == 0 {
		return
	}
	v := operation.Results[0]
	at := slices.Index(edge.Args, v)
	if at < 0 || e.stack[len(e.stack)-1] != v || e.uses[v] != 1 || e.homed[v] || e.remat[v] {
		return
	}
	dest := e.home[e.function.Block(edge.Block).Params[at]]
	reads := func(w ssa.Value) bool {
		source, ok := e.source[w]
		return e.home[w] == dest && e.homed[w] ||
			ok && e.remat[w] && source.Op == ssa.OpLoad && source.Slot.Space == ssa.SpaceLocal && source.Slot.Index == dest
	}
	for _, w := range edge.Args {
		if w != v && !e.settled[w] && reads(w) {
			return
		}
	}
	for _, later := range e.ops[id][index+1:] {
		if later.Slot.Space == ssa.SpaceLocal && later.Slot.Index == dest && (later.Op == ssa.OpLoad || later.Op == ssa.OpStore) || slices.ContainsFunc(later.Args, reads) {
			return
		}
	}
	e.used[v]++
	e.write(instr.LOCAL_SET, uint64(dest))
	e.stack = e.stack[:len(e.stack)-1]
	e.settled[v] = true
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
		if e.after[id] >= 0 {
			e.branch(instr.BR, e.function.Len())
		}
	case ssa.OpJump:
		edge := term.Edges[0]
		args, moves := edge.Args, []int(nil)
		if e.loops[edge.Block] {
			args = nil
			for i, param := range e.function.Block(edge.Block).Params {
				// An argument already in its param's local, or stored as it was computed, needs no move.
				if home, ok := e.home[edge.Args[i]]; ok && (edge.Args[i] == param || e.placed && home == e.home[param]) || e.settled[edge.Args[i]] {
					continue
				}
				args, moves = append(args, edge.Args[i]), append(moves, e.home[param])
			}
		}
		if !e.carry(args) {
			return false
		}
		for i := len(moves) - 1; i >= 0; i-- {
			e.write(instr.LOCAL_SET, uint64(moves[i]))
		}
		e.stack = e.stack[:len(e.stack)-len(moves)]
		e.leave(id, edge.Block)
	case ssa.OpBranch, ssa.OpTable:
		if !uniform(term.Edges) || !e.carry(append(slices.Clone(term.Edges[0].Args), term.Args[0])) {
			return false
		}
		e.stack = e.stack[:len(e.stack)-1]
		if term.Op == ssa.OpTable {
			e.table(term.Edges)
			return true
		}
		last := len(e.code) - 1
		if last >= e.floor && term.Edges[0].Block == e.after[id] {
			if inverse, ok := inverses[e.code[last].Opcode()]; ok {
				e.code[last] = instr.MustNew(inverse)
				e.branch(instr.BR_IF, term.Edges[1].Block)
				return true
			}
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
	for _, v := range args {
		e.used[v]++
	}
	at := 0
	for n := min(len(args), len(e.stack)); n > 0; n-- {
		if slices.Equal(e.stack[len(e.stack)-n:], args[:n]) {
			at = n
			break
		}
	}
	for _, v := range args[at:] {
		if e.remat[v] {
			if !e.perform(e.source[v]) {
				return false
			}
			continue
		}
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

func (e *emitter) perform(operation ssa.Operation) bool {
	if !e.load(operation.Args) {
		return false
	}
	// A value this operation alone consumes off the stack, with a use to
	// come, is duplicated rather than homed; a later use that is not next on
	// the stack makes the walk fail and homes it. An operation that can trap
	// or enter a frame keeps the stack its caller left, which a trap exposes.
	if len(operation.Args) == 1 && len(operation.Results) == 0 && (operation.Op != ssa.OpExec || !operation.Code.Traps() && !operation.Code.Writes(instr.Frame)) && e.spare[operation.Args[0]] && e.used[operation.Args[0]] < e.uses[operation.Args[0]] {
		e.write(instr.DUP)
		e.stack = append(e.stack, operation.Args[0])
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
	if next != e.after[id] {
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
		// A reload of a typed ref recovers the type the local declares; a tee
		// would keep the value's own, which the analysis may not know.
		t := e.slot(int(operands[0]))
		typed := t.Kind() == types.KindRef && !t.Equals(types.TypeAny())
		if last := e.code[len(e.code)-1]; last.Opcode() == instr.LOCAL_SET && last.Operand(0) == operands[0] && !typed {
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

// slot returns the type of local slot, declared or new.
func (e *emitter) slot(slot int) types.Type {
	if slot < len(e.declared) {
		return e.declared[slot]
	}
	if slot-len(e.declared) < len(e.locals) {
		return e.locals[slot-len(e.declared)]
	}
	return types.TypeAny()
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
