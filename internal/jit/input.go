package jit

import "github.com/siyul-park/minivm/types"

// Input is the compile-time-stable snapshot a JIT driver plans and lowers
// against. Producing one is the interpreter's job: it is the only side that
// holds the private state (function bodies, constants, globals, heap,
// declared types, recorded traces) a snapshot copies out of.
type Input struct {
	Traces   RecordedTraces
	Address  int
	Function *types.Function
	Module   *types.Function
	// Constants and Heap are read-only views the plan and any published
	// module keep referencing after Compile returns.
	Constants []types.Boxed
	Globals   []types.Kind
	Heap      []types.Value
	// Decl is the program's declared-type table, indexed by the type operand
	// of STRUCT_NEW and REF_CAST.
	Decl      []types.Type
	Layout    Layout
	Installed bool
}

// RecordedTraces is the compile-time-stable trace data TracePlan reads: the
// anchors recorded for one function and the published tree rooted at one of
// them. interp's tracer satisfies it, but TracePlan depends on this narrow
// read view rather than the live recorder itself — the recorder clones the
// whole running interpreter and single-steps its threaded closures, a
// facility that belongs to interp alone and can never follow this portable
// planner out of it.
type RecordedTraces interface {
	Anchors(addr int) []int
	RootAt(a Anchor) *Tree
}

// Resolve returns the function addr names: module when addr is zero, or the
// heap function at addr otherwise.
func Resolve(module *types.Function, heap []types.Value, addr int) *types.Function {
	if addr == 0 {
		return module
	}
	if addr < 0 || addr >= len(heap) {
		return nil
	}
	fn, _ := heap[addr].(*types.Function)
	return fn
}
