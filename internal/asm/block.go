package asm

import "sort"

// block is a maximal straight-line run of instructions: entered only at
// start and exited only from the instruction immediately before end, so any
// position inside a block dominates every later position in the same block
// without consulting the dominator tree.
type block struct {
	start, end int // half-open instruction range [start, end)
	succ       []int
	pred       []int
}

// cfg is the control-flow graph the allocator consults to decide which
// store point safely dominates which reload. Block 0 is always the entry:
// buildCFG makes instruction 0 a leader whenever insts is non-empty.
type cfg struct {
	blocks []block
	of     []int // of[i] names the block index containing instruction i
}

// buildCFG partitions insts into blocks using bound labels as boundaries and
// frame's Returns/Calls/Jumps predicates to classify each block's exit, then
// wires successor/predecessor edges.
//
// A call (frame.Calls) is deliberately not treated as a branch here: it
// always falls through to its own resume point once the callee returns, and
// its label operand names a routine entry rather than a point in this
// block's own control flow. inject applies the same view when it decides
// where to emit Resume. Whether a call's target is bound at or before the
// call site — a self-recursive call sharing the caller's spill frame — is a
// question for barriers, not this graph; see docs/jit-internals.md.
func buildCFG(insts []Instruction, labels map[Label]int, frame Frame) *cfg {
	n := len(insts)
	if n == 0 {
		return &cfg{blocks: []block{{}}}
	}

	starts := leaders(insts, labels, n)
	blocks := make([]block, len(starts))
	of := make([]int, n)
	for bi, start := range starts {
		end := n
		if bi+1 < len(starts) {
			end = starts[bi+1]
		}
		blocks[bi] = block{start: start, end: end}
		for i := start; i < end; i++ {
			of[i] = bi
		}
	}

	locate := func(pos int) int {
		return sort.Search(len(starts), func(i int) bool { return starts[i] > pos }) - 1
	}
	for bi := range blocks {
		wireExits(blocks, bi, insts, labels, locate, frame)
	}

	pred := make([][]int, len(blocks))
	for bi := range blocks {
		for _, s := range blocks[bi].succ {
			pred[s] = append(pred[s], bi)
		}
	}
	for bi := range blocks {
		blocks[bi].pred = pred[bi]
	}

	return &cfg{blocks: blocks, of: of}
}

// leaders collects every instruction index that starts a block: the first
// instruction, every bound label position, and the instruction following
// any branch or return.
func leaders(insts []Instruction, labels map[Label]int, n int) []int {
	set := map[int]bool{0: true}
	for _, pos := range labels {
		if pos >= 0 && pos < n {
			set[pos] = true
		}
	}
	for i := range insts {
		if i+1 < n && endsBlock(insts[i]) {
			set[i+1] = true
		}
	}
	out := make([]int, 0, len(set))
	for pos := range set {
		out = append(out, pos)
	}
	sort.Ints(out)
	return out
}

// endsBlock reports whether inst may end a block: it targets a label, so a
// later pass can decide whether it also falls through once frame is known.
// A conservative caller without frame in scope over-splits rather than
// missing a real boundary; wireExits alone decides fall-through.
func endsBlock(inst Instruction) bool {
	_, ok := inst.Src2.(LabelOperand)
	return ok
}

// wireExits appends bi's successor edges by inspecting its last instruction.
// A call falls through unconditionally and never contributes a branch edge:
// see buildCFG's doc comment.
func wireExits(blocks []block, bi int, insts []Instruction, labels map[Label]int, locate func(int) int, frame Frame) {
	b := &blocks[bi]
	fallthroughEdge := func() {
		if b.end < len(insts) {
			addSucc(b, locate(b.end))
		}
	}
	if b.start == b.end {
		fallthroughEdge()
		return
	}

	last := insts[b.end-1]
	if frame.Returns(last.Op) {
		return
	}
	lbl, isLabel := last.Src2.(LabelOperand)
	if !isLabel || frame.Calls(last.Op) {
		fallthroughEdge()
		return
	}
	if target, bound := labels[lbl.ID]; bound {
		addSucc(b, locate(target))
	}
	if !frame.Jumps(last.Op) {
		fallthroughEdge()
	}
}

func addSucc(b *block, target int) {
	for _, s := range b.succ {
		if s == target {
			return
		}
	}
	b.succ = append(b.succ, target)
}
