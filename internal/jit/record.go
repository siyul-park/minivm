package jit

import (
	"maps"
	"slices"
)

// Status is how a recorded Trace ended.
type Status int

// Record is one recorded instruction: the Step it executed, plus the
// branch-specific bookkeeping the trace frontend needs to rebuild control
// flow from a linear recording. Cut marks a synthetic boundary the recorder
// inserted instead of a real instruction — a back-edge to a different loop
// header, or the point an opLimit-bounded recording stopped — naming Target
// as the IP execution would have resumed at.
type Record struct {
	Step
	Cut    bool
	Target int
	Taken  bool
}

// Trace is one recorded path through the bytecode: the ops observed from
// Anchor until Status ended the recording. Carried is true when the anchor
// frame held live operands on entry, which disqualifies it from becoming a
// loop plan's native back-edge.
type Trace struct {
	Anchor  Anchor
	Ops     []Record
	Status  Status
	Carried bool
}

// Tree is every trace recorded from one Anchor: the root path and the
// branches recorded at its hot exits, keyed by the exit id Exits assigns.
type Tree struct {
	Root     *Trace
	Branches map[int]*Trace
	Hits     []int64
	Exits    map[Anchor]int

	Attempts int
}

const (
	StatusFallback Status = iota
	StatusLoop
	StatusReturned
	StatusCompleted
	StatusPartial
	StatusAborted
)

// Snapshot returns a compile-time-stable copy of the fields readers consume
// off a tree (root pointer, branches, hits). Published *Trace values are
// immutable, so sharing the pointers is safe; copying the container lets the
// trace compiler lower a root without racing a recorder that keeps mutating
// the live tree concurrently.
func (t *Tree) Snapshot() *Tree {
	return &Tree{
		Root:     t.Root,
		Branches: maps.Clone(t.Branches),
		Hits:     slices.Clone(t.Hits),
	}
}
