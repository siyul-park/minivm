package graph

// LoopHeaders returns the node index of every node some back-edge
// targets: b->s is a back-edge, and s a loop header, exactly when s
// dominates b — the standard definition of a natural loop.
func LoopHeaders(g Graph, d *Dominance) []int {
	seen := make(map[int]bool)
	var out []int
	for b := 0; b < g.Len(); b++ {
		for _, s := range g.Succ(b) {
			if !seen[s] && d.Dominates(s, b) {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}
