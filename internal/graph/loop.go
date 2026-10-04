package graph

// Headers returns the node index of every node some back-edge
// targets: b->s is a back-edge, and s a loop header, exactly when s
// dominates b — the standard definition of a natural loop.
func Headers(g Graph, d *Dominance) []int {
	seen := make([]bool, g.Len())
	var out []int
	for b := range g.Len() {
		for _, s := range g.Succ(b) {
			if !seen[s] && d.Dominates(s, b) {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}

// Body returns the nodes of the natural loop rooted at header.
func Body(g Graph, d *Dominance, header int) map[int]bool {
	body := map[int]bool{header: true}
	var stack []int
	for _, pred := range g.Pred(header) {
		if d.Dominates(header, pred) && !body[pred] {
			body[pred] = true
			stack = append(stack, pred)
		}
	}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, pred := range g.Pred(node) {
			if !body[pred] {
				body[pred] = true
				stack = append(stack, pred)
			}
		}
	}
	return body
}

// Preheader returns the unique outside predecessor whose only successor is header.
func Preheader(g Graph, body map[int]bool, header int) (int, bool) {
	found := -1
	for _, pred := range g.Pred(header) {
		if body[pred] {
			continue
		}
		if found >= 0 {
			return -1, false
		}
		found = pred
	}
	if found < 0 {
		return -1, false
	}
	succ := g.Succ(found)
	if len(succ) != 1 || succ[0] != header {
		return -1, false
	}
	return found, true
}
