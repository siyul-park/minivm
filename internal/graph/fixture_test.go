package graph_test

type fixture struct {
	succ [][]int
	pred [][]int
}

func (g *fixture) Len() int                 { return len(g.succ) }
func (g *fixture) Successors(n int) []int   { return g.succ[n] }
func (g *fixture) Predecessors(n int) []int { return g.pred[n] }

func newFixture(n int, edges [][2]int) *fixture {
	g := &fixture{succ: make([][]int, n), pred: make([][]int, n)}
	for _, e := range edges {
		from, to := e[0], e[1]
		g.succ[from] = append(g.succ[from], to)
		g.pred[to] = append(g.pred[to], from)
	}
	return g
}
