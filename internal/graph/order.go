package graph

// Order returns reachable nodes in reverse-postorder, root first.
func Order(g Graph) []int {
	n := g.Len()
	if n == 0 {
		return nil
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
		successors := g.Succ(top.node)
		if top.next < len(successors) {
			next := successors[top.next]
			top.next++
			if !visited[next] {
				visited[next] = true
				stack = append(stack, struct {
					node int
					next int
				}{next, 0})
			}
			continue
		}
		post = append(post, top.node)
		stack = stack[:len(stack)-1]
	}

	order := make([]int, len(post))
	for i, node := range post {
		order[len(post)-1-i] = node
	}
	return order
}
