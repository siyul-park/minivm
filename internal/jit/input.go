package jit

import "github.com/siyul-park/minivm/types"

// Input is the compile-time-stable snapshot a JIT driver plans and lowers
// against. Producing one is the interpreter's job: it is the only side that
// holds the private state (function bodies, constants, globals, heap,
// declared types, recorded traces) a snapshot copies out of.
//
// Nothing reachable through an Input is storage the interpreter keeps
// mutating. Constants, Globals, and Decl are fixed once a program is loaded,
// a published Trace is immutable, and Objects resolves the live heap into
// immutable facts while the snapshot still runs on the interpreter's own
// goroutine. A compile therefore reads no cell another goroutine can change
// underneath it.
type Input struct {
	Traces   RecordedTraces
	Address  int
	Function *types.Function
	// Constants is the module's constant pool, written once when the
	// program is loaded and read-only from then on; the plan and any
	// published module keep referencing it after Compile returns.
	Constants []types.Boxed
	Globals   []types.Kind
	Objects   Objects
	// Decl is the program's declared-type table, indexed by the type operand
	// of STRUCT_NEW and REF_CAST.
	Decl      []types.Type
	Layout    Layout
	Installed bool
}

// Objects is the compile-time-stable view of the heap: every address a plan
// can name, resolved to the facts a frontend or a lowering reads off the cell
// living there. Address zero names the module body, exactly as it does in the
// interpreter's dispatch table.
//
// A resolved address is present even when its cell carries no fact at all, so
// a lowering that only needs to know an address named a live cell tests for
// membership.
type Objects map[int]Object

// Object is one heap cell as a compile sees it. Every field is zero for a
// cell that carries no such fact, and none of them is re-read from the heap
// after the snapshot resolves it.
type Object struct {
	// Fn is the function published at this address.
	Fn *types.Function
	// Typ is the type a struct cell carries.
	Typ *types.StructType
	// Array is the concrete itab of a primitive typed-array cell, which
	// ElemShapeByItab resolves to the element kind and stride an access
	// lowers with. A ref array carries none: its elements are boxed, so no
	// element kind resolves from the container alone.
	Array uintptr
	// Calls is the address of the function a closure cell calls.
	Calls int
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

// Function returns the function published at addr, or nil when addr names no
// function.
func (o Objects) Function(addr int) *types.Function {
	return o[addr].Fn
}
