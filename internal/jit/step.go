package jit

import (
	"reflect"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// Step is one lowerable instruction inside a Block, together with the
// dataflow facts a backend needs to lower it without re-deriving them:
// operands, the observed or statically resolved result kind, and the
// container shape a guarded access checks against.
type Step struct {
	Op       instr.Opcode
	Args     [2]uint64
	Seen     types.Boxed
	Arg      types.Boxed
	Shape    Shape
	Terminal bool

	Fn     int
	IP     int
	Depth  int
	Callee int
	Known  bool
}

// Slot is one dataflow fact about a value the plan-building walk tracks
// across a block: its kind, where it derives its reference count from, and —
// for a value backed by a VM slot — which one.
type Slot struct {
	Kind    types.Kind
	Backing Backing
	// Offset is the local, global, or upval index Backing names, meaningful
	// only when Backing designates one of those deferred sources.
	Offset int
	Ref    int

	refKnown    bool
	callee      int
	calleeKnown bool
	styp        *types.StructType
	atyp        *types.ArrayType
	val         int32
	valKnown    bool
}

// Shape is how one operand's container was observed: the concrete heap itab,
// the struct type it carries (for a struct), and — for a *HostStruct access —
// the Go kind the accessed field converts through.
type Shape struct {
	Itab uintptr
	Typ  uintptr
	// Field is the Go kind a *HostStruct access read its field through. A
	// host field converts on the way out, so its width and signedness are
	// what the read loads with, and the VM kind alone does not name them:
	// int16, int32, and uint32 all reach the guest as i32. It is
	// reflect.Invalid for every other container, and for a host field the
	// lowerer has no row for.
	Field reflect.Kind
}

// Hoist marks one loop-invariant container: a ref local that every block in a
// loop plan leaves untouched, so a backend may derive its heap cell, shape
// guard, and slice header once per native entry instead of once per access.
// Want is the recorded primitive-array itab; the planner admits only
// backend-supported accesses whose root-frame slot fits the ARM64 load
// immediate.
type Hoist struct {
	Local int
	Want  uintptr
}

// Args reads inst's operands into the fixed two-word form a Step carries
// them in.
func Args(inst instr.Instruction) [2]uint64 {
	var args [2]uint64
	for idx, width := range inst.Type().Widths {
		if idx >= len(args) || width < 0 {
			break
		}
		args[idx] = inst.Operand(idx)
	}
	return args
}
