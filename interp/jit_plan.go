package interp

import (
	"sort"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
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
	op    instr.Opcode
	args  [2]uint64
	seen  types.Boxed
	arg   types.Boxed
	shape shape

	fn     int
	ip     int
	depth  int
	callee int
	ref    int
	known  bool
}

type planner interface {
	plan(*compileInput) ([]plan, error)
}

type compileInput struct {
	tracer    *Tracer
	address   int
	function  *types.Function
	constants []types.Boxed
	globals   []types.Kind
	heap      []types.Value
	functions map[int]*types.Function
	installed bool
}

type plan struct {
	entry  entry
	blocks []block
	spill  spillPolicy
}

type entry struct {
	anchor anchor
	kind   entryKind
}

type entryKind uint8

const (
	entryInvalid entryKind = iota
	entryFunction
	entryLoop
	entryModule
)

type block struct {
	anchor anchor
	hits   int64
	state  *state
	steps  []step
	term   terminator
}

type state struct {
	slots []slot
}

type slot struct {
	kind        types.Kind
	ref         int
	refKnown    bool
	callee      int
	calleeKnown bool
}

type terminator struct {
	kind    terminatorKind
	ip      int
	hot     int
	targets []int
	tail    []block
}

type terminatorKind uint8

const (
	terminateFallthrough terminatorKind = iota
	terminateBranch
	terminateBranchIf
	terminateBranchTable
	terminateReturn
	terminateComplete
	terminateFallback
)

type spillPolicy uint8

const (
	spillAllowed spillPolicy = iota
	spillForbidden
)

func newCompileInput(i *Interpreter, addr int) (*compileInput, bool) {
	fn, ok := i.function(addr)
	if !ok || fn == nil || len(fn.Code) == 0 {
		return nil, false
	}
	globals := make([]types.Kind, len(i.globals))
	for idx, global := range i.globals {
		globals[idx] = global.Kind()
	}
	functions := make(map[int]*types.Function)
	for fnAddr := range i.instrs {
		if target, ok := i.function(fnAddr); ok {
			functions[fnAddr] = target
		}
	}
	return &compileInput{
		tracer:    i.tracer,
		address:   addr,
		function:  fn,
		constants: i.constants,
		globals:   globals,
		heap:      i.heap,
		functions: functions,
		installed: i.stub(addr) != nil,
	}, true
}

func (p plan) valid() bool {
	if len(p.blocks) == 0 {
		return false
	}
	switch p.entry.kind {
	case entryFunction:
		if p.entry.anchor.addr <= 0 || p.entry.anchor.ip != 0 {
			return false
		}
	case entryLoop:
		if p.entry.anchor.addr <= 0 || p.entry.anchor.ip <= 0 {
			return false
		}
	case entryModule:
		if p.entry.anchor.addr != 0 || p.entry.anchor.ip != 0 {
			return false
		}
	default:
		return false
	}
	seen := make(map[anchor]struct{}, len(p.blocks))
	for _, block := range p.blocks {
		if _, ok := seen[block.anchor]; ok {
			return false
		}
		seen[block.anchor] = struct{}{}
	}
	work := append([]block(nil), p.blocks...)
	for len(work) > 0 {
		block := work[len(work)-1]
		work = work[:len(work)-1]
		switch block.term.kind {
		case terminateFallthrough, terminateReturn, terminateComplete, terminateFallback:
			if len(block.term.targets) != 0 {
				return false
			}
		case terminateBranch:
			if len(block.term.targets) != 1 {
				return false
			}
		case terminateBranchIf:
			if len(block.term.targets) != 2 {
				return false
			}
		case terminateBranchTable:
			if len(block.term.targets) == 0 {
				return false
			}
		default:
			return false
		}
		work = append(work, block.term.tail...)
	}
	if _, ok := seen[p.entry.anchor]; !ok {
		return false
	}
	return true
}

type staticPlanner struct{}

func (staticPlanner) plan(input *compileInput) ([]plan, error) {
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

	manager := pass.NewManager()
	pass.Register[*types.Function, []*analysis.BasicBlock](manager, analysis.NewBasicBlocksAnalysis())
	blocks, err := pass.GetResult[[]*analysis.BasicBlock](manager, input.function)
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
	result := plan{entry: entry{anchor: anchor{addr: input.address}, kind: entryType}}
	result.blocks = make([]block, len(blocks))
	locals := localTypes(input.function)
	for idx, source := range blocks {
		target := block{anchor: anchor{addr: input.address, ip: source.Start}, state: &state{}}
		target.state.slots = append([]slot(nil), facts[idx]...)
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
					step.ref = constants[constant].Ref()
					step.known = true
				}
			}
			switch inst.Opcode() {
			case instr.BR:
				target.term = terminator{kind: terminateBranch, ip: ip, hot: -1, targets: instr.Targets(input.function.Code, ip)}
			case instr.BR_IF:
				target.term = terminator{kind: terminateBranchIf, ip: ip, hot: -1, targets: append(instr.Targets(input.function.Code, ip), next)}
			case instr.BR_TABLE:
				target.term = terminator{kind: terminateBranchTable, ip: ip, hot: -1, targets: instr.Targets(input.function.Code, ip)}
			case instr.RETURN:
				target.term = terminator{kind: terminateReturn, ip: ip}
			default:
				target.steps = append(target.steps, step)
			}
			if !applyPlanStep(input.function, locals, constants, input.globals, heap, &flow, inst) {
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
				target.term = terminator{kind: terminateBranch, ip: source.End, hot: -1, targets: []int{source.End}}
			}
		}
		result.blocks[idx] = target
	}
	if !result.valid() {
		return nil, nil
	}
	return []plan{result}, nil
}

type tracePlanner struct{}

func (tracePlanner) plan(input *compileInput) ([]plan, error) {
	if input == nil || input.tracer == nil || input.function == nil {
		return nil, nil
	}
	var plans []plan
	for _, ip := range input.tracer.anchors(input.address) {
		a := anchor{addr: input.address, ip: ip}
		tree := input.tracer.rootAt(a)
		if tree == nil || tree.root == nil || tree.root.kind == aborted {
			continue
		}
		if (ip != 0) != (tree.root.kind == loop) {
			continue
		}
		kind := entryFunction
		if input.address == 0 {
			kind = entryModule
		}
		if ip != 0 {
			kind = entryLoop
		}
		planned := plan{entry: entry{anchor: a, kind: kind}}
		planned.blocks = append(planned.blocks, split(tree.root, 0, input.functions)...)

		type leg struct {
			trace *trace
			hits  int64
		}
		var legs []leg
		for id, tr := range tree.branches {
			if tr == nil || tr.kind == loop || tr.kind == aborted {
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
		seen := make(map[anchor]bool, len(planned.blocks))
		for _, block := range planned.blocks {
			seen[block.anchor] = true
		}
		for _, leg := range legs {
			for _, block := range split(leg.trace, leg.hits, input.functions) {
				if seen[block.anchor] {
					continue
				}
				seen[block.anchor] = true
				planned.blocks = append(planned.blocks, block)
			}
		}
		planned.spill = planSpill(planned.blocks)
		if planned.valid() {
			plans = append(plans, planned)
		}
	}
	return plans, nil
}

func split(tr *trace, hits int64, functions map[int]*types.Function) []block {
	if tr == nil {
		return nil
	}
	current := block{anchor: tr.anchor, hits: hits}
	var blocks []block
	commit := func() {
		blocks = append(blocks, current)
	}
	for idx, op := range tr.ops {
		if op.cut {
			current.term = terminator{kind: terminateFallback, ip: op.target, hot: -1}
			commit()
			return blocks
		}
		switch op.op {
		case instr.BR:
			current.term = terminator{kind: terminateBranch, ip: op.ip, hot: 0, targets: []int{op.target}}
			commit()
		case instr.BR_IF:
			next := op.ip + 3
			hot := 1
			if op.taken {
				hot = 0
			}
			current.term = terminator{kind: terminateBranchIf, ip: op.ip, hot: hot, targets: []int{op.target, next}, tail: suffix(tr, idx, functions)}
			commit()
		case instr.BR_TABLE:
			var targets []int
			if fn := functions[op.fn]; fn != nil {
				targets = instr.Targets(fn.Code, op.ip)
			}
			hot := -1
			for n, target := range targets {
				if target == op.target {
					hot = n
					break
				}
			}
			current.term = terminator{kind: terminateBranchTable, ip: op.ip, hot: hot, targets: targets, tail: suffix(tr, idx, functions)}
			commit()
		case instr.RETURN:
			if op.depth == 0 {
				current.term = terminator{kind: terminateReturn, ip: op.ip, hot: -1}
				commit()
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
		current = block{anchor: anchor{addr: next.fn, ip: next.ip}}
	}
	if len(blocks) > 0 && len(current.steps) == 0 && current.term.kind == terminateFallthrough {
		return blocks
	}
	switch tr.kind {
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
		current.term = terminator{kind: terminateBranch, ip: tr.anchor.ip, hot: 0, targets: []int{tr.anchor.ip}}
	default:
		current.term = terminator{kind: terminateFallback, ip: tr.anchor.ip, hot: -1}
	}
	commit()
	return blocks
}

func suffix(tr *trace, idx int, functions map[int]*types.Function) []block {
	depth := tr.ops[idx].depth
	for at := idx + 1; at < len(tr.ops); at++ {
		if tr.ops[at].depth >= depth {
			continue
		}
		tail := &trace{
			anchor: anchor{addr: tr.ops[at].fn, ip: tr.ops[at].ip},
			ops:    append([]record(nil), tr.ops[at:]...),
			kind:   tr.kind,
		}
		return split(tail, 0, functions)
	}
	return nil
}

func planSpill(blocks []block) spillPolicy {
	for _, block := range blocks {
		if len(block.steps) > 0 {
			switch block.steps[len(block.steps)-1].op {
			case instr.ARRAY_SET, instr.STRUCT_SET:
				return spillForbidden
			}
		}
		if planSpill(block.term.tail) == spillForbidden {
			return spillForbidden
		}
	}
	return spillAllowed
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
		if !applyPlanBlock(fn, locals, constants, globals, heap, blocks[idx], &state) {
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
	if dst.refKnown && (!src.refKnown || dst.ref != src.ref) {
		dst.ref, dst.refKnown = 0, false
		changed = true
	}
	if dst.calleeKnown && (!src.calleeKnown || dst.callee != src.callee) {
		dst.callee, dst.calleeKnown = 0, false
		changed = true
	}
	return changed, true
}

func applyPlanBlock(fn *types.Function, locals []types.Type, constants []types.Boxed, globals []types.Kind, heap []types.Value, block *analysis.BasicBlock, state *[]slot) bool {
	for ip := block.Start; ip < block.End; {
		inst := instr.Instruction(fn.Code[ip:])
		if !applyPlanStep(fn, locals, constants, globals, heap, state, inst) {
			return false
		}
		ip += inst.Width()
	}
	return true
}

func applyPlanStep(fn *types.Function, locals []types.Type, constants []types.Boxed, globals []types.Kind, heap []types.Value, state *[]slot, inst instr.Instruction) bool {
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
		push(slot{kind: locals[idx].Kind()})
		return true
	case instr.LOCAL_TEE:
		return len(*state) > 0
	case instr.UPVAL_GET:
		idx := int(inst.Operand(0))
		if idx >= len(fn.Captures) {
			return false
		}
		push(slot{kind: fn.Captures[idx].Kind()})
		return true
	case instr.GLOBAL_GET:
		idx := int(inst.Operand(0))
		if idx >= len(globals) {
			return false
		}
		push(slot{kind: globals[idx]})
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
