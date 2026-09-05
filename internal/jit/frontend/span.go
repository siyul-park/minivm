package frontend

import (
	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/jit"
)

// span is one straight-line run of bytecode that becomes one SSA block. A
// basic block is one span, or one more for every opcode it bridges: the
// interpreter re-enters natively at the instruction after a bridged opcode, and
// a point a backend can be entered at is a block of its own.
//
// Control and dataflow are not the same successors. An opcode that leaves the
// function - a throw, a tail call, an unreachable - is followed by the bytecode
// after it, which control reaches only from somewhere else; flow carries the
// operand facts along the edges execution really takes, succs names the blocks
// the terminator wires.
type span struct {
	start int
	end   int
	flow  []int
	succs []int
}

// split cuts every basic block into its spans and wires both successor sets.
// Spans stay in bytecode order, so a bridged opcode's span is followed by the
// span it resumes into.
func split(code []byte, blocks []*analysis.BasicBlock) []span {
	var spans []span
	first := make([]int, len(blocks))
	last := make([]int, len(blocks))
	for i, block := range blocks {
		first[i] = len(spans)
		start := block.Start
		for ip := block.Start; ip < block.End; {
			inst := instr.Instruction(code[ip:])
			ip += inst.Width()
			if jit.Bridgeable(inst.Opcode()) {
				spans = append(spans, span{start: start, end: ip})
				start = ip
			}
		}
		last[i] = len(spans)
		spans = append(spans, span{start: start, end: block.End})
	}
	// A block ending on a bridge leaves an empty span at its end, which shares
	// its start with the next block's first span. The later one wins, exactly
	// as the plan's anchor map resolves the same collision, and the empty span
	// stays reachable through the bridge that precedes it.
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
			spans[id].succs = []int{id + 1}
		}
		for _, succ := range block.Succs {
			spans[last[i]].flow = append(spans[last[i]].flow, first[succ])
		}
		spans[last[i]].succs = leaves(code, block, at)
		// The virtual exit is no basic block of its own, so nothing hands it the
		// operands it is entered with unless the branches reaching it do.
		for _, succ := range spans[last[i]].succs {
			if succ == exit {
				spans[last[i]].flow = append(spans[last[i]].flow, exit)
			}
		}
	}
	return spans
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
// that follows it. A block whose last span is empty ends on a bridge, and
// falls through like any other.
func leaves(code []byte, block *analysis.BasicBlock, at map[int]int) []int {
	ip, inst, ok := tail(code, block)
	if ok {
		switch inst.Opcode() {
		case instr.RETURN:
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

// enter returns the span a root anchors at. A zero IP is the function or
// module entry, which an already installed entry does not rebuild; every other
// IP must name a loop header, because those are the only points a live frame
// re-enters native code at.
func enter(spans []span, ip int, installed bool) (int, bool) {
	if ip == 0 {
		return 0, !installed && spans[0].start == 0
	}
	for id, s := range spans {
		if s.start == ip && header(spans, id) {
			return id, true
		}
	}
	return 0, false
}

// header reports whether a backward edge targets a span, which makes it one of
// this function's loop headers. A span at IP zero is never one, because the
// entry root already owns that anchor.
func header(spans []span, id int) bool {
	if spans[id].start <= 0 {
		return false
	}
	for _, s := range spans {
		if spans[id].start >= s.start {
			continue
		}
		for _, succ := range s.succs {
			if succ == id {
				return true
			}
		}
	}
	return false
}

// reach returns the spans control enters from root, root first. A backend
// emits every block a function holds, so a header keeps only what it reaches
// rather than re-emitting the whole function once per header.
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
