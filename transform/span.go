package transform

import (
	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
)

// span is one straight-line run of bytecode that becomes one SSA block.
// Spans are cut at basic-block boundaries and suspension points. A suspension
// keeps flow for the threaded continuation but emits no native successor.
//
// Control and dataflow are not the same successors. An opcode that leaves the
// function - a throw, an unreachable - is followed by the bytecode after it,
// which control reaches only from somewhere else; flow carries the operand
// facts along the edges execution really takes, succs names the blocks the
// terminator wires.
type span struct {
	start int
	end   int
	flow  []int
	succs []int
	// suspend marks a span ending on a suspension point: its facts still flow
	// to the next span, because threaded execution continues there, but its
	// terminator wires no successor, because native execution does not.
	suspend bool
}

// split cuts every basic block into spans and wires dataflow and control
// successors. Spans stay in bytecode order.
func split(code []byte, blocks []*analysis.BasicBlock) ([]span, map[int]int) {
	var spans []span
	first := make([]int, len(blocks))
	last := make([]int, len(blocks))
	for i, block := range blocks {
		first[i] = len(spans)
		start := block.Start
		for ip := block.Start; ip < block.End; {
			inst := instr.Instruction(code[ip:])
			ip += inst.Width()
			if inst.Opcode() == instr.YIELD || inst.Opcode() == instr.RESUME {
				spans = append(spans, span{start: start, end: ip, suspend: true})
				start = ip
			}
		}
		last[i] = len(spans)
		spans = append(spans, span{start: start, end: block.End})
	}
	at := make(map[int]int, len(spans))
	for i, s := range spans {
		at[s.start] = i
	}
	exit := -1
	if _, ok := at[len(code)]; !ok && past(code) {
		spans = append(spans, span{start: len(code), end: len(code)})
		exit = len(spans) - 1
		at[len(code)] = exit
	}

	for i, block := range blocks {
		for id := first[i]; id < last[i]; id++ {
			spans[id].flow = []int{id + 1}
			if !spans[id].suspend {
				spans[id].succs = []int{id + 1}
			}
		}
		for _, succ := range block.Succs {
			spans[last[i]].flow = append(spans[last[i]].flow, first[succ])
		}
		spans[last[i]].succs = leaves(code, block, at)
		for _, succ := range spans[last[i]].succs {
			if succ == exit {
				spans[last[i]].flow = append(spans[last[i]].flow, exit)
			}
		}
	}
	return spans, at
}

// past reports whether a branch leaves through the offset one past the end of
// the code, the virtual exit analysis.Blocks lays out no block for.
func past(code []byte) bool {
	for ip := 0; ip < len(code); {
		for _, target := range instr.Targets(code, ip) {
			if target == len(code) {
				return true
			}
		}
		ip += instr.Instruction(code[ip:]).Width()
	}
	return false
}

// leaves returns the spans control reaches from a block's last span: its
// branch targets, the instruction after a conditional branch, or the block
// that follows it. A block whose last span is empty falls through like any
// other. A return and a tail call reach nothing: both
// leave the frame the span was translated in, so the bytecode after them is
// entered only from elsewhere.
func leaves(code []byte, block *analysis.BasicBlock, at map[int]int) []int {
	ip, inst, ok := tail(code, block)
	if ok {
		switch inst.Opcode() {
		case instr.RETURN, instr.RETURN_CALL:
			return nil
		case instr.BR, instr.BR_TABLE:
			return targets(instr.Targets(code, ip), at)
		case instr.BR_IF:
			return targets(append(instr.Targets(code, ip), ip+inst.Width()), at)
		}
	}
	if block.End >= len(code) {
		return nil
	}
	return targets([]int{block.End}, at)
}

// tail returns a block's last instruction, and false for an empty block.
func tail(code []byte, block *analysis.BasicBlock) (int, instr.Instruction, bool) {
	last := -1
	for ip := block.Start; ip < block.End; {
		inst := instr.Instruction(code[ip:])
		last = ip
		ip += inst.Width()
	}
	if last < 0 {
		return 0, nil, false
	}
	return last, instr.Instruction(code[last:]), true
}

// targets resolves branch offsets into span ids.
func targets(ips []int, at map[int]int) []int {
	out := make([]int, 0, len(ips))
	for _, ip := range ips {
		if id, ok := at[ip]; ok {
			out = append(out, id)
		}
	}
	return out
}

// reach returns the spans control enters from the entry span, entry first.
func reach(spans []span, root int) []int {
	seen := make([]bool, len(spans))
	seen[root] = true
	order := []int{root}
	for n := 0; n < len(order); n++ {
		for _, succ := range spans[order[n]].succs {
			if !seen[succ] {
				seen[succ] = true
				order = append(order, succ)
			}
		}
	}
	return order
}
