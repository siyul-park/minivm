package jit

import (
	"sort"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// StaticPlan builds the whole-function entry plan plus one plan per loop
// header, from bytecode alone: it covers opcodes no trace can record but
// resolves a container's element or field kind only when a constant or
// declared type answers it statically. It returns (nil, nil) when the
// function cannot be planned this way, which is not an error: the caller
// falls back to the trace frontend.
func StaticPlan(input *Input) ([]Plan, error) {
	if input == nil || input.Function == nil || len(input.Function.Code) == 0 {
		return nil, nil
	}
	// A declared array type may only answer arrayKind in a call-free plan.
	// The general array path combined with a native call still corrupts
	// native state (see docs/jit-internals.md); until that is root-caused,
	// a calling function keeps resolving element kinds only from a constant
	// container, exactly as before declared types were consulted.
	declared := callFree(input.Function.Code)
	if input.Address == 0 && !declared {
		return nil, nil
	}

	blocks, err := analysis.Blocks(input.Function)
	if err != nil {
		return nil, err
	}
	constants, heap := input.Constants, input.Heap
	facts, ok := planStates(input.Function, blocks, constants, input.Globals, heap, input.Decl, declared)
	if !ok {
		return nil, nil
	}

	entryType := EntryFunction
	if input.Address == 0 {
		entryType = EntryModule
	}
	result := Plan{Anchor: Anchor{Addr: input.Address}, Kind: entryType}
	result.Blocks = make([]Block, 0, len(blocks))
	locals := localTypes(input.Function)
	for idx, source := range blocks {
		target := Block{Anchor: Anchor{Addr: input.Address, IP: source.Start}}
		target.State = append([]Slot{}, facts[idx]...)
		flow := append([]Slot(nil), facts[idx]...)
		for ip := source.Start; ip < source.End; {
			inst := instr.Instruction(input.Function.Code[ip:])
			next := ip + inst.Width()
			step := Step{Op: inst.Opcode(), Args: Args(inst), Fn: input.Address, IP: ip}
			if instr.IsCall(inst.Opcode()) && len(flow) > 0 {
				callee := flow[len(flow)-1]
				if callee.calleeKnown {
					step.Callee = callee.callee
					step.Known = true
				}
			}
			if inst.Opcode() == instr.CONST_GET {
				constant := int(inst.Operand(0))
				if constant < len(constants) && constants[constant].Kind() == types.KindRef {
					step.Known = true
				}
			}
			// Static steps carry no recorded observation, but structGet and
			// arrayGet read op.Seen.Kind() for the result kind; synthesize the
			// zero boxed value of the statically resolved kind. Runtime itab,
			// type, bounds, and per-field kind guards keep the lowering sound
			// regardless - a slot that currently holds null or a differently
			// shaped container deopts before the access.
			//
			// A constant container is the one case arrayGet does not need this
			// for: arrayGetKnown reads the shape straight out of the constant
			// heap value. Every other container resolves through its declared
			// array type (see arrayKind), and without a synthesized seen the
			// general path would lower against the zero kind and read the
			// element at the wrong width.
			if len(flow) >= 2 {
				switch inst.Opcode() {
				case instr.STRUCT_GET:
					if kind, ok := structFieldKind(heap, flow[len(flow)-2], flow[len(flow)-1]); ok {
						step.Seen = types.Zero(kind)
					}
				case instr.ARRAY_GET:
					if kind, ok := arrayKind(heap, flow[len(flow)-2], declared); ok {
						step.Seen = types.Zero(kind)
					}
				}
			}
			switch inst.Opcode() {
			case instr.BR:
				target.Term = Terminator{Kind: TerminateBranch, IP: ip, Hot: -1, Edges: jumps(input.Address, instr.Targets(input.Function.Code, ip))}
			case instr.BR_IF:
				target.Term = Terminator{Kind: TerminateBranchIf, IP: ip, Hot: -1, Edges: jumps(input.Address, append(instr.Targets(input.Function.Code, ip), next))}
			case instr.BR_TABLE:
				target.Term = Terminator{Kind: TerminateBranchTable, IP: ip, Hot: -1, Edges: jumps(input.Address, instr.Targets(input.Function.Code, ip))}
			case instr.RETURN:
				target.Term = Terminator{Kind: TerminateReturn, IP: ip}
			default:
				if bridgeable(inst.Opcode()) {
					// The backend cannot lower this opcode, so it ends the block as
					// a bridge: interp runs its own threaded closure once and
					// re-enters natively at next (see TerminateBridge). The
					// remaining source instructions continue into a fresh block
					// anchored at next, carrying the post-op dataflow state so
					// lowering reloads it exactly like any other state-backed
					// block.
					target.Term = Terminator{Kind: TerminateBridge, IP: ip}
				} else {
					target.Steps = append(target.Steps, step)
				}
			}
			if !applyStep(input.Function, locals, constants, input.Globals, heap, input.Decl, declared, &flow, inst) {
				return nil, nil
			}
			if bridgeable(inst.Opcode()) {
				result.Blocks = append(result.Blocks, target)
				target = Block{Anchor: Anchor{Addr: input.Address, IP: next}, Bridge: true}
				target.State = append([]Slot{}, flow...)
			}
			ip = next
		}
		if target.Term.Kind == TerminateFallthrough {
			if source.End == len(input.Function.Code) {
				if input.Address == 0 {
					target.Term = Terminator{Kind: TerminateComplete, IP: source.End}
				} else {
					target.Term = Terminator{Kind: TerminateReturn, IP: source.End}
				}
			} else {
				target.Term = Terminator{Kind: TerminateBranch, IP: source.End, Hot: -1, Edges: []Edge{jump(input.Address, source.End)}}
			}
		}
		result.Blocks = append(result.Blocks, target)
	}
	roots := make(map[Anchor]int, len(result.Blocks))
	for id, block := range result.Blocks {
		roots[block.Anchor] = id
	}
	wire(&result, roots)
	result.Carried = carried(input.Function, result.Blocks)
	// The entry plan owns the function's ABI and reaches every block. Each loop
	// header additionally gets a plan that re-enters the live frame there, so a
	// loop that becomes hot before its function does no longer needs a recorded
	// trace to get native code. That plan carries only the blocks its own root
	// can reach: a backend emits every block a plan holds, so sharing the whole
	// -function list would re-emit the entire function once per header.
	loops := headers(result.Blocks)
	plans := make([]Plan, 0, 1+len(loops))
	if !input.Installed {
		plans = append(plans, result)
	}
	for _, id := range loops {
		header, ok := result.prune(id)
		if !ok {
			continue
		}
		header.Kind = EntryLoop
		// Recomputed, not inherited: a block the header cannot reach is never
		// emitted, so its bridges must not strip its loop-carried registers.
		header.Carried = carried(input.Function, header.Blocks)
		plans = append(plans, header)
	}
	return plans, nil
}

// prune returns the plan rooted at root: the blocks reachable from it,
// renumbered densely, anchored where root is. It reports false when the block
// list does not satisfy the bridge-successor layout described below, in which
// case the caller must skip this root rather than emit a plan missing a
// resume target.
//
// It has one caller. The reachability walk and the renumbering it forces are
// a closed mechanic with its own failure mode, and inlining them would leave
// StaticPlan mixing block-graph bookkeeping with the planning it exists to
// do.
func (p Plan) prune(root int) (Plan, bool) {
	if root < 0 || root >= len(p.Blocks) {
		return Plan{}, false
	}
	keep := make([]bool, len(p.Blocks))
	order := []int{root}
	keep[root] = true
	visit := func(next int) {
		if next < 0 || next >= len(p.Blocks) || keep[next] {
			return
		}
		keep[next] = true
		order = append(order, next)
	}
	for n := 0; n < len(order); n++ {
		source := p.Blocks[order[n]]
		for _, edge := range source.Term.Edges {
			visit(edge.Index)
			for _, tail := range edge.Tail {
				visit(tail)
			}
		}
		// No edge names a bridge resume block: resumption is a fresh external
		// entry, not a branch (see TerminateBridge). The planner appends it
		// immediately after the block that bridges into it.
		if source.Term.Kind == TerminateBridge {
			resume := order[n] + 1
			if resume >= len(p.Blocks) || !p.Blocks[resume].Bridge {
				return Plan{}, false
			}
			visit(resume)
		}
	}

	ids := make([]int, len(p.Blocks))
	out := Plan{Anchor: p.Blocks[root].Anchor, Hoist: p.Hoist}
	out.Blocks = make([]Block, 0, len(order))
	for id, live := range keep {
		if !live {
			ids[id] = NoBlock
			continue
		}
		ids[id] = len(out.Blocks)
		out.Blocks = append(out.Blocks, p.Blocks[id])
	}
	out.Root = ids[root]
	for id := range out.Blocks {
		// The edges are the source plan's until copied, and every root shares
		// one block list, so rewriting them in place would corrupt its peers.
		edges := append([]Edge(nil), out.Blocks[id].Term.Edges...)
		for n, e := range edges {
			if e.Index != NoBlock {
				edges[n].Index = ids[e.Index]
			}
			if len(e.Tail) > 0 {
				tail := make([]int, len(e.Tail))
				for k, t := range e.Tail {
					tail[k] = ids[t]
				}
				edges[n].Tail = tail
			}
		}
		out.Blocks[id].Term.Edges = edges
	}
	return out, true
}

// headers returns the block IDs a backward edge targets: this function's loop
// headers, where a hot loop re-enters. A header at IP zero is excluded
// because the entry plan already owns that anchor.
func headers(blocks []Block) []int {
	var out []int
	seen := map[int]bool{}
	for _, source := range blocks {
		for _, edge := range source.Term.Edges {
			if edge.Index == NoBlock || edge.Anchor.IP <= 0 || edge.Anchor.IP >= source.Anchor.IP || seen[edge.Index] {
				continue
			}
			seen[edge.Index] = true
			out = append(out, edge.Index)
		}
	}
	sort.Ints(out)
	return out
}

// bridgeable reports whether op is an opcode the ARM64 backend cannot lower
// natively but the threaded closure can still perform exactly once: the
// static planner ends its block on op instead of rejecting the whole
// function (see TerminateBridge), and a backend's bridge deopts to run op's
// own threaded closure before resuming native execution at the following
// block. An opcode already lowered natively (for example ARRAY_GET or
// STRUCT_SET) MUST NOT appear here: a bridge is strictly the fallback for
// opcodes with no native lowering. YIELD and RESUME are excluded even though
// the backend cannot lower them either: suspension cannot resume mid-frame
// into native code (see docs/jit-internals.md, Suspension), so they keep
// their unconditional terminal-fallback treatment instead of becoming a
// bridge.
func bridgeable(op instr.Opcode) bool {
	switch op {
	case instr.ARRAY_NEW, instr.ARRAY_NEW_DEFAULT, instr.ARRAY_SLICE, instr.ARRAY_DELETE,
		instr.STRUCT_NEW, instr.STRUCT_NEW_DEFAULT,
		instr.MAP_NEW, instr.MAP_NEW_DEFAULT, instr.MAP_DELETE, instr.MAP_CLEAR,
		instr.REF_NEW, instr.REF_SET, instr.CLOSURE_NEW, instr.STRING_NEW_UTF32,
		instr.STRING_ENCODE_UTF32, instr.STRING_ITER,
		instr.MAP_LEN, instr.MAP_GET, instr.MAP_LOOKUP, instr.MAP_KEYS, instr.MAP_ITER,
		instr.ARRAY_FILL, instr.ARRAY_COPY, instr.ARRAY_APPEND, instr.MAP_SET,
		instr.ERROR_NEW, instr.ERROR_CODE, instr.THROW,
		instr.REF_TEST, instr.REF_CAST:
		return true
	default:
		return false
	}
}

func planStates(fn *types.Function, blocks []*analysis.BasicBlock, constants []types.Boxed, globals []types.Kind, heap []types.Value, decl []types.Type, declared bool) ([][]Slot, bool) {
	if len(fn.Handlers) > 0 {
		return nil, false
	}
	if len(blocks) == 0 {
		return nil, true
	}
	locals := localTypes(fn)
	states := make([][]Slot, len(blocks))
	seen := make([]bool, len(blocks))
	seen[0] = true
	work := []int{0}
	for len(work) > 0 {
		idx := work[len(work)-1]
		work = work[:len(work)-1]
		state := append([]Slot(nil), states[idx]...)
		if !applyBlock(fn, locals, constants, globals, heap, decl, declared, blocks[idx], &state) {
			return nil, false
		}
		for _, succ := range blocks[idx].Succs {
			if !seen[succ] {
				seen[succ] = true
				states[succ] = append([]Slot(nil), state...)
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

func mergeSlot(dst *Slot, src Slot) (bool, bool) {
	if dst.Kind != src.Kind {
		return false, false
	}
	changed := false
	if dst.Backing != src.Backing || dst.Offset != src.Offset {
		dst.Backing, dst.Offset = BackingStack, 0
		changed = true
	}
	if dst.refKnown && (!src.refKnown || dst.Ref != src.Ref) {
		dst.Ref, dst.refKnown = 0, false
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

func applyBlock(fn *types.Function, locals []types.Type, constants []types.Boxed, globals []types.Kind, heap []types.Value, decl []types.Type, declared bool, block *analysis.BasicBlock, state *[]Slot) bool {
	for ip := block.Start; ip < block.End; {
		inst := instr.Instruction(fn.Code[ip:])
		if !applyStep(fn, locals, constants, globals, heap, decl, declared, state, inst) {
			return false
		}
		ip += inst.Width()
	}
	return true
}

func applyStep(fn *types.Function, locals []types.Type, constants []types.Boxed, globals []types.Kind, heap []types.Value, decl []types.Type, declared bool, state *[]Slot, inst instr.Instruction) bool {
	push := func(value Slot) { *state = append(*state, value) }
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
		atyp, _ := locals[idx].(*types.ArrayType)
		push(Slot{Kind: locals[idx].Kind(), Backing: BackingLocal, Offset: idx, styp: styp, atyp: atyp})
		return true
	case instr.LOCAL_TEE:
		return len(*state) > 0
	case instr.UPVAL_GET:
		idx := int(inst.Operand(0))
		if idx >= len(fn.Captures) {
			return false
		}
		styp, _ := fn.Captures[idx].(*types.StructType)
		atyp, _ := fn.Captures[idx].(*types.ArrayType)
		push(Slot{Kind: fn.Captures[idx].Kind(), Backing: BackingUpval, Offset: idx, styp: styp, atyp: atyp})
		return true
	case instr.GLOBAL_GET:
		idx := int(inst.Operand(0))
		if idx >= len(globals) {
			return false
		}
		push(Slot{Kind: globals[idx], Backing: BackingGlobal, Offset: idx})
		return true
	case instr.GLOBAL_TEE:
		return len(*state) > 0
	case instr.CONST_GET:
		idx := int(inst.Operand(0))
		if idx >= len(constants) {
			return false
		}
		value := Slot{Kind: constants[idx].Kind()}
		if value.Kind == types.KindRef {
			value.Backing = BackingConst
			value.Ref, value.refKnown = constants[idx].Ref(), true
			if value.Ref > 0 && value.Ref < len(heap) {
				if _, ok := heap[value.Ref].(*types.Function); ok {
					value.callee, value.calleeKnown = value.Ref, true
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
		if a.Kind != b.Kind {
			return false
		}
		*state = (*state)[:n-3]
		push(Slot{Kind: a.Kind})
		return true
	case instr.I32_CONST:
		push(Slot{Kind: types.KindI32, val: int32(inst.Operand(0)), valKnown: true})
		return true
	case instr.ARRAY_GET:
		if len(*state) < 2 {
			return false
		}
		array := (*state)[len(*state)-2]
		kind, ok := arrayKind(heap, array, declared)
		if !ok || !pop(2) {
			return false
		}
		push(Slot{Kind: kind})
		return true
	case instr.STRUCT_GET:
		if len(*state) < 2 {
			return false
		}
		kind, ok := structFieldKind(heap, (*state)[len(*state)-2], (*state)[len(*state)-1])
		if !ok || !pop(2) {
			return false
		}
		push(Slot{Kind: kind})
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
				push(Slot{Kind: typ.Kind()})
			}
		}
		return true
	case instr.RETURN:
		returns := 0
		if fn.Typ != nil {
			returns = len(fn.Typ.Returns)
		}
		return len(*state) >= returns
	case instr.REF_CAST:
		// A successful cast validates the operand's declared type in place
		// and leaves the same boxed value on the stack; its kind never
		// changes. REF_CAST is bridged (see bridgeable), so the resume
		// block's operand is a fresh retain taken by retainDeferred, not the
		// pre-cast operand's backing slot; push a new BackingStack slot
		// instead of mutating the existing one in place, or the resume block
		// would elide a release that must run and leak the retain. Narrow a
		// struct-typed target so a later STRUCT_GET can resolve its field
		// kind statically, the same way a declared struct-typed local does.
		if len(*state) == 0 {
			return false
		}
		top := (*state)[len(*state)-1]
		cast := Slot{Kind: top.Kind}
		if idx := int(inst.Operand(0)); idx < len(decl) {
			if styp, ok := decl[idx].(*types.StructType); ok {
				cast.styp = styp
			}
			if atyp, ok := decl[idx].(*types.ArrayType); ok {
				cast.atyp = atyp
			}
		}
		(*state)[len(*state)-1] = cast
		return true
	case instr.ARRAY_DELETE:
		// The removed element's kind is the array's element kind, resolved
		// the same way ARRAY_GET resolves it.
		if len(*state) < 2 {
			return false
		}
		kind, ok := arrayKind(heap, (*state)[len(*state)-2], declared)
		if !ok || !pop(2) {
			return false
		}
		push(Slot{Kind: kind})
		return true
	case instr.ARRAY_NEW:
		// ARRAY_NEW's element count is a runtime i32 on top of the elements,
		// not the fixed two-operand shape instr.TypeOf approximates for the
		// verifier; the effect is only knowable when that count is a known
		// constant.
		if len(*state) == 0 {
			return false
		}
		count := (*state)[len(*state)-1]
		if count.Kind != types.KindI32 || !count.valKnown || count.val < 0 {
			return false
		}
		if !pop(1 + int(count.val)) {
			return false
		}
		push(Slot{Kind: types.KindRef})
		return true
	case instr.STRUCT_NEW:
		// STRUCT_NEW's arity is not on the operand stack: it pops exactly one
		// value per field of its declared struct type.
		idx := int(inst.Operand(0))
		if idx >= len(decl) {
			return false
		}
		styp, ok := decl[idx].(*types.StructType)
		if !ok || !pop(len(styp.Fields)) {
			return false
		}
		push(Slot{Kind: types.KindRef, styp: styp})
		return true
	case instr.MAP_NEW:
		// MAP_NEW's entry count is a runtime i32 pushed ahead of it, not a
		// bytecode operand; the effect is only knowable when that count is a
		// known constant.
		if len(*state) == 0 {
			return false
		}
		count := (*state)[len(*state)-1]
		if count.Kind != types.KindI32 || !count.valKnown || count.val < 0 {
			return false
		}
		if !pop(1 + int(count.val)*2) {
			return false
		}
		push(Slot{Kind: types.KindRef})
		return true
	case instr.CLOSURE_NEW:
		// CLOSURE_NEW pops its captures plus the function reference below
		// them; the capture count is only knowable when that reference
		// resolves to a known heap function, exactly like the
		// CONST_GET+CALL fusion a backend performs.
		if len(*state) == 0 {
			return false
		}
		callee := (*state)[len(*state)-1]
		if !callee.refKnown || callee.Ref <= 0 || callee.Ref >= len(heap) {
			return false
		}
		target, ok := heap[callee.Ref].(*types.Function)
		if !ok || !pop(1+len(target.Captures)) {
			return false
		}
		push(Slot{Kind: types.KindRef})
		return true
	case instr.ARRAY_APPEND:
		// ARRAY_APPEND's value count is a runtime i32 on top of the values;
		// the array reference below the values is never popped, so it stays
		// on the stack afterward. ARRAY_APPEND is bridged (see bridgeable),
		// so that surviving operand is a fresh retain taken by
		// retainDeferred after the bridge, not the pre-append operand's
		// backing slot (see the same note on REF_CAST above): replace it
		// with a fresh BackingStack slot instead of leaving the existing
		// one, or a later consumer that elides its release for a deferred
		// backing would leak the retain.
		if len(*state) == 0 {
			return false
		}
		count := (*state)[len(*state)-1]
		if count.Kind != types.KindI32 || !count.valKnown || count.val < 0 {
			return false
		}
		if !pop(1 + int(count.val)) {
			return false
		}
		if len(*state) == 0 {
			return false
		}
		(*state)[len(*state)-1] = Slot{Kind: types.KindRef}
		return true
	}
	typ := inst.Type()
	if typ.Pop == nil && typ.Push == nil || !pop(len(typ.Pop)) {
		return false
	}
	for _, kind := range typ.Push {
		if kind == instr.KindAny {
			return false
		}
		push(Slot{Kind: types.Kind(kind)})
	}
	return true
}

// arrayKind resolves an array's element kind. A container whose identity is
// known resolves from the live heap cell; otherwise the declared array type
// answers, exactly as a declared struct type answers structFieldKind. Both
// are hints: the lowering's runtime tag and itab guards verify the shape
// before any access, so a slot declared as an array that currently holds
// null or a differently shaped array deopts instead of reading it.
func arrayKind(heap []types.Value, array Slot, declared bool) (types.Kind, bool) {
	if !array.refKnown || array.Ref <= 0 || array.Ref >= len(heap) {
		if declared && array.atyp != nil && array.atyp.ElemKind != instr.KindAny {
			return array.atyp.ElemKind, true
		}
		return 0, false
	}
	switch heap[array.Ref].(type) {
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
// must carry a declared struct type (or reference a known heap struct) and
// the field index must be a known in-bounds constant.
func structFieldKind(heap []types.Value, container, index Slot) (types.Kind, bool) {
	typ := container.styp
	if typ == nil && container.refKnown && container.Ref > 0 && container.Ref < len(heap) {
		if s, ok := heap[container.Ref].(*types.Struct); ok {
			typ = s.Typ
		}
	}
	if typ == nil || !index.valKnown || index.val < 0 || int(index.val) >= len(typ.Fields) {
		return 0, false
	}
	return typ.Fields[index.val].Kind, true
}

func localTypes(fn *types.Function) []types.Type {
	var result []types.Type
	if fn.Typ != nil {
		result = append(result, fn.Typ.Params...)
	}
	return append(result, fn.Locals...)
}

// callFree reports whether code contains no call. A call-free plan is the
// only place a declared array type may stand in for an observed container
// shape.
func callFree(code []byte) bool {
	for ip := 0; ip < len(code); {
		inst := instr.Instruction(code[ip:])
		if instr.IsCall(inst.Opcode()) {
			return false
		}
		ip += inst.Width()
	}
	return true
}
