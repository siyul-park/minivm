package jit

import (
	"sort"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// TracePlan builds one plan per anchor input.Traces has a published tree for:
// a recorded loop specializes its body to the path actually taken (folded
// legs, a hoisted container), which StaticPlan's whole-function view cannot
// express. It returns (nil, nil) when input carries no recorder or no
// function, which is not an error: the caller falls back to the static
// frontend.
func TracePlan(input *Input) ([]Plan, error) {
	if input == nil || input.Traces == nil || input.Function == nil {
		return nil, nil
	}
	var plans []Plan
	for _, ip := range input.Traces.Anchors(input.Address) {
		a := Anchor{Addr: input.Address, IP: ip}
		tree := input.Traces.RootAt(a)
		if tree == nil || tree.Root == nil {
			continue
		}
		// A loop anchor accepts a looping root or a returned straight-line
		// fragment: a body whose terminal boundary (bulk mutation, yield,
		// throw) deopts before the back-edge still compiles as a per-entry
		// prefix that re-enters at the header next iteration. Carried entry
		// operands stay rejected either way.
		switch tree.Root.Status {
		case StatusFallback, StatusCompleted, StatusPartial:
			if ip != 0 {
				continue
			}
		case StatusReturned:
			if ip != 0 && tree.Root.Carried {
				continue
			}
		case StatusLoop:
			if ip == 0 || tree.Root.Carried {
				continue
			}
		case StatusAborted:
			continue
		default:
			continue
		}
		kind := EntryFunction
		if input.Address == 0 {
			kind = EntryModule
		}
		if ip != 0 {
			kind = EntryLoop
		}
		planned := Plan{Anchor: a, Kind: kind, Root: -1}
		root := store(&planned, split(&planned, tree.Root, input), false)
		if len(root) == 0 {
			continue
		}
		planned.Root = root[0]
		roots := map[Anchor]int{tree.Root.Anchor: root[0]}

		type leg struct {
			trace *Trace
			hits  int64
		}
		var legs []leg
		for id, tr := range tree.Branches {
			if tr == nil {
				continue
			}
			// A loop-kind leg is a loop root of its own: anchored at this
			// header it is the root trace itself (capture returns the
			// existing root, and its edge already wires to the root block);
			// anchored elsewhere it is a different loop, and splitting it
			// here would inline that whole loop body instead of using its
			// native entry.
			switch tr.Status {
			case StatusAborted, StatusLoop:
				continue
			case StatusFallback, StatusReturned, StatusCompleted, StatusPartial:
			default:
				continue
			}
			hits := int64(0)
			if id >= 0 && id < len(tree.Hits) {
				hits = tree.Hits[id]
			}
			legs = append(legs, leg{trace: tr, hits: hits})
		}
		sort.SliceStable(legs, func(i, j int) bool {
			if legs[i].hits != legs[j].hits {
				return legs[i].hits > legs[j].hits
			}
			if legs[i].trace.Anchor.Addr != legs[j].trace.Anchor.Addr {
				return legs[i].trace.Anchor.Addr < legs[j].trace.Anchor.Addr
			}
			return legs[i].trace.Anchor.IP < legs[j].trace.Anchor.IP
		})
		for _, leg := range legs {
			ids := store(&planned, split(&planned, leg.trace, input), false)
			if len(ids) > 0 {
				roots[leg.trace.Anchor] = ids[0]
			}
		}
		wire(&planned, roots)
		planned.Carried = carried(input.Function, planned.Blocks)
		if kind == EntryLoop {
			planned.Hoist = hoistable(input.Function, planned.Blocks)
		}
		planned.NoSpill = noSpill(planned.Blocks)
		plans = append(plans, planned)
	}
	return plans, nil
}

// hoistable picks the most-accessed loop-invariant container for a loop plan.
// A local qualifies when it is a declared ref, no block writes it, and every
// recorded ARRAY_GET/ARRAY_SET on it observed one itab. Any call disqualifies
// the plan: a BL clobbers the hoisted registers and re-enters via the
// journal's back-edge continuation. Container provenance is a per-block
// marker stack. Variable-effect stack operators update markers explicitly;
// fixed-effect operators use instr.Type, and anything else clears the
// markers conservatively. Underflow also clears them — loop plans with
// carried entry operands are rejected before planning.
func hoistable(fn *types.Function, blocks []Block) *Hoist {
	locals := localTypes(fn)
	banned := make([]bool, len(locals))
	for _, block := range blocks {
		for _, step := range block.Steps {
			if instr.IsCall(step.Op) {
				return nil
			}
			if instr.WritesLocal(step.Op) {
				if local := int(step.Args[0]); local < len(banned) {
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
		for _, step := range block.Steps {
			record := func(depth int) {
				if depth > len(stack) || step.Op == instr.ARRAY_SET && step.Terminal {
					return
				}
				// Only the primitive typed arrays hoist: a ref array's
				// elements carry ownership, which a cached slice header
				// cannot account for.
				if shape, ok := ElemShapeByItab(step.Shape.Itab); !ok || !shape.Raw {
					return
				}
				local := stack[len(stack)-depth]
				if local < 0 || local >= len(locals) || local > MaxHoistSlot || banned[local] {
					return
				}
				candidate := &candidates[local]
				if candidate.want != 0 && candidate.want != step.Shape.Itab {
					candidate.conflict = true
					return
				}
				candidate.want = step.Shape.Itab
				candidate.hits++
			}

			switch step.Op {
			case instr.LOCAL_GET:
				stack = append(stack, int(step.Args[0]))
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

			typ := instr.TypeOf(step.Op)
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
	return &Hoist{Local: best, Want: candidates[best].want}
}

func split(p *Plan, tr *Trace, input *Input) []Block {
	if tr == nil {
		return nil
	}
	current := Block{Anchor: tr.Anchor}
	var blocks []Block
	rejoins := func(op Record) bool {
		return tr.Status == StatusPartial && p.Kind == EntryLoop && op.Cut && op.Depth == 0 &&
			op.Fn == p.Anchor.Addr && op.Target == p.Anchor.IP
	}
	for idx, op := range tr.Ops {
		if op.Cut {
			// A leg cut at the loop plan's own header is the loop back-edge:
			// wire a real branch so wire() folds it onto the root block and
			// the lowering takes the committing-flush native back-edge
			// instead of a deopt round trip. Cuts inside an inlined frame
			// keep the fallback — the root block expects the anchor frame
			// only.
			if rejoins(op) {
				current.Term = Terminator{Kind: TerminateBranch, IP: op.Target, Hot: 0, Edges: []Edge{jump(op.Fn, op.Target)}}
			} else {
				current.Term = Terminator{Kind: TerminateFallback, IP: op.Target, Hot: -1}
			}
			blocks = append(blocks, current)
			return blocks
		}
		path := -1
		switch op.Op {
		case instr.BR:
			current.Term = Terminator{Kind: TerminateBranch, IP: op.IP, Hot: 0, Edges: []Edge{jump(op.Fn, op.Target)}}
			path = 0
			blocks = append(blocks, current)
		case instr.BR_IF:
			next := op.IP + 3
			hot := 1
			if op.Taken {
				hot = 0
			}
			edges := []Edge{jump(op.Fn, op.Target), jump(op.Fn, next)}
			edges[1-hot].Tail = suffix(p, tr, idx, input)
			current.Term = Terminator{Kind: TerminateBranchIf, IP: op.IP, Hot: hot, Edges: edges}
			path = hot
			blocks = append(blocks, current)
		case instr.BR_TABLE:
			var targets []int
			if fn := Resolve(input.Module, input.Heap, op.Fn); fn != nil {
				targets = instr.Targets(fn.Code, op.IP)
			}
			hot := -1
			for n, target := range targets {
				if target == op.Target {
					hot = n
					break
				}
			}
			edges := jumps(op.Fn, targets)
			tail := suffix(p, tr, idx, input)
			for i := range edges {
				if i != hot {
					edges[i].Tail = tail
				}
			}
			current.Term = Terminator{Kind: TerminateBranchTable, IP: op.IP, Hot: hot, Edges: edges}
			path = hot
			blocks = append(blocks, current)
		case instr.RETURN:
			if op.Depth == 0 {
				current.Term = Terminator{Kind: TerminateReturn, IP: op.IP, Hot: -1}
				blocks = append(blocks, current)
				return blocks
			}
			current.Steps = append(current.Steps, op.Step)
			continue
		default:
			current.Steps = append(current.Steps, op.Step)
			continue
		}
		if idx+1 >= len(tr.Ops) {
			return blocks
		}
		next := tr.Ops[idx+1]
		if path >= 0 {
			// A cut straight after the branch carries no new ops: the trace
			// took the branch and stopped. End the split with the hot edge
			// unresolved so wire() can fold it onto a known root block (the
			// loop back-edge) instead of chaining a spurious empty block.
			hot := &blocks[len(blocks)-1].Term.Edges[path]
			if rejoins(next) && hot.Anchor == p.Anchor {
				return blocks
			}
			hot.Index = local(len(blocks))
		}
		current = Block{Anchor: Anchor{Addr: next.Fn, IP: next.IP}}
	}
	if len(blocks) > 0 && len(current.Steps) == 0 && current.Term.Kind == TerminateFallthrough {
		return blocks
	}
	switch tr.Status {
	case StatusFallback:
		current.Term = Terminator{Kind: TerminateFallback, IP: tr.Anchor.IP, Hot: -1}
	case StatusReturned:
		current.Term = Terminator{Kind: TerminateFallthrough, Hot: -1}
	case StatusCompleted:
		current.Term = Terminator{Kind: TerminateComplete, Hot: -1}
	case StatusPartial:
		resume := tr.Anchor.IP
		if len(tr.Ops) > 0 {
			resume = tr.Ops[len(tr.Ops)-1].Target
		}
		current.Term = Terminator{Kind: TerminateFallback, IP: resume, Hot: -1}
	case StatusLoop:
		current.Term = Terminator{Kind: TerminateBranch, IP: tr.Anchor.IP, Hot: 0, Edges: []Edge{{Anchor: tr.Anchor, Index: local(0)}}}
	case StatusAborted:
		return nil
	default:
		return nil
	}
	blocks = append(blocks, current)
	return blocks
}

// suffix plans the continuation a conditional branch falls through to: the
// rest of the trace from the first op that leaves the branch's frame depth.
//
// Results are memoized on the first op of the suffix. Every branch in a trace
// asks for a suffix, and nested branches ask for suffixes of each other's
// suffixes, so planning each one afresh costs time exponential in the number
// of branches - a recorded call tree with a hundred of them never finishes.
// The op pointer is the right key because all suffixes of one trace index
// into its own ops array, and a suffix strictly advances, so the memo is
// never asked for an entry still being built.
func suffix(p *Plan, tr *Trace, idx int, input *Input) []int {
	depth := tr.Ops[idx].Depth
	for at := idx + 1; at < len(tr.Ops); at++ {
		if tr.Ops[at].Depth >= depth {
			continue
		}
		key := &tr.Ops[at]
		if ids, ok := p.tails[key]; ok {
			return ids
		}
		tail := &Trace{
			Anchor: Anchor{Addr: tr.Ops[at].Fn, IP: tr.Ops[at].IP},
			Ops:    tr.Ops[at:],
			Status: tr.Status,
		}
		ids := store(p, split(p, tail, input), true)
		if p.tails == nil {
			p.tails = map[*Record][]int{}
		}
		p.tails[key] = ids
		return ids
	}
	return nil
}

// local encodes a plan-local block placeholder id as a negative Edge.Index
// (distinct from NoBlock), resolved to a real Plan.Blocks index by store.
func local(id int) int {
	return -id - 2
}

// localID decodes a placeholder id encoded by local, reporting false for
// NoBlock or a real, already-resolved block index.
func localID(block int) (int, bool) {
	if block >= NoBlock {
		return 0, false
	}
	return -block - 2, true
}

func store(p *Plan, blocks []Block, tail bool) []int {
	start := len(p.Blocks)
	ids := make([]int, len(blocks))
	for i, block := range blocks {
		block.Tail = tail
		ids[i] = start + i
		p.Blocks = append(p.Blocks, block)
	}
	for _, id := range ids {
		for i := range p.Blocks[id].Term.Edges {
			edge := &p.Blocks[id].Term.Edges[i]
			local, ok := localID(edge.Index)
			if !ok {
				continue
			}
			if local < 0 || local >= len(ids) {
				edge.Index = NoBlock
				continue
			}
			edge.Index = ids[local]
		}
	}
	return ids
}
