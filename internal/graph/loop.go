package graph

import "sort"

// Loop is one natural loop.
type Loop struct {
	Header int
	// Body holds the loop's nodes, Header included.
	Body map[int]bool
	// Preheader is the unique outside predecessor whose only successor is
	// Header, or -1 when the loop has none.
	Preheader int
}

// Loops returns the natural loops of g, innermost first. A node is a loop
// header exactly when some back-edge b->s has s dominating b.
func Loops(g Graph, d *Dominance) []Loop {
	seen := make([]bool, g.Len())
	var loops []Loop
	for b := range g.Len() {
		for _, s := range g.Succ(b) {
			if seen[s] || !d.Dominates(s, b) {
				continue
			}
			seen[s] = true
			body := body(g, d, s)
			loops = append(loops, Loop{Header: s, Body: body, Preheader: preheader(g, body, s)})
		}
	}
	sort.Slice(loops, func(i, j int) bool {
		return len(loops[i].Body) < len(loops[j].Body)
	})
	return loops
}

func body(g Graph, d *Dominance, header int) map[int]bool {
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

func preheader(g Graph, body map[int]bool, header int) int {
	found := -1
	for _, pred := range g.Pred(header) {
		if body[pred] {
			continue
		}
		if found >= 0 {
			return -1
		}
		found = pred
	}
	if found < 0 {
		return -1
	}
	if succ := g.Succ(found); len(succ) != 1 || succ[0] != header {
		return -1
	}
	return found
}
