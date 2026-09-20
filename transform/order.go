package transform

import "github.com/siyul-park/minivm/internal/ssa"

func reversePostorder(function *ssa.Function) []int {
	n := function.Len()
	if n == 0 {
		return nil
	}

	visited := make([]bool, n)
	var post []int
	type activation struct{ block, next int }
	stack := []activation{{0, 0}}
	visited[0] = true
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		succ := function.Succ(top.block)
		if top.next < len(succ) {
			s := succ[top.next]
			top.next++
			if !visited[s] {
				visited[s] = true
				stack = append(stack, activation{s, 0})
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
