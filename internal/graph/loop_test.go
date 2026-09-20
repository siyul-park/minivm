package graph_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/internal/graph"
)

func TestLoopHeaders(t *testing.T) {
	t.Run("finds no headers in an acyclic graph", func(t *testing.T) {
		// 0 -> 1, 0 -> 2, 1 -> 3, 2 -> 3.
		g := newFixture(4, [][2]int{{0, 1}, {0, 2}, {1, 3}, {2, 3}})
		d := graph.NewDominance(g)

		require.Empty(t, graph.LoopHeaders(g, d))
	})

	t.Run("names the target of a natural loop back edge", func(t *testing.T) {
		// 0 -> 1 (header), 1 -> 2 (body), 2 -> 1 (back edge), 2 -> 3 (exit).
		g := newFixture(4, [][2]int{{0, 1}, {1, 2}, {2, 1}, {2, 3}})
		d := graph.NewDominance(g)

		require.ElementsMatch(t, []int{1}, graph.LoopHeaders(g, d))
	})

	t.Run("names every header of nested loops", func(t *testing.T) {
		// 0 -> 1 (outer header), 1 -> 2 (inner header), 2 -> 3 (inner body),
		// 3 -> 2 (inner back edge), 3 -> 1 (outer back edge), 3 -> 4 (exit).
		g := newFixture(5, [][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 2}, {3, 1}, {3, 4}})
		d := graph.NewDominance(g)

		require.ElementsMatch(t, []int{1, 2}, graph.LoopHeaders(g, d))
	})

	t.Run("excludes a backward edge whose target does not dominate its source", func(t *testing.T) {
		// 0 -> 1, 0 -> 2, 1 -> 3, 2 -> 3, and 3 -> 1: a lower-numbered
		// backward edge, but 1 does not dominate 3 (the sibling arm
		// through 2 bypasses it), so it is not a natural loop.
		g := newFixture(4, [][2]int{{0, 1}, {0, 2}, {1, 3}, {2, 3}, {3, 1}})
		d := graph.NewDominance(g)

		require.Empty(t, graph.LoopHeaders(g, d))
	})
}
