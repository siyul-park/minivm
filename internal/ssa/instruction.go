package ssa

import (
	"reflect"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// Op is what an Instruction or a Terminator does. Pure computation keeps its
// bytecode identity in Code rather than being restated here, so this set
// names only what the bytecode has no word for: the interpreter state a deopt
// resumes into, the speculation guards that must fail before an operation
// runs, explicit reference ownership, and control flow.
type Op uint8

// Space is the interpreter storage a Slot names.
type Space uint8

// Slot names one interpreter storage location an OpLoad reads or an OpStore
// writes. A slot is interpreter-visible state: a deopt resumes with whatever
// the stores before it left there.
type Slot struct {
	Space Space
	Index int
}

// Shape is the speculated container fact one heap instruction is compiled
// against: a guard admits only this shape, and the access lowered after it
// loads through it. Itab is the concrete heap type identity, Typ the struct
// type pointer a struct carries, and Host the Go kind a *HostStruct field
// converts through - a host field's width and signedness are not implied by
// its VM type, since int16, int32, and uint32 all reach the guest as i32.
// Host is reflect.Invalid for every container that is not a host view.
type Shape struct {
	Itab uintptr
	Typ  uintptr
	Host reflect.Kind
}

// Frame is one interpreter frame an OpState materializes: the function slot
// it runs in, its base offset from the entry frame, the IP it resumes at, the
// result count its teardown uses, and the operand values live on its stack,
// bottom first. The chain is multi-frame because a frontend may inline
// callees into one native frame, and the interpreter has to be handed every
// frame that inlining hid.
type Frame struct {
	Addr    int
	Base    int
	IP      int
	Returns int
	Stack   []Value
}

// Instruction is one operation inside a Block. Only the fields its Op names
// carry meaning; the rest stay zero.
type Instruction struct {
	Op Op
	// Code is the bytecode operation an OpPure, OpRead, OpWrite, OpCall, or
	// OpBridge performs.
	Code   instr.Opcode
	Slot   Slot
	Const  types.Boxed
	Shape  Shape
	Frames []Frame

	Args []Value
	// State is the interpreter state this instruction deoptimizes into, or
	// NoValue when it cannot deoptimize.
	State   Value
	Results []Value
}

// Terminator ends a Block: how control leaves it, and to which successors.
type Terminator struct {
	Op Op
	// Args is the OpBranch condition, the OpTable index, or the values
	// OpReturn hands back.
	Args  []Value
	State Value
	Edges []Edge
}

// Edge names one successor block and the arguments it passes to that block's
// parameters. Merge points take arguments on the edge instead of naming phis
// in the block, so a value is defined exactly once at its own definition.
type Edge struct {
	Block int
	Args  []Value
}

const (
	// OpConst materializes a compile-time value: a numeric literal, a null
	// ref, or a constant-pool entry, all of which are fixed once a program is
	// loaded.
	OpConst Op = iota
	// OpPure computes Code over its arguments with no effect the interpreter
	// can observe.
	OpPure
	OpSelect
	OpLoad
	OpStore
	// OpRead reads a heap container through Shape: an element, a field, a
	// length, or a payload.
	OpRead
	// OpWrite stores into a heap container through Shape. The stored value is
	// the last argument.
	OpWrite
	// OpCall enters another function. The callee is the first argument.
	OpCall
	// OpGuardKind admits only a value whose runtime kind is the result's
	// type.
	OpGuardKind
	// OpGuardShape admits only a container matching Shape.
	OpGuardShape
	// OpGuardBounds admits only an index below a length.
	OpGuardBounds
	// OpGuardValue admits only a value equal to the second argument, the
	// observed value a specialization was recorded against.
	OpGuardValue
	OpRetain
	OpRelease
	// OpBridge hands Code to the interpreter and resumes here with its
	// results. It is productive continuation, not a give-up: unlike OpExit
	// the operations after it still run natively.
	OpBridge
	// OpState materializes the interpreter frame chain a deopt resumes into.
	OpState

	OpJump
	OpBranch
	OpTable
	// OpReturn leaves the function with Args as its results.
	OpReturn
	// OpComplete ends module code, which has no return: the top-level frame
	// survives and execution advances past the end of the module.
	OpComplete
	// OpExit abandons native execution and resumes the interpreter at State.
	OpExit
	// OpSuspend leaves through a suspension point, which resumes at the
	// opcode's own IP and only ever in the frame that owns the coroutine, so
	// its State carries exactly one frame.
	OpSuspend
)

const (
	SpaceLocal Space = iota
	SpaceGlobal
	SpaceUpval
)

func (o Op) String() string {
	switch o {
	case OpConst:
		return "const"
	case OpPure:
		return "pure"
	case OpSelect:
		return "select"
	case OpLoad:
		return "load"
	case OpStore:
		return "store"
	case OpRead:
		return "read"
	case OpWrite:
		return "write"
	case OpCall:
		return "call"
	case OpGuardKind:
		return "guard.kind"
	case OpGuardShape:
		return "guard.shape"
	case OpGuardBounds:
		return "guard.bounds"
	case OpGuardValue:
		return "guard.value"
	case OpRetain:
		return "retain"
	case OpRelease:
		return "release"
	case OpBridge:
		return "bridge"
	case OpState:
		return "state"
	case OpJump:
		return "jump"
	case OpBranch:
		return "br"
	case OpTable:
		return "table"
	case OpReturn:
		return "return"
	case OpComplete:
		return "complete"
	case OpExit:
		return "exit"
	case OpSuspend:
		return "suspend"
	default:
		return "invalid"
	}
}

func (s Space) String() string {
	switch s {
	case SpaceLocal:
		return "local"
	case SpaceGlobal:
		return "global"
	case SpaceUpval:
		return "upval"
	default:
		return "invalid"
	}
}
