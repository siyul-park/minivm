package asm

import "sort"

// crosses reports whether spilling id at at would be unsound: some
// still-pending use of id is not dominated by the store at at (so some path
// reaches that use without ever running the store), a self-recursive call
// sits between at and id's last use, or a loop-governed self-referencing
// redefinition of id sits between them with the store itself outside that
// loop.
//
// Dominance replaces the old "no label bound in between" approximation: it
// asks the real question (does every path to the reload pass through the
// store) instead of a proxy for it, so a value confined to one loop
// iteration — dead before the back-edge, redefined after it — is now
// spillable, while one redefined only after the branch and read again at the
// top of the next iteration still fails on its own merits: the first pass
// through the loop reaches that read without ever running a store issued
// later. But dominance alone is not sufficient for a value whose
// redefinition reads itself (an accumulating ADD, say — see carry.go): such
// a redefinition's own store dominates its own reload trivially under the
// ordinary definition, since dominance does not count how many times a loop
// body re-executes, yet a spill inserts exactly one store and one reload,
// not one pair per iteration. A reload sitting at that redefinition would
// read back whatever the store captured once, not the previous iteration's
// update, unless the store itself sits inside the same loop and therefore
// refreshes on the same schedule.
//
// A self-recursive call needs its own rule alongside both: the call shares
// the caller's spill frame (Frame.Resume, not a fresh Frame.Enter — see
// docs/jit-internals.md), so a value the caller still needs after such a
// call is unsound to leave spilled across it even though the store trivially
// dominates the reload in the caller's own single-execution control flow.
// Nothing about that hazard is visible to a dominance query either: it is
// about two activations of the same code sharing one physical slot, not
// about which paths reach which instruction.
func (r *rewriter) crosses(at int, id int32) bool {
	s := &r.regs[id]
	if s.last <= at {
		return false
	}
	if crossesBarrier(r.barriers, at, s.last) {
		return true
	}
	for _, h := range r.hazards[id] {
		if h.at > at && h.at <= s.last && !r.dom.blockDominates(h.header, r.dom.cfg.of[at]) {
			return true
		}
	}
	for _, u := range r.uses.futureUses(id, at) {
		if !r.dom.dominates(at, int(u)) {
			return true
		}
	}
	return false
}

// crossesBarrier reports whether a barrier lies strictly between at and
// last, ascending barriers making a binary search sufficient.
func crossesBarrier(barriers []int, at, last int) bool {
	n := sort.SearchInts(barriers, at+1)
	return n < len(barriers) && barriers[n] < last
}

// barriers collects the position of every self-recursive call — a call
// (frame.Calls) whose target label is bound at or before the call itself —
// ascending. See crosses.
func barriers(insts []Instruction, labels map[Label]int, frame Frame) []int {
	var out []int
	for i, inst := range insts {
		if !frame.Calls(inst.Op) {
			continue
		}
		lbl, ok := inst.Src2.(LabelOperand)
		if !ok {
			continue
		}
		if target, bound := labels[lbl.ID]; bound && target <= i {
			out = append(out, i)
		}
	}
	return out
}
