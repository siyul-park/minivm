// Package graph computes architecture- and IR-neutral flow-graph facts
// over caller-supplied directed graphs.
package graph

// Graph is a directed graph whose nodes are the dense integers
// [0, Len()), node 0 being the entry every query is computed from. A caller
// adapts its own node type by implementing these three methods directly on it,
// so no per-query adapter allocates.
type Graph interface {
	// Len returns the number of nodes.
	Len() int
	// Successors returns node's successors.
	Successors(node int) []int
	// Predecessors returns node's predecessors.
	Predecessors(node int) []int
}
