package asm

// dominance answers "does instruction a dominate instruction b" queries by
// combining block-level dominance (via idom, computed once) with intra-block
// position order: two instructions in the same block are ordered by index
// alone, since a block has one entry and one exit.
type dominance struct {
	cfg  *cfg
	idom []int
}

// newDominance computes the immediate dominator of every block reachable
// from block 0 using the iterative algorithm of Cooper, Harvey, and Kennedy
// (2001). A block unreachable from the entry keeps idom -1 and dominates
// nothing but itself.
func newDominance(g *cfg) *dominance {
	rpoNum, order := reversePostorder(g)
	idom := make([]int, len(g.blocks))
	for i := range idom {
		idom[i] = -1
	}
	if len(order) == 0 {
		return &dominance{cfg: g, idom: idom}
	}
	idom[order[0]] = order[0]

	for changed := true; changed; {
		changed = false
		for _, b := range order[1:] {
			pick := -1
			for _, p := range g.blocks[b].pred {
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
	return &dominance{cfg: g, idom: idom}
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

// reversePostorder walks the graph depth-first from block 0 and returns each
// reachable block's reverse-postorder number (unreachable blocks get -1)
// together with the reachable blocks listed in that order, root first.
func reversePostorder(g *cfg) (rpoNum []int, order []int) {
	n := len(g.blocks)
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
		block int
		next  int
	}{{0, 0}}
	visited[0] = true
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.next < len(g.blocks[top.block].succ) {
			s := g.blocks[top.block].succ[top.next]
			top.next++
			if !visited[s] {
				visited[s] = true
				stack = append(stack, struct {
					block int
					next  int
				}{s, 0})
			}
			continue
		}
		post = append(post, top.block)
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

// dominates reports whether instruction a dominates instruction b: every
// control-flow path from the entry to b passes through a.
func (d *dominance) dominates(a, b int) bool {
	ba, bb := d.cfg.of[a], d.cfg.of[b]
	if ba == bb {
		return a <= b
	}
	return d.blockDominates(ba, bb)
}

// blockDominates reports whether block a dominates block b in the graph
// this dominance was computed over.
func (d *dominance) blockDominates(a, b int) bool {
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
