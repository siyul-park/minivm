package graph_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/internal/graph"
)

func TestOrder(t *testing.T) {
	t.Run("returns reachable nodes from the entry in reverse postorder", func(t *testing.T) {
		g := newFixture(5, [][2]int{{0, 1}, {0, 2}, {1, 3}, {2, 3}, {3, 4}})

		require.Equal(t, []int{0, 2, 1, 3, 4}, graph.Order(g))
	})

	t.Run("returns no nodes for an empty graph", func(t *testing.T) {
		require.Nil(t, graph.Order(newFixture(0, nil)))
	})

	t.Run("visits a cycle once", func(t *testing.T) {
		g := newFixture(3, [][2]int{{0, 1}, {1, 2}, {2, 1}})

		require.Equal(t, []int{0, 1, 2}, graph.Order(g))
	})

	t.Run("omits unreachable nodes", func(t *testing.T) {
		g := newFixture(4, [][2]int{{0, 1}, {2, 3}})

		require.Equal(t, []int{0, 1}, graph.Order(g))
	})
}
