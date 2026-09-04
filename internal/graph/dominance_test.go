package graph_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/internal/graph"
)

// fixture is a fixed adjacency-list graph.Graph built from an explicit
// edge list, so node numbering and edge order stay deterministic across
// runs — it is a public API client of graph.Graph, not a private-state
// double.
type fixture struct {
	succ [][]int
	pred [][]int
}

func newFixture(n int, edges [][2]int) *fixture {
	g := &fixture{succ: make([][]int, n), pred: make([][]int, n)}
	for _, e := range edges {
		from, to := e[0], e[1]
		g.succ[from] = append(g.succ[from], to)
		g.pred[to] = append(g.pred[to], from)
	}
	return g
}

func (g *fixture) Len() int         { return len(g.succ) }
func (g *fixture) Succ(n int) []int { return g.succ[n] }
func (g *fixture) Pred(n int) []int { return g.pred[n] }

func TestNewDominance(t *testing.T) {
	t.Run("computes dominance for an empty graph", func(t *testing.T) {
		g := newFixture(0, nil)

		d := graph.NewDominance(g)

		require.NotNil(t, d)
	})

	t.Run("computes dominance for a single node with no edges", func(t *testing.T) {
		g := newFixture(1, nil)

		d := graph.NewDominance(g)

		require.True(t, d.Dominates(0, 0))
	})
}

func TestDominance_Dominates(t *testing.T) {
	t.Run("entry dominates every node reachable through a diamond, but neither arm dominates the join", func(t *testing.T) {
		// 0 -> 1, 0 -> 2, 1 -> 3, 2 -> 3.
		g := newFixture(4, [][2]int{{0, 1}, {0, 2}, {1, 3}, {2, 3}})

		d := graph.NewDominance(g)

		require.True(t, d.Dominates(0, 3), "entry reaches the join through every path")
		require.False(t, d.Dominates(1, 3), "the sibling arm through 2 bypasses 1")
		require.False(t, d.Dominates(2, 3), "the sibling arm through 1 bypasses 2")
		require.True(t, d.Dominates(0, 1))
		require.True(t, d.Dominates(0, 2))
	})

	t.Run("a loop header dominates its body across the back edge, never the reverse", func(t *testing.T) {
		// 0 -> 1 (header), 1 -> 2 (body), 2 -> 1 (back edge), 2 -> 3 (exit).
		g := newFixture(4, [][2]int{{0, 1}, {1, 2}, {2, 1}, {2, 3}})

		d := graph.NewDominance(g)

		require.True(t, d.Dominates(1, 2), "the header dominates the body on every iteration")
		require.False(t, d.Dominates(2, 1), "the back edge does not make the body dominate its own header")
		require.True(t, d.Dominates(1, 3), "the only path to the exit passes through the header")
		require.True(t, d.Dominates(0, 3))
	})

	t.Run("an unreachable node dominates nothing, including itself", func(t *testing.T) {
		// 0 -> 1. Node 2 has no incoming edge and is never reached.
		g := newFixture(3, [][2]int{{0, 1}})

		d := graph.NewDominance(g)

		require.False(t, d.Dominates(0, 2))
		require.False(t, d.Dominates(2, 2))
	})

	t.Run("disagrees with a flat before-in-the-stream approximation across sibling arms", func(t *testing.T) {
		// A store at node 1 and a reload at node 4 would look ordered by a
		// flat instruction-position scan alone (1 precedes 4), the
		// approximation asm's allocator used before it consulted real
		// dominance. Real dominance rejects it: the sibling arm through
		// node 2 reaches node 4 without ever running node 1.
		g := newFixture(5, [][2]int{{0, 1}, {0, 2}, {1, 3}, {2, 3}, {3, 4}})

		d := graph.NewDominance(g)

		require.False(t, d.Dominates(1, 4), "the sibling arm through 2 bypasses 1 even though 1 sits earlier in node order")
		require.True(t, d.Dominates(3, 4), "the join point genuinely dominates everything after it")
		require.True(t, d.Dominates(0, 4))
	})
}

func TestDominance_IDom(t *testing.T) {
	t.Run("the entry has no immediate dominator", func(t *testing.T) {
		g := newFixture(2, [][2]int{{0, 1}})

		d := graph.NewDominance(g)

		require.Equal(t, -1, d.IDom(0))
	})

	t.Run("an unreachable node has no immediate dominator", func(t *testing.T) {
		g := newFixture(2, nil)

		d := graph.NewDominance(g)

		require.Equal(t, -1, d.IDom(1))
	})

	t.Run("a join's immediate dominator is the nearest common ancestor of its arms, not either arm", func(t *testing.T) {
		// 0 -> 1, 0 -> 2, 1 -> 3, 2 -> 3.
		g := newFixture(4, [][2]int{{0, 1}, {0, 2}, {1, 3}, {2, 3}})

		d := graph.NewDominance(g)

		require.Equal(t, 0, d.IDom(1))
		require.Equal(t, 0, d.IDom(2))
		require.Equal(t, 0, d.IDom(3), "neither sibling arm dominates the join, so its idom is their common ancestor")
	})

	t.Run("a loop body's immediate dominator is its header, not the back edge", func(t *testing.T) {
		// 0 -> 1 (header), 1 -> 2 (body), 2 -> 1 (back edge), 2 -> 3 (exit).
		g := newFixture(4, [][2]int{{0, 1}, {1, 2}, {2, 1}, {2, 3}})

		d := graph.NewDominance(g)

		require.Equal(t, 1, d.IDom(2))
		require.Equal(t, 2, d.IDom(3))
	})
}
