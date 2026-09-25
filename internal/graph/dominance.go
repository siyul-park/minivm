package graph

// Dominance answers "does node a dominate node b" queries: does every path
// from the graph's entry node (node 0) to b pass through a.
type Dominance struct {
	idom []int
}

// Frontier returns each node's dominance frontier using the
// Cooper-Harvey-Kennedy join-predecessor walk. Unreachable predecessors contribute
// nothing because they have no dominator chain.
func Frontier(g Graph, d *Dominance) [][]int {
	frontier := make([][]int, g.Len())
	for b := range g.Len() {
		idom := d.IDom(b)
		preds := g.Pred(b)
		if b != 0 && (idom < 0 || len(preds) < 2) {
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

// NewDominance computes the immediate dominator of every node reachable
// from node 0 using the iterative algorithm of Cooper, Harvey, and Kennedy
// (2001). A node unreachable from the entry keeps idom -1 and Dominates
// treats it as dominating nothing, not even itself.
func NewDominance(g Graph) *Dominance {
	order := Order(g)
	rpoNum := make([]int, g.Len())
	for i, node := range order {
		rpoNum[node] = i
	}
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

// Children returns the dominator-tree children for every node.
func (d *Dominance) Children() [][]int {
	children := make([][]int, len(d.idom))
	for node := 1; node < len(d.idom); node++ {
		parent := d.IDom(node)
		if parent >= 0 {
			children[parent] = append(children[parent], node)
		}
	}
	return children
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
