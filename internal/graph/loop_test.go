package graph_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/internal/graph"
)

func TestLoops(t *testing.T) {
	t.Run("finds none in an acyclic graph", func(t *testing.T) {
		g := newFixture(4, [][2]int{{0, 1}, {0, 2}, {1, 3}, {2, 3}})

		require.Empty(t, graph.Loops(g, graph.NewDominance(g)))
	})

	t.Run("returns the header and body of a natural loop", func(t *testing.T) {
		g := newFixture(5, [][2]int{{0, 1}, {1, 2}, {2, 1}, {2, 3}, {3, 4}})

		loops := graph.Loops(g, graph.NewDominance(g))

		require.Len(t, loops, 1)
		require.Equal(t, 1, loops[0].Header)
		require.Equal(t, map[int]bool{1: true, 2: true}, loops[0].Body)
	})

	t.Run("orders nested loops innermost first", func(t *testing.T) {
		// 0 -> 1 (outer header), 1 -> 2 (inner header), 2 -> 3 (inner body),
		// 3 -> 2 (inner back edge), 3 -> 1 (outer back edge), 3 -> 4 (exit).
		g := newFixture(5, [][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 2}, {3, 1}, {3, 4}})

		loops := graph.Loops(g, graph.NewDominance(g))

		require.Len(t, loops, 2)
		require.Equal(t, 2, loops[0].Header)
		require.Equal(t, map[int]bool{2: true, 3: true}, loops[0].Body)
		require.Equal(t, 1, loops[1].Header)
		require.Equal(t, map[int]bool{1: true, 2: true, 3: true}, loops[1].Body)
	})

	t.Run("merges all back edges targeting the same header", func(t *testing.T) {
		g := newFixture(6, [][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 1}, {2, 4}, {4, 5}, {5, 1}})

		loops := graph.Loops(g, graph.NewDominance(g))

		require.Len(t, loops, 1)
		require.Equal(t, map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true}, loops[0].Body)
	})

	t.Run("excludes a backward edge whose target does not dominate its source", func(t *testing.T) {
		// 1 does not dominate 3: the sibling arm through 2 bypasses it.
		g := newFixture(4, [][2]int{{0, 1}, {0, 2}, {1, 3}, {2, 3}, {3, 1}})

		require.Empty(t, graph.Loops(g, graph.NewDominance(g)))
	})

	t.Run("names the unique outside predecessor as preheader", func(t *testing.T) {
		g := newFixture(4, [][2]int{{0, 1}, {1, 2}, {2, 1}, {2, 3}})

		loops := graph.Loops(g, graph.NewDominance(g))

		require.Len(t, loops, 1)
		require.Equal(t, 0, loops[0].Preheader)
	})

	t.Run("has no preheader with multiple outside predecessors", func(t *testing.T) {
		g := newFixture(6, [][2]int{{0, 1}, {0, 2}, {1, 3}, {2, 3}, {3, 4}, {4, 3}})

		loops := graph.Loops(g, graph.NewDominance(g))

		require.Len(t, loops, 1)
		require.Equal(t, -1, loops[0].Preheader)
	})

	t.Run("has no preheader when the outside predecessor branches elsewhere", func(t *testing.T) {
		g := newFixture(5, [][2]int{{0, 2}, {2, 1}, {2, 3}, {1, 4}, {4, 1}})

		loops := graph.Loops(g, graph.NewDominance(g))

		require.Len(t, loops, 1)
		require.Equal(t, -1, loops[0].Preheader)
	})

	t.Run("has no preheader for a loop rooted at the entry", func(t *testing.T) {
		g := newFixture(1, [][2]int{{0, 0}})

		loops := graph.Loops(g, graph.NewDominance(g))

		require.Len(t, loops, 1)
		require.Equal(t, -1, loops[0].Preheader)
	})
}
