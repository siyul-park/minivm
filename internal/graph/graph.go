// Package graph computes flow-graph facts — dominance and natural loop
// headers — over any caller-supplied directed graph. It is architecture-
// and IR-neutral: nothing here knows about instructions, blocks, or bytes.
package graph

// Graph is a directed graph whose nodes are the dense integers
// [0, Len()), node 0 being the entry every dominance and loop query is
// computed from. A caller adapts its own node type by implementing these
// three methods directly on it, so no per-query adapter allocates.
type Graph interface {
	// Len returns the number of nodes.
	Len() int
	// Successors returns node's successors.
	Successors(node int) []int
	// Predecessors returns node's predecessors.
	Predecessors(node int) []int
}
