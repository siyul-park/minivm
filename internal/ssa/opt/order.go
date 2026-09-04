package opt

import "github.com/siyul-park/minivm/internal/ssa"

// order returns the blocks of fn reachable from the entry in reverse
// postorder: a block precedes every block it dominates, so a pass that walks
// blocks in this order always resolves a value's own definition - and, for a
// pass that renumbers or substitutes values as it goes, its final
// substitution - before it needs to translate a later use of it. It is the
// one traversal every pass in this package shares, computed directly over
// *ssa.Function rather than through graph.Graph, since none of the four needs
// more than the reachable set and this order over it.
func order(fn *ssa.Function) []int {
	n := fn.Len()
	if n == 0 {
		return nil
	}

	visited := make([]bool, n)
	var post []int
	type frame struct{ block, next int }
	stack := []frame{{0, 0}}
	visited[0] = true
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		succ := fn.Succ(top.block)
		if top.next < len(succ) {
			s := succ[top.next]
			top.next++
			if !visited[s] {
				visited[s] = true
				stack = append(stack, frame{s, 0})
			}
			continue
		}
		post = append(post, top.block)
		stack = stack[:len(stack)-1]
	}

	rpo := make([]int, len(post))
	for i, b := range post {
		rpo[len(post)-1-i] = b
	}
	return rpo
}
