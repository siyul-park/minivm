package graph

// Dominance answers "does node a dominate node b" queries: does every path
// from the graph's entry node (node 0) to b pass through a.
type Dominance struct {
	idom []int
}

// NewDominance computes the immediate dominator of every node reachable
// from node 0 using the iterative algorithm of Cooper, Harvey, and Kennedy
// (2001). A node unreachable from the entry keeps idom -1 and Dominates
// treats it as dominating nothing, not even itself.
func NewDominance(g Graph) *Dominance {
	rpoNum, order := reversePostorder(g)
	idom := make([]int, g.Len())
	for i := range idom {
		idom[i] = -1
	}
	if len(order) == 0 {
		return &Dominance{idom: idom}
	}
	idom[order[0]] = order[0]

	for changed := true; changed; {
		changed = false
		for _, b := range order[1:] {
			pick := -1
			for _, p := range g.Pred(b) {
				if idom[p] == -1 {
					continue
				}
				if pick == -1 {
					pick = p
					continue
				}
				pick = intersect(idom, rpoNum, pick, p)
			}
			if pick != -1 && idom[b] != pick {
				idom[b] = pick
				changed = true
			}
		}
	}
	return &Dominance{idom: idom}
}

// Dominates reports whether node a dominates node b in the graph this
// Dominance was computed over.
func (d *Dominance) Dominates(a, b int) bool {
	if d.idom[b] == -1 {
		return false
	}
	for b != a {
		if b == 0 {
			return false
		}
		b = d.idom[b]
	}
	return true
}

// IDom returns node's immediate dominator: the unique closest node that
// strictly dominates it. It returns -1 for the entry, which has none, and for
// a node unreachable from the entry.
func (d *Dominance) IDom(node int) int {
	if node <= 0 || node >= len(d.idom) || d.idom[node] == -1 {
		return -1
	}
	return d.idom[node]
}

// Frontier returns the dominance frontier of every node: frontier[a] holds
// each node b that a dominates a predecessor of without strictly dominating b
// itself, which is exactly where a definition in a stops being the only one
// reaching. It is the Cooper, Harvey, and Kennedy formulation - from every
// predecessor of a join, walk the dominator tree up to that join's immediate
// dominator, adding the join on the way - so it costs one walk per edge into a
// join and nothing at all for the rest of the graph.
//
// Only a predecessor the entry reaches contributes: a node the entry does not
// reach has no immediate dominator to walk toward and no definition that ever
// reaches the join, exactly as Dominates leaves it out.
func Frontier(g Graph, d *Dominance) [][]int {
	frontier := make([][]int, g.Len())
	for b := range g.Len() {
		idom := d.IDom(b)
		preds := g.Pred(b)
		if idom < 0 || len(preds) < 2 {
			continue
		}
		for _, p := range preds {
			if !d.Dominates(0, p) {
				continue
			}
			for at := p; at != idom && at >= 0; at = d.IDom(at) {
				if len(frontier[at]) == 0 || frontier[at][len(frontier[at])-1] != b {
					frontier[at] = append(frontier[at], b)
				}
			}
		}
	}
	return frontier
}

// intersect finds the nearest common ancestor of a and b in the dominator
// tree being built, walking each toward the root by reverse-postorder
// number until they meet.
func intersect(idom, rpoNum []int, a, b int) int {
	for a != b {
		for rpoNum[a] > rpoNum[b] {
			a = idom[a]
		}
		for rpoNum[b] > rpoNum[a] {
			b = idom[b]
		}
	}
	return a
}

// reversePostorder walks g depth-first from node 0 and returns each
// reachable node's reverse-postorder number (unreachable nodes get -1)
// together with the reachable nodes listed in that order, root first.
func reversePostorder(g Graph) (rpoNum []int, order []int) {
	n := g.Len()
	rpoNum = make([]int, n)
	for i := range rpoNum {
		rpoNum[i] = -1
	}
	if n == 0 {
		return rpoNum, nil
	}

	visited := make([]bool, n)
	post := make([]int, 0, n)
	stack := []struct {
		node int
		next int
	}{{0, 0}}
	visited[0] = true
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		succ := g.Succ(top.node)
		if top.next < len(succ) {
			s := succ[top.next]
			top.next++
			if !visited[s] {
				visited[s] = true
				stack = append(stack, struct {
					node int
					next int
				}{s, 0})
			}
			continue
		}
		post = append(post, top.node)
		stack = stack[:len(stack)-1]
	}

	order = make([]int, len(post))
	for i, b := range post {
		pos := len(post) - 1 - i
		order[pos] = b
		rpoNum[b] = pos
	}
	return rpoNum, order
}
