// Package jit holds the architecture-neutral compiler IR shared by minivm's
// JIT driver and its native backends: the plan graph both frontends build and
// every backend lowers, the per-step dataflow facts that drive lowering
// decisions, the runtime layout tables a backend needs to reach into the
// interpreter's private types, and the recorded-trace data the trace frontend
// reads. It has no dependency on interp: the driver, the recorder, and every
// backend live in interp and import this package, never the reverse.
package jit

import "github.com/siyul-park/minivm/prof"

// Anchor names one native entry point: a function address and the in-function
// instruction offset a plan starts compiling from. ip is zero for a
// function-entry or module-entry anchor and positive for a loop-header
// anchor.
type Anchor struct {
	Addr int
	IP   int
}

// EntryKind classifies what a plan's root anchor represents.
type EntryKind uint8

// Plan is one compilable unit: the blocks reachable from Root, anchored at
// Anchor. A function may produce more than one Plan — the whole-function
// entry plus one per loop header — each carrying only the blocks its own
// root can reach.
type Plan struct {
	Anchor  Anchor
	Kind    EntryKind
	Root    int
	Blocks  []Block
	Carried []int
	Hoist   *Hoist

	// tails memoizes suffix's per-trace continuation blocks. It is planning
	// scratch state with no reader outside this package: a backend receives
	// only the finished Blocks list.
	tails map[*Record][]int
}

// Block is one straight-line span of steps ending in a Terminator. State is
// the per-slot dataflow facts live on entry, or nil for a tail block reached
// only through a side exit, which the backend must reload rather than assume.
type Block struct {
	Anchor Anchor
	// Tail marks a block that a plan reaches only through a cold-path or
	// bridge-resume edge, never through the block list a backend walks
	// linearly: it carries no State and gets no eagerly scheduled label.
	Tail bool
	// Bridge marks a block reached only by a bridge resume (see
	// TerminateBridge): a backend's entry dispatch treats its Anchor.IP as a
	// valid external re-entry IP alongside the plan's normal anchor.
	Bridge bool
	State  []Slot
	Steps  []Step
	Term   Terminator
}

// Edge names one control-flow successor: the anchor it targets, the resolved
// index into Plan.Blocks (or NoBlock before wiring, or while pointing outside
// the current plan), and — for every edge but the terminator's likeliest one
// — the tail blocks a cold path must still emit.
type Edge struct {
	Anchor Anchor
	Index  int
	Tail   []int
}

// Terminator ends a Block: how control leaves it, and to which edges.
type Terminator struct {
	Kind TerminatorKind
	IP   int
	// Hot is the index into Edges the compiled path treats as the likely
	// successor, or -1 when no edge is preferred.
	Hot   int
	Edges []Edge
}

// TerminatorKind is how a Block's control flow leaves it.
type TerminatorKind uint8

const (
	entryInvalid EntryKind = iota
	EntryFunction
	EntryLoop
	EntryModule
)

const (
	TerminateFallthrough TerminatorKind = iota
	TerminateBranch
	TerminateBranchIf
	TerminateBranchTable
	TerminateReturn
	TerminateComplete
	TerminateFallback
	// TerminateBridge deopts one opcode a backend cannot lower to the
	// threaded interpreter and resumes natively afterward. IP names that
	// opcode's own IP; the block reached after it (marked Block.Bridge)
	// carries the resume state and needs no edge, because resumption happens
	// through a fresh external entry, not a branch within this callable.
	TerminateBridge
)

const (
	// NoBlock marks an Edge not yet resolved to a Plan.Blocks index, or one
	// that never will be (a cold exit out of the plan entirely).
	NoBlock = -1
	// MaxHoistSlot is the largest root-frame local offset a hoisted
	// container may occupy: the offset must fit the ARM64 load immediate a
	// backend derives it with.
	MaxHoistSlot = 4095
	maxCarried   = 7
)

// Profile converts kind to the profiling vocabulary prof.Collector records
// against.
func (kind EntryKind) Profile() prof.EntryKind {
	switch kind {
	case EntryModule:
		return prof.EntryStart
	case EntryFunction:
		return prof.EntryCall
	case EntryLoop:
		return prof.EntryLoop
	default:
		return prof.EntryNone
	}
}

// Valid reports whether p's block graph is internally consistent: the root
// resolves to itself, every terminator carries the edge count its kind
// requires, and every edge and tail id names a live, matching block.
func (p Plan) Valid() bool {
	if p.Root < 0 || p.Root >= len(p.Blocks) || p.Blocks[p.Root].Tail || p.Blocks[p.Root].Anchor != p.Anchor {
		return false
	}
	switch p.Kind {
	case EntryFunction:
		if p.Anchor.Addr <= 0 || p.Anchor.IP != 0 {
			return false
		}
	case EntryLoop:
		if p.Anchor.Addr < 0 || p.Anchor.IP <= 0 {
			return false
		}
	case EntryModule:
		if p.Anchor.Addr != 0 || p.Anchor.IP != 0 {
			return false
		}
	default:
		return false
	}
	for _, block := range p.Blocks {
		switch block.Term.Kind {
		case TerminateFallthrough, TerminateReturn, TerminateComplete, TerminateFallback, TerminateBridge:
			if len(block.Term.Edges) != 0 {
				return false
			}
		case TerminateBranch:
			if len(block.Term.Edges) != 1 {
				return false
			}
		case TerminateBranchIf:
			if len(block.Term.Edges) != 2 {
				return false
			}
		case TerminateBranchTable:
			if len(block.Term.Edges) == 0 {
				return false
			}
		default:
			return false
		}
		if block.Term.Hot < -1 || (len(block.Term.Edges) > 0 && block.Term.Hot >= len(block.Term.Edges)) {
			return false
		}
		for _, edge := range block.Term.Edges {
			if edge.Index != NoBlock {
				if edge.Index < 0 || edge.Index >= len(p.Blocks) || p.Blocks[edge.Index].Anchor != edge.Anchor {
					return false
				}
			}
			for _, id := range edge.Tail {
				if id < 0 || id >= len(p.Blocks) || !p.Blocks[id].Tail {
					return false
				}
			}
		}
	}
	return true
}
