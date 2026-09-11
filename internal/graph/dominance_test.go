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

func TestFrontier(t *testing.T) {
	t.Run("names the join each arm of a diamond stops being the only definition at", func(t *testing.T) {
		entry, left, right, join := 0, 1, 2, 3
		g := newFixture(4, [][2]int{{entry, left}, {entry, right}, {left, join}, {right, join}})

		f := graph.Frontier(g, graph.NewDominance(g))

		want := make([][]int, 4)
		want[entry] = nil
		want[left] = []int{join}
		want[right] = []int{join}
		want[join] = nil
		require.Equal(t, want, f)
	})

	t.Run("names a loop header as the frontier of every block its body reaches it from", func(t *testing.T) {
		entry, header, body, latch, exit := 0, 1, 2, 3, 4
		g := newFixture(5, [][2]int{{entry, header}, {header, body}, {body, latch}, {latch, header}, {body, exit}})

		f := graph.Frontier(g, graph.NewDominance(g))

		want := make([][]int, 5)
		want[entry] = nil
		want[header] = []int{header}
		want[body] = []int{header}
		want[latch] = []int{header}
		want[exit] = nil
		require.Equal(t, want, f)
	})

	t.Run("leaves a graph with no join empty", func(t *testing.T) {
		// 0 -> 1 -> 2.
		g := newFixture(3, [][2]int{{0, 1}, {1, 2}})

		f := graph.Frontier(g, graph.NewDominance(g))

		require.Equal(t, [][]int{nil, nil, nil}, f)
	})

	t.Run("leaves out a join only unreachable nodes reach", func(t *testing.T) {
		// 0 -> 1; 2 -> 1 and 3 -> 1 are unreachable from the entry.
		g := newFixture(4, [][2]int{{0, 1}, {2, 1}, {3, 1}})

		f := graph.Frontier(g, graph.NewDominance(g))

		require.Equal(t, [][]int{nil, nil, nil, nil}, f)
	})
}

func TestDominance_Dominates(t *testing.T) {
	t.Run("entry dominates every node reachable through a diamond, but neither arm dominates the join", func(t *testing.T) {
		entry, left, right, join := 0, 1, 2, 3
		g := newFixture(4, [][2]int{{entry, left}, {entry, right}, {left, join}, {right, join}})

		d := graph.NewDominance(g)

		require.True(t, d.Dominates(entry, join))
		require.False(t, d.Dominates(left, join))
		require.False(t, d.Dominates(right, join))
		require.True(t, d.Dominates(entry, left))
		require.True(t, d.Dominates(entry, right))
	})

	t.Run("a loop header dominates its body across the back edge, never the reverse", func(t *testing.T) {
		entry, header, body, exit := 0, 1, 2, 3
		g := newFixture(4, [][2]int{{entry, header}, {header, body}, {body, header}, {body, exit}})

		d := graph.NewDominance(g)

		require.True(t, d.Dominates(header, body))
		require.False(t, d.Dominates(body, header))
		require.True(t, d.Dominates(header, exit))
		require.True(t, d.Dominates(entry, exit))
	})

	t.Run("an unreachable node dominates nothing, including itself", func(t *testing.T) {
		// 0 -> 1. Node 2 has no incoming edge and is never reached.
		g := newFixture(3, [][2]int{{0, 1}})

		d := graph.NewDominance(g)

		require.False(t, d.Dominates(0, 2))
		require.False(t, d.Dominates(2, 2))
	})

	t.Run("disagrees with a flat before-in-the-stream approximation across sibling arms", func(t *testing.T) {
		// store and reload would look ordered by a flat instruction-position
		// scan alone (store precedes reload), the approximation asm's
		// allocator used before it consulted real dominance. Real dominance
		// rejects it: the sibling arm reaches reload without ever running
		// store.
		entry, store, sibling, join, reload := 0, 1, 2, 3, 4
		g := newFixture(5, [][2]int{{entry, store}, {entry, sibling}, {store, join}, {sibling, join}, {join, reload}})

		d := graph.NewDominance(g)

		require.False(t, d.Dominates(store, reload))
		require.True(t, d.Dominates(join, reload))
		require.True(t, d.Dominates(entry, reload))
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
		entry, left, right, join := 0, 1, 2, 3
		g := newFixture(4, [][2]int{{entry, left}, {entry, right}, {left, join}, {right, join}})

		d := graph.NewDominance(g)

		require.Equal(t, entry, d.IDom(left))
		require.Equal(t, entry, d.IDom(right))
		require.Equal(t, entry, d.IDom(join))
	})

	t.Run("a loop body's immediate dominator is its header, not the back edge", func(t *testing.T) {
		// 0 -> 1 (header), 1 -> 2 (body), 2 -> 1 (back edge), 2 -> 3 (exit).
		g := newFixture(4, [][2]int{{0, 1}, {1, 2}, {2, 1}, {2, 3}})

		d := graph.NewDominance(g)

		require.Equal(t, 1, d.IDom(2))
		require.Equal(t, 2, d.IDom(3))
	})
}
