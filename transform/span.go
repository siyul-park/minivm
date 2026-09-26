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
		for _, succ := range spans[last[i]].succs {
			if succ == exit {
				spans[last[i]].flow = append(spans[last[i]].flow, exit)
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

func successors(code []byte, block *analysis.BasicBlock, at map[int]int) []int {
	last := -1
	for ip := block.Start; ip < block.End; {
		inst := instr.Instruction(code[ip:])
		last = ip
		ip += inst.Width()
	}
	if last >= 0 {
		inst := instr.Instruction(code[last:])
		switch inst.Opcode() {
		case instr.RETURN, instr.RETURN_CALL:
			return nil
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

func targets(offsets []int, at map[int]int) []int {
	out := make([]int, 0, len(offsets))
	for _, ip := range offsets {
		if id, ok := at[ip]; ok {
			out = append(out, id)
		}
	}
	return out
}
