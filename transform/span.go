package transform

import (
	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
)

type span struct {
	start   int
	end     int
	flow    []int
	succs   []int
	suspend bool
}

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
		spans[last[i]].succs = successors(code, block, at)
		inst, ip := final(code, block)
		tail := ip >= 0 && inst.Opcode() == instr.RETURN_CALL
		for _, succ := range spans[last[i]].succs {
			if succ == exit || tail {
				spans[last[i]].flow = append(spans[last[i]].flow, succ)
			}
		}
	}
	return spans, at
}

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

// successors lists the spans block's last instruction can continue into. A
// RETURN_CALL continues into the function's first span: the self tail call
// the walker lowers as a loop re-enters it.
func successors(code []byte, block *analysis.BasicBlock, at map[int]int) []int {
	if inst, last := final(code, block); last >= 0 {
		switch inst.Opcode() {
		case instr.RETURN:
			return nil
		case instr.RETURN_CALL:
			return targets([]int{0}, at)
		case instr.BR, instr.BR_TABLE:
			return targets(instr.Targets(code, last), at)
		case instr.BR_IF:
			return targets(append(instr.Targets(code, last), last+inst.Width()), at)
		}
	}
	if block.End >= len(code) {
		return nil
	}
	return targets([]int{block.End}, at)
}

// final is block's last instruction and its offset, -1 for an empty block.
func final(code []byte, block *analysis.BasicBlock) (instr.Instruction, int) {
	var inst instr.Instruction
	last := -1
	for ip := block.Start; ip < block.End; {
		inst, last = instr.Instruction(code[ip:]), ip
		ip += inst.Width()
	}
	return inst, last
}

func targets(offsets []int, at map[int]int) []int {
	out := make([]int, 0, len(offsets))
	for _, ip := range offsets {
		if id, ok := at[ip]; ok {
			out = append(out, id)
		}
	}
	return out
}
