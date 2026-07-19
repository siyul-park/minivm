package interp

import (
	"sort"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

type anchor struct {
	addr int
	ip   int
}

type shape struct {
	itab uintptr
	typ  uintptr
}

type step struct {
	op       instr.Opcode
	args     [2]uint64
	seen     types.Boxed
	arg      types.Boxed
	shape    shape
	terminal bool

	fn     int
	ip     int
	depth  int
	callee int
	known  bool
}

type compileInput struct {
	tracer    *Tracer
	address   int
	function  *types.Function
	module    *types.Function
	constants []types.Boxed
	globals   []types.Kind
	heap      []types.Value
	installed bool
}

type plan struct {
	anchor  anchor
	kind    entryKind
	root    int
	blocks  []block
	hoist   *hoist
	noSpill bool
}

// hoist marks one loop-invariant container: a ref local that every block
// leaves untouched, so the backend may derive its heap cell, shape guard, and
// slice header once per native entry instead of once per access. want is the
// recorded primitive-array itab; the planner admits only backend-supported
// accesses whose root-frame slot fits the ARM64 load immediate.
type hoist struct {
	local int
	want  uintptr
}

type entryKind uint8

type block struct {
	anchor anchor
	tail   bool
	state  []slot
	steps  []step
	term   terminator
}

type slot struct {
	kind        types.Kind
	backing     backing
	slot        int
	ref         int
	refKnown    bool
	callee      int
	calleeKnown bool
	styp        *types.StructType
	val         int32
	valKnown    bool
}

type edge struct {
	anchor anchor
	block  int
	tail   []int
}

type terminator struct {
	kind  terminatorKind
	ip    int
	hot   int
	edges []edge
}

type terminatorKind uint8

const (
	entryInvalid entryKind = iota
	entryFunction
	entryLoop
	entryModule
)

const (
	terminateFallthrough terminatorKind = iota
	terminateBranch
	terminateBranchIf
	terminateBranchTable
	terminateReturn
	terminateComplete
	terminateFallback
)

const (
	noBlock      = -1
	maxHoistSlot = 4095
)

func input(i *Interpreter, addr int) (*compileInput, bool) {
	fn, ok := i.function(addr)
	if !ok || fn == nil || len(fn.Code) == 0 {
		return nil, false
	}
	return &compileInput{
		tracer:    i.tracer,
		address:   addr,
		function:  fn,
		module:    i.module,
		constants: i.constants,
		globals:   i.globalKinds(),
		heap:      i.heap,
		installed: i.stub(addr) != nil,
	}, true
}

func (kind entryKind) profile() prof.EntryKind {
	switch kind {
	case entryModule:
		return prof.EntryStart
	case entryFunction:
		return prof.EntryCall
	case entryLoop:
		return prof.EntryLoop
	default:
		return prof.EntryNone
	}
}

func (p plan) valid() bool {
	if p.root < 0 || p.root >= len(p.blocks) || p.blocks[p.root].tail || p.blocks[p.root].anchor != p.anchor {
		return false
	}
	switch p.kind {
	case entryFunction:
		if p.anchor.addr <= 0 || p.anchor.ip != 0 {
			return false
		}
	case entryLoop:
		if p.anchor.addr < 0 || p.anchor.ip <= 0 {
			return false
		}
	case entryModule:
		if p.anchor.addr != 0 || p.anchor.ip != 0 {
			return false
		}
	default:
		return false
	}
	for _, block := range p.blocks {
		switch block.term.kind {
		case terminateFallthrough, terminateReturn, terminateComplete, terminateFallback:
			if len(block.term.edges) != 0 {
				return false
			}
		case terminateBranch:
			if len(block.term.edges) != 1 {
				return false
			}
		case terminateBranchIf:
			if len(block.term.edges) != 2 {
				return false
			}
		case terminateBranchTable:
			if len(block.term.edges) == 0 {
				return false
			}
		default:
			return false
		}
		if block.term.hot < -1 || (len(block.term.edges) > 0 && block.term.hot >= len(block.term.edges)) {
			return false
		}
		for _, edge := range block.term.edges {
			if edge.block != noBlock {
				if edge.block < 0 || edge.block >= len(p.blocks) || p.blocks[edge.block].anchor != edge.anchor {
					return false
				}
			}
			for _, id := range edge.tail {
				if id < 0 || id >= len(p.blocks) || !p.blocks[id].tail {
					return false
				}
			}
		}
	}
	return true
}

func staticPlan(input *compileInput) ([]plan, error) {
	if input == nil || input.function == nil || len(input.function.Code) == 0 || input.installed {
		return nil, nil
	}
	if input.address == 0 {
		for ip := 0; ip < len(input.function.Code); {
			inst := instr.Instruction(input.function.Code[ip:])
			if inst.Opcode() == instr.CALL || inst.Opcode() == instr.RETURN_CALL {
				return nil, nil
			}
			ip += inst.Width()
		}
	}

	blocks, err := analysis.Blocks(input.function)
	if err != nil {
		return nil, err
	}
	constants, heap := input.constants, input.heap
	facts, ok := planStates(input.function, blocks, constants, input.globals, heap)
	if !ok {
		return nil, nil
	}

	entryType := entryFunction
	if input.address == 0 {
		entryType = entryModule
	}
	result := plan{anchor: anchor{addr: input.address}, kind: entryType}
	result.blocks = make([]block, len(blocks))
	locals := localTypes(input.function)
	for idx, source := range blocks {
		target := block{anchor: anchor{addr: input.address, ip: source.Start}}
		target.state = append([]slot{}, facts[idx]...)
		flow := append([]slot(nil), facts[idx]...)
		for ip := source.Start; ip < source.End; {
			inst := instr.Instruction(input.function.Code[ip:])
			next := ip + inst.Width()
			step := step{op: inst.Opcode(), args: args(inst), fn: input.address, ip: ip}
			if (inst.Opcode() == instr.CALL || inst.Opcode() == instr.RETURN_CALL) && len(flow) > 0 {
				callee := flow[len(flow)-1]
				if callee.calleeKnown {
					step.callee = callee.callee
					step.known = true
				}
			}
			if inst.Opcode() == instr.CONST_GET {
				constant := int(inst.Operand(0))
				if constant < len(constants) && constants[constant].Kind() == types.KindRef {
					step.known = true
				}
			}
			// Static steps carry no recorded observation, but structGet reads
			// op.seen.Kind() for the result kind; synthesize the zero boxed
			// value of the statically resolved field kind. Runtime itab, type,
			// and per-field kind guards keep the lowering sound regardless.
			if inst.Opcode() == instr.STRUCT_GET && len(flow) >= 2 {
				if kind, ok := structFieldKind(heap, flow[len(flow)-2], flow[len(flow)-1]); ok {
					if seen, ok := zeroBoxed(kind); ok {
						step.seen = seen
					}
				}
			}
			switch inst.Opcode() {
			case instr.BR:
				target.term = terminator{kind: terminateBranch, ip: ip, hot: -1, edges: jumps(input.address, instr.Targets(input.function.Code, ip))}
			case instr.BR_IF:
				target.term = terminator{kind: terminateBranchIf, ip: ip, hot: -1, edges: jumps(input.address, append(instr.Targets(input.function.Code, ip), next))}
			case instr.BR_TABLE:
				target.term = terminator{kind: terminateBranchTable, ip: ip, hot: -1, edges: jumps(input.address, instr.Targets(input.function.Code, ip))}
			case instr.RETURN:
				target.term = terminator{kind: terminateReturn, ip: ip}
			default:
				target.steps = append(target.steps, step)
			}
			if !applyStep(input.function, locals, constants, input.globals, heap, &flow, inst) {
				return nil, nil
			}
			ip = next
		}
		if target.term.kind == terminateFallthrough {
			if source.End == len(input.function.Code) {
				if input.address == 0 {
					target.term = terminator{kind: terminateComplete, ip: source.End}
				} else {
					target.term = terminator{kind: terminateReturn, ip: source.End}
				}
			} else {
				target.term = terminator{kind: terminateBranch, ip: source.End, hot: -1, edges: []edge{jump(input.address, source.End)}}
			}
		}
		result.blocks[idx] = target
	}
	roots := make(map[anchor]int, len(result.blocks))
	for id, block := range result.blocks {
		roots[block.anchor] = id
	}
	wire(&result, roots)
	return []plan{result}, nil
}

func tracePlan(input *compileInput) ([]plan, error) {
	if input == nil || input.tracer == nil || input.function == nil {
		return nil, nil
	}
	var plans []plan
	for _, ip := range input.tracer.anchors(input.address) {
		a := anchor{addr: input.address, ip: ip}
		tree := input.tracer.rootAt(a)
		if tree == nil || tree.root == nil || tree.root.status == aborted {
			continue
		}
		// A loop anchor accepts a looping root or a returned straight-line
		// fragment: a body whose terminal boundary (bulk mutation, yield,
		// throw) deopts before the back-edge still compiles as a per-entry
		// prefix that re-enters at the header next iteration. Carried entry
		// operands stay rejected either way.
		if ip != 0 && (tree.root.status != loop && tree.root.status != returned || tree.root.carried) {
			continue
		}
		if ip == 0 && tree.root.status == loop {
			continue
		}
		kind := entryFunction
		if input.address == 0 {
			kind = entryModule
		}
		if ip != 0 {
			kind = entryLoop
		}
		planned := plan{anchor: a, kind: kind, root: -1}
		root := store(&planned, split(&planned, tree.root, input), false)
		if len(root) == 0 {
			continue
		}
		planned.root = root[0]
		roots := map[anchor]int{tree.root.anchor: root[0]}

		type leg struct {
			trace *trace
			hits  int64
		}
		var legs []leg
		for id, tr := range tree.branches {
			if tr == nil || tr.status == aborted {
				continue
			}
			// A loop-kind leg is a loop root of its own: anchored at this
			// header it is the root trace itself (capture returns the existing
			// root, and its edge already wires to the root block); anchored
			// elsewhere it is a different loop, and splitting it here would
			// inline that whole loop body instead of using its native entry.
			if tr.status == loop {
				continue
			}
			hits := int64(0)
			if id >= 0 && id < len(tree.hits) {
				hits = tree.hits[id]
			}
			legs = append(legs, leg{trace: tr, hits: hits})
		}
		sort.SliceStable(legs, func(i, j int) bool {
			if legs[i].hits != legs[j].hits {
				return legs[i].hits > legs[j].hits
			}
			if legs[i].trace.anchor.addr != legs[j].trace.anchor.addr {
				return legs[i].trace.anchor.addr < legs[j].trace.anchor.addr
			}
			return legs[i].trace.anchor.ip < legs[j].trace.anchor.ip
		})
		for _, leg := range legs {
			ids := store(&planned, split(&planned, leg.trace, input), false)
			if len(ids) > 0 {
				roots[leg.trace.anchor] = ids[0]
			}
		}
		wire(&planned, roots)
		if kind == entryLoop {
			planned.hoist = hoistable(input.function, planned.blocks)
		}
		planned.noSpill = noSpill(planned.blocks)
		plans = append(plans, planned)
	}
	return plans, nil
}

// hoistable picks the most-accessed loop-invariant container for a loop plan.
// A local qualifies when it is a declared ref, no block writes it, and every
// recorded ARRAY_GET/ARRAY_SET on it observed one itab. Any call disqualifies
// the plan: a BL clobbers the hoisted registers and re-enters via ctx.back.
// Container provenance is a per-block marker stack. Variable-effect stack
// operators update markers explicitly; fixed-effect operators use instr.Type,
// and anything else clears the markers conservatively. Underflow also clears
// them — loop plans with carried entry operands are rejected before planning.
func hoistable(fn *types.Function, blocks []block) *hoist {
	locals := localTypes(fn)
	banned := make([]bool, len(locals))
	for _, block := range blocks {
		for _, step := range block.steps {
			switch step.op {
			case instr.CALL, instr.RETURN_CALL:
				return nil
			case instr.LOCAL_SET, instr.LOCAL_TEE:
				local := int(step.args[0])
				if local < len(banned) {
					banned[local] = true
				}
			}
		}
	}

	type candidate struct {
		want     uintptr
		hits     int
		conflict bool
	}
	candidates := make([]candidate, len(locals))
	for _, block := range blocks {
		var stack []int
		for _, step := range block.steps {
			record := func(depth int) {
				if depth > len(stack) || step.op == instr.ARRAY_SET && step.terminal {
					return
				}
				switch step.shape.itab {
				case itab(types.TypedArray[bool](nil)),
					itab(types.TypedArray[int8](nil)),
					itab(types.TypedArray[int32](nil)),
					itab(types.TypedArray[int64](nil)),
					itab(types.TypedArray[float32](nil)),
					itab(types.TypedArray[float64](nil)):
				default:
					return
				}
				local := stack[len(stack)-depth]
				if local < 0 || local >= len(locals) || local > maxHoistSlot || banned[local] {
					return
				}
				candidate := &candidates[local]
				if candidate.want != 0 && candidate.want != step.shape.itab {
					candidate.conflict = true
					return
				}
				candidate.want = step.shape.itab
				candidate.hits++
			}

			switch step.op {
			case instr.LOCAL_GET:
				stack = append(stack, int(step.args[0]))
				continue
			case instr.DUP:
				if len(stack) == 0 {
					stack = nil
				} else {
					stack = append(stack, stack[len(stack)-1])
				}
				continue
			case instr.SWAP:
				if len(stack) < 2 {
					stack = nil
				} else {
					stack[len(stack)-1], stack[len(stack)-2] = stack[len(stack)-2], stack[len(stack)-1]
				}
				continue
			case instr.SELECT:
				if len(stack) < 3 {
					stack = nil
				} else {
					a, b := stack[len(stack)-3], stack[len(stack)-2]
					stack = stack[:len(stack)-3]
					if a != b {
						a = -1
					}
					stack = append(stack, a)
				}
				continue
			case instr.LOCAL_TEE, instr.GLOBAL_TEE:
				continue
			case instr.CONST_GET, instr.GLOBAL_GET, instr.UPVAL_GET:
				stack = append(stack, -1)
				continue
			case instr.ARRAY_GET:
				record(2)
				if len(stack) < 2 {
					stack = nil
				} else {
					stack = append(stack[:len(stack)-2], -1)
				}
				continue
			case instr.ARRAY_SET:
				record(3)
				if len(stack) < 3 {
					stack = nil
				} else {
					stack = stack[:len(stack)-3]
				}
				continue
			}

			typ := instr.TypeOf(step.op)
			if typ.Pop == nil && typ.Push == nil {
				stack = nil
				continue
			}
			if n := len(typ.Pop); n >= len(stack) {
				stack = stack[:0]
			} else {
				stack = stack[:len(stack)-n]
			}
			for range typ.Push {
				stack = append(stack, -1)
			}
		}
	}

	best := -1
	for local, candidate := range candidates {
		if candidate.hits == 0 || candidate.conflict || locals[local].Kind() != types.KindRef {
			continue
		}
		if best < 0 || candidate.hits > candidates[best].hits {
			best = local
		}
	}
	if best < 0 {
		return nil
	}
	return &hoist{local: best, want: candidates[best].want}
}

func split(p *plan, tr *trace, input *compileInput) []block {
	if tr == nil {
		return nil
	}
	current := block{anchor: tr.anchor}
	var blocks []block
	rejoins := func(op record) bool {
		return tr.status == partial && p.kind == entryLoop && op.cut && op.depth == 0 &&
			op.fn == p.anchor.addr && op.target == p.anchor.ip
	}
	for idx, op := range tr.ops {
		if op.cut {
			// A leg cut at the loop plan's own header is the loop back-edge:
			// wire a real branch so wire() folds it onto the root block and
			// the lowering takes the committing-flush native back-edge instead
			// of a deopt round trip. Cuts inside an inlined frame keep the
			// fallback — the root block expects the anchor frame only.
			if rejoins(op) {
				current.term = terminator{kind: terminateBranch, ip: op.target, hot: 0, edges: []edge{jump(op.fn, op.target)}}
			} else {
				current.term = terminator{kind: terminateFallback, ip: op.target, hot: -1}
			}
			blocks = append(blocks, current)
			return blocks
		}
		path := -1
		switch op.op {
		case instr.BR:
			current.term = terminator{kind: terminateBranch, ip: op.ip, hot: 0, edges: []edge{jump(op.fn, op.target)}}
			path = 0
			blocks = append(blocks, current)
		case instr.BR_IF:
			next := op.ip + 3
			hot := 1
			if op.taken {
				hot = 0
			}
			edges := []edge{jump(op.fn, op.target), jump(op.fn, next)}
			edges[1-hot].tail = suffix(p, tr, idx, input)
			current.term = terminator{kind: terminateBranchIf, ip: op.ip, hot: hot, edges: edges}
			path = hot
			blocks = append(blocks, current)
		case instr.BR_TABLE:
			var targets []int
			if fn := resolve(input.module, input.heap, op.fn); fn != nil {
				targets = instr.Targets(fn.Code, op.ip)
			}
			hot := -1
			for n, target := range targets {
				if target == op.target {
					hot = n
					break
				}
			}
			edges := jumps(op.fn, targets)
			tail := suffix(p, tr, idx, input)
			for i := range edges {
				if i != hot {
					edges[i].tail = tail
				}
			}
			current.term = terminator{kind: terminateBranchTable, ip: op.ip, hot: hot, edges: edges}
			path = hot
			blocks = append(blocks, current)
		case instr.RETURN:
			if op.depth == 0 {
				current.term = terminator{kind: terminateReturn, ip: op.ip, hot: -1}
				blocks = append(blocks, current)
				return blocks
			}
			current.steps = append(current.steps, op.step)
			continue
		default:
			current.steps = append(current.steps, op.step)
			continue
		}
		if idx+1 >= len(tr.ops) {
			return blocks
		}
		next := tr.ops[idx+1]
		if path >= 0 {
			// A cut straight after the branch carries no new ops: the trace
			// took the branch and stopped. End the split with the hot edge
			// unresolved so wire() can fold it onto a known root block (the
			// loop back-edge) instead of chaining a spurious empty block.
			hot := &blocks[len(blocks)-1].term.edges[path]
			if rejoins(next) && hot.anchor == p.anchor {
				return blocks
			}
			hot.block = local(len(blocks))
		}
		current = block{anchor: anchor{addr: next.fn, ip: next.ip}}
	}
	if len(blocks) > 0 && len(current.steps) == 0 && current.term.kind == terminateFallthrough {
		return blocks
	}
	switch tr.status {
	case returned:
		current.term = terminator{kind: terminateFallthrough, hot: -1}
	case completed:
		current.term = terminator{kind: terminateComplete, hot: -1}
	case partial:
		resume := tr.anchor.ip
		if len(tr.ops) > 0 {
			resume = tr.ops[len(tr.ops)-1].target
		}
		current.term = terminator{kind: terminateFallback, ip: resume, hot: -1}
	case loop:
		current.term = terminator{kind: terminateBranch, ip: tr.anchor.ip, hot: 0, edges: []edge{{anchor: tr.anchor, block: local(0)}}}
	default:
		current.term = terminator{kind: terminateFallback, ip: tr.anchor.ip, hot: -1}
	}
	blocks = append(blocks, current)
	return blocks
}

func suffix(p *plan, tr *trace, idx int, input *compileInput) []int {
	depth := tr.ops[idx].depth
	for at := idx + 1; at < len(tr.ops); at++ {
		if tr.ops[at].depth >= depth {
			continue
		}
		tail := &trace{
			anchor: anchor{addr: tr.ops[at].fn, ip: tr.ops[at].ip},
			ops:    tr.ops[at:],
			status: tr.status,
		}
		return store(p, split(p, tail, input), true)
	}
	return nil
}

func local(id int) int {
	return -id - 2
}

func jumps(addr int, ips []int) []edge {
	edges := make([]edge, len(ips))
	for i, ip := range ips {
		edges[i] = jump(addr, ip)
	}
	return edges
}

func jump(addr, ip int) edge {
	return edge{anchor: anchor{addr: addr, ip: ip}, block: noBlock}
}

func resolve(module *types.Function, heap []types.Value, addr int) *types.Function {
	if addr == 0 {
		return module
	}
	if addr < 0 || addr >= len(heap) {
		return nil
	}
	fn, _ := heap[addr].(*types.Function)
	return fn
}

func store(p *plan, blocks []block, tail bool) []int {
	start := len(p.blocks)
	ids := make([]int, len(blocks))
	for i, block := range blocks {
		block.tail = tail
		ids[i] = start + i
		p.blocks = append(p.blocks, block)
	}
	for _, id := range ids {
		for i := range p.blocks[id].term.edges {
			edge := &p.blocks[id].term.edges[i]
			local, ok := localID(edge.block)
			if !ok {
				continue
			}
			if local < 0 || local >= len(ids) {
				edge.block = noBlock
				continue
			}
			edge.block = ids[local]
		}
	}
	return ids
}

func localID(block int) (int, bool) {
	if block >= noBlock {
		return 0, false
	}
	return -block - 2, true
}

func wire(p *plan, roots map[anchor]int) {
	for id := range p.blocks {
		for i := range p.blocks[id].term.edges {
			edge := &p.blocks[id].term.edges[i]
			if edge.block != noBlock {
				continue
			}
			if target, ok := roots[edge.anchor]; ok {
				edge.block = target
			}
		}
	}
}

func noSpill(blocks []block) bool {
	for _, block := range blocks {
		for _, step := range block.steps {
			switch step.op {
			case instr.ARRAY_SET, instr.STRUCT_SET:
				return true
			}
		}
	}
	return false
}

func planStates(fn *types.Function, blocks []*analysis.BasicBlock, constants []types.Boxed, globals []types.Kind, heap []types.Value) ([][]slot, bool) {
	if len(fn.Handlers) > 0 {
		return nil, false
	}
	if len(blocks) == 0 {
		return nil, true
	}
	locals := localTypes(fn)
	states := make([][]slot, len(blocks))
	seen := make([]bool, len(blocks))
	seen[0] = true
	work := []int{0}
	for len(work) > 0 {
		idx := work[len(work)-1]
		work = work[:len(work)-1]
		state := append([]slot(nil), states[idx]...)
		if !applyBlock(fn, locals, constants, globals, heap, blocks[idx], &state) {
			return nil, false
		}
		for _, succ := range blocks[idx].Succs {
			if !seen[succ] {
				seen[succ] = true
				states[succ] = append([]slot(nil), state...)
				work = append(work, succ)
				continue
			}
			if len(states[succ]) != len(state) {
				return nil, false
			}
			changed := false
			for pos := range state {
				merged, ok := mergeSlot(&states[succ][pos], state[pos])
				if !ok {
					return nil, false
				}
				changed = changed || merged
			}
			if changed {
				work = append(work, succ)
			}
		}
	}
	for idx := range states {
		if !seen[idx] {
			return nil, false
		}
	}
	return states, true
}

func mergeSlot(dst *slot, src slot) (bool, bool) {
	if dst.kind != src.kind {
		return false, false
	}
	changed := false
	if dst.backing != src.backing || dst.slot != src.slot {
		dst.backing, dst.slot = backingStack, 0
		changed = true
	}
	if dst.refKnown && (!src.refKnown || dst.ref != src.ref) {
		dst.ref, dst.refKnown = 0, false
		changed = true
	}
	if dst.calleeKnown && (!src.calleeKnown || dst.callee != src.callee) {
		dst.callee, dst.calleeKnown = 0, false
		changed = true
	}
	if dst.styp != nil && dst.styp != src.styp {
		dst.styp = nil
		changed = true
	}
	if dst.valKnown && (!src.valKnown || dst.val != src.val) {
		dst.val, dst.valKnown = 0, false
		changed = true
	}
	return changed, true
}

func applyBlock(fn *types.Function, locals []types.Type, constants []types.Boxed, globals []types.Kind, heap []types.Value, block *analysis.BasicBlock, state *[]slot) bool {
	for ip := block.Start; ip < block.End; {
		inst := instr.Instruction(fn.Code[ip:])
		if !applyStep(fn, locals, constants, globals, heap, state, inst) {
			return false
		}
		ip += inst.Width()
	}
	return true
}

func applyStep(fn *types.Function, locals []types.Type, constants []types.Boxed, globals []types.Kind, heap []types.Value, state *[]slot, inst instr.Instruction) bool {
	push := func(value slot) { *state = append(*state, value) }
	pop := func(count int) bool {
		if len(*state) < count {
			return false
		}
		*state = (*state)[:len(*state)-count]
		return true
	}
	switch inst.Opcode() {
	case instr.NOP, instr.UNREACHABLE, instr.BR:
		return true
	case instr.LOCAL_GET:
		idx := int(inst.Operand(0))
		if idx >= len(locals) {
			return false
		}
		styp, _ := locals[idx].(*types.StructType)
		push(slot{kind: locals[idx].Kind(), backing: backingLocal, slot: idx, styp: styp})
		return true
	case instr.LOCAL_TEE:
		return len(*state) > 0
	case instr.UPVAL_GET:
		idx := int(inst.Operand(0))
		if idx >= len(fn.Captures) {
			return false
		}
		styp, _ := fn.Captures[idx].(*types.StructType)
		push(slot{kind: fn.Captures[idx].Kind(), backing: backingUpval, slot: idx, styp: styp})
		return true
	case instr.GLOBAL_GET:
		idx := int(inst.Operand(0))
		if idx >= len(globals) {
			return false
		}
		push(slot{kind: globals[idx], backing: backingGlobal, slot: idx})
		return true
	case instr.GLOBAL_TEE:
		return len(*state) > 0
	case instr.CONST_GET:
		idx := int(inst.Operand(0))
		if idx >= len(constants) {
			return false
		}
		value := slot{kind: constants[idx].Kind()}
		if value.kind == types.KindRef {
			value.backing = backingConst
			value.ref, value.refKnown = constants[idx].Ref(), true
			if value.ref > 0 && value.ref < len(heap) {
				if _, ok := heap[value.ref].(*types.Function); ok {
					value.callee, value.calleeKnown = value.ref, true
				}
			}
		}
		push(value)
		return true
	case instr.DUP:
		if len(*state) == 0 {
			return false
		}
		push((*state)[len(*state)-1])
		return true
	case instr.SWAP:
		if len(*state) < 2 {
			return false
		}
		n := len(*state)
		(*state)[n-1], (*state)[n-2] = (*state)[n-2], (*state)[n-1]
		return true
	case instr.SELECT:
		if len(*state) < 3 {
			return false
		}
		n := len(*state)
		a, b := (*state)[n-2], (*state)[n-3]
		if a.kind != b.kind {
			return false
		}
		*state = (*state)[:n-3]
		push(slot{kind: a.kind})
		return true
	case instr.I32_CONST:
		push(slot{kind: types.KindI32, val: int32(inst.Operand(0)), valKnown: true})
		return true
	case instr.ARRAY_GET:
		if len(*state) < 2 {
			return false
		}
		array := (*state)[len(*state)-2]
		kind, ok := arrayKind(heap, array)
		if !ok || !pop(2) {
			return false
		}
		push(slot{kind: kind})
		return true
	case instr.STRUCT_GET:
		if len(*state) < 2 {
			return false
		}
		kind, ok := structFieldKind(heap, (*state)[len(*state)-2], (*state)[len(*state)-1])
		if !ok || !pop(2) {
			return false
		}
		push(slot{kind: kind})
		return true
	case instr.CALL, instr.RETURN_CALL:
		if len(*state) == 0 {
			return false
		}
		callee := (*state)[len(*state)-1]
		if !callee.calleeKnown || callee.callee <= 0 || callee.callee >= len(heap) {
			return false
		}
		target, ok := heap[callee.callee].(*types.Function)
		if !ok || target.Typ == nil || !pop(1+len(target.Typ.Params)) {
			return false
		}
		if inst.Opcode() == instr.CALL {
			for _, typ := range target.Typ.Returns {
				push(slot{kind: typ.Kind()})
			}
		}
		return true
	case instr.RETURN:
		returns := 0
		if fn.Typ != nil {
			returns = len(fn.Typ.Returns)
		}
		return len(*state) >= returns
	case instr.STRUCT_NEW, instr.MAP_NEW, instr.CLOSURE_NEW:
		return false
	}
	typ := inst.Type()
	if typ.Pop == nil && typ.Push == nil || !pop(len(typ.Pop)) {
		return false
	}
	for _, kind := range typ.Push {
		if kind == instr.KindAny {
			return false
		}
		push(slot{kind: types.Kind(kind)})
	}
	return true
}

func arrayKind(heap []types.Value, array slot) (types.Kind, bool) {
	if !array.refKnown || array.ref <= 0 || array.ref >= len(heap) {
		return 0, false
	}
	switch heap[array.ref].(type) {
	case types.TypedArray[bool]:
		return types.KindI1, true
	case types.TypedArray[int8]:
		return types.KindI8, true
	case types.TypedArray[int32]:
		return types.KindI32, true
	case types.TypedArray[int64]:
		return types.KindI64, true
	case types.TypedArray[float32]:
		return types.KindF32, true
	case types.TypedArray[float64]:
		return types.KindF64, true
	default:
		return 0, false
	}
}

// structFieldKind resolves a STRUCT_GET result kind statically: the container
// must carry a declared struct type (or reference a known heap struct) and the
// field index must be a known in-bounds constant.
func structFieldKind(heap []types.Value, container, index slot) (types.Kind, bool) {
	typ := container.styp
	if typ == nil && container.refKnown && container.ref > 0 && container.ref < len(heap) {
		if s, ok := heap[container.ref].(*types.Struct); ok {
			typ = s.Typ
		}
	}
	if typ == nil || !index.valKnown || index.val < 0 || int(index.val) >= len(typ.Fields) {
		return 0, false
	}
	return typ.Fields[index.val].Kind, true
}

// zeroBoxed builds the zero boxed value whose Kind() reports exactly kind; the
// static frontend uses it to synthesize step.seen for lowerers that read the
// observed result kind.
func zeroBoxed(kind types.Kind) (types.Boxed, bool) {
	switch kind {
	case types.KindI1:
		return types.BoxI1(false), true
	case types.KindI8:
		return types.BoxI8(0), true
	case types.KindI32:
		return types.BoxI32(0), true
	case types.KindI64:
		return types.BoxI64(0), true
	case types.KindF32:
		return types.BoxF32(0), true
	case types.KindF64:
		return types.BoxF64(0), true
	case types.KindRef:
		return types.BoxedNull, true
	default:
		return 0, false
	}
}

func args(inst instr.Instruction) [2]uint64 {
	var args [2]uint64
	for idx, width := range inst.Type().Widths {
		if idx >= len(args) || width < 0 {
			break
		}
		args[idx] = inst.Operand(idx)
	}
	return args
}

func localTypes(fn *types.Function) []types.Type {
	var result []types.Type
	if fn.Typ != nil {
		result = append(result, fn.Typ.Params...)
	}
	return append(result, fn.Locals...)
}
