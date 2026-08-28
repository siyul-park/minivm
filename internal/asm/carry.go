package asm

// hazard names one self-referencing redefinition governed by a loop: an
// instruction that both reads and writes the same vreg, in a block some
// loop header dominates. header is that loop header's block index; at is
// the redefinition's instruction index.
type hazard struct {
	header int
	at     int
}

// carryHazards indexes, per vreg id, every self-referencing redefinition a
// loop governs — the shape a loop-carried mutable value takes in this
// flat, non-SSA IR: one vreg, defined once ahead of the loop and again by
// an instruction that also reads it (an accumulating ADD, say), so the
// same static instruction computes a new value from the previous one on
// every iteration.
//
// crosses treats each such redefinition as a barrier a spilled value's
// store must itself sit inside the same loop to cross safely. Dominance
// alone cannot see the hazard: a store placed before the loop dominates
// the redefinition's every static reference under the ordinary definition
// (every path to it passes through the store), because dominance asks
// nothing about how many times a loop body re-executes. But a spill
// inserts exactly one store and one reload instruction, not one pair per
// iteration, so a reload sitting at a redefinition that runs again next
// iteration would read back the value the store captured once, not the
// previous iteration's update — silently reusing stale data. A value
// confined to one pass, by contrast, is undamaged: nothing about it
// depends on a redefinition surviving to a later iteration.
func carryHazards(insts []Instruction, g *cfg, dom *dominance, count int) [][]hazard {
	out := make([][]hazard, count)
	headers := loopHeaders(g, dom)
	if len(headers) == 0 {
		return out
	}
	for i, inst := range insts {
		dst, ok := inst.def()
		if !ok {
			continue
		}
		uses, n := inst.uses()
		self := false
		for _, v := range uses[:n] {
			if v.ID() == dst.ID() {
				self = true
				break
			}
		}
		if !self {
			continue
		}
		id := dst.ID()
		for _, h := range headers {
			if dom.blockDominates(h, g.of[i]) {
				out[id] = append(out[id], hazard{header: h, at: i})
			}
		}
	}
	return out
}

// loopHeaders returns the block index of every block some back-edge
// targets: b->s is a back-edge, and s a loop header, exactly when s
// dominates b — the standard definition of a natural loop.
func loopHeaders(g *cfg, dom *dominance) []int {
	seen := make(map[int]bool)
	var out []int
	for b := range g.blocks {
		for _, s := range g.blocks[b].succ {
			if !seen[s] && dom.blockDominates(s, b) {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}
