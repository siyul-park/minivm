package asm

// useIndex maps a vreg id to every instruction position that reads it,
// ascending. It backs crosses' dominance check: whether a spill's store
// dominates every read still pending after it. A definition needs no entry
// of its own here — it already kills whatever value came before it, so a
// pending read is the only kind of reference a reload must reach.
type useIndex [][]int32

// buildUseIndex scans insts once, independent of any control-flow graph,
// and records every instruction position that reads each vreg.
func buildUseIndex(insts []Instruction, count int) useIndex {
	idx := make(useIndex, count)
	for i, inst := range insts {
		uses, n := inst.uses()
		for _, v := range uses[:n] {
			id := v.ID()
			idx[id] = append(idx[id], int32(i))
		}
	}
	return idx
}

// futureUses returns id's use positions strictly after i, ascending.
func (idx useIndex) futureUses(id int32, i int) []int32 {
	positions := idx[id]
	lo := searchAfter(positions, i)
	return positions[lo:]
}

// searchAfter returns the index of the first position strictly greater
// than i in the ascending slice positions.
func searchAfter(positions []int32, i int) int {
	lo, hi := 0, len(positions)
	for lo < hi {
		mid := (lo + hi) / 2
		if positions[mid] <= int32(i) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}
