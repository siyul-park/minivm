package ssa

import (
	"reflect"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// Op identifies an SSA operation or terminator.
type Op uint8

// Space identifies interpreter storage.
type Space uint8

// Slot identifies one interpreter storage location.
type Slot struct {
	// Space identifies the storage class.
	Space Space
	// Base identifies the frame base.
	Base int
	// Index identifies the slot.
	Index int
}

// Shape describes the container specialization a guard admits: an array
// whose elements are of Kind in its canonical representation
// (types.TypedArray[T] for a scalar Kind, *types.Array for KindRef), or,
// when Struct is set, a *types.Struct of struct type Type.
type Shape struct {
	// Kind is the admitted array element kind; meaningless when Struct.
	Kind types.Kind
	// Struct reports whether the admitted representation is a struct of
	// type Type instead of an array of Kind.
	Struct bool
	// Type identifies the admitted struct type, when Struct.
	Type uintptr
	// Host identifies the host field kind, when applicable.
	Host reflect.Kind
}

// Frame materializes one interpreter frame for deoptimization.
type Frame struct {
	// Address identifies the function slot.
	Address int
	// Base is the frame base offset.
	Base int
	// IP is the resume offset.
	IP int
	// Returns is the result count.
	Returns int
	// Stack is the frame operand stack.
	Stack []Operand
	// Locals are promoted slots written back on resume.
	Locals []Local
}

// Local is a promoted frame slot value.
type Local struct {
	// Index identifies the slot.
	Index int
	// Value is written back to the slot.
	Value Value
}

// Operand is one deoptimization stack entry.
type Operand struct {
	// Value is the stack value.
	Value Value
	// Owned reports whether the entry carries its reference count.
	Owned bool
}

// Operation is one SSA instruction.
type Operation struct {
	// Op identifies the IR operation.
	Op Op
	// Code identifies the guest opcode for OpExec.
	Code instr.Opcode
	// Slot identifies storage for OpLoad/OpStore.
	Slot Slot
	// Const is the value for OpConst.
	Const types.Boxed
	// Shape is the admitted specialization.
	Shape Shape
	// Frames is the deoptimization frame chain.
	Frames []Frame

	// Args are operation inputs.
	Args []Value
	// State identifies the resume state.
	State Value
	// Results are operation outputs.
	Results []Value
}

// Terminator ends a block and selects successors.
type Terminator struct {
	// Op identifies the terminator.
	Op Op
	// Args are values passed to the successor.
	Args []Value
	// State identifies the resume state.
	State Value
	// Edges identify successor blocks and arguments.
	Edges []Edge
}

// Edge names a successor and its block arguments.
type Edge struct {
	// Block is the successor id.
	Block int
	Args  []Value
}

const (
	OpConst Op = iota
	OpExec
	OpLoad
	OpStore
	OpGuardKind
	OpGuardShape
	OpGuardBounds
	OpGuardValue
	OpRetain
	OpRelease
	OpState

	OpJump
	OpBranch
	OpTable
	OpReturn
	OpComplete
	OpExit
	OpSuspend
)

const (
	SpaceLocal Space = iota
	SpaceGlobal
	SpaceUpval
)

// OverflowsI64 reports opcodes that can exceed the boxed i64 payload.
func OverflowsI64(code instr.Opcode) bool {
	switch code {
	case instr.I64_ADD, instr.I64_SUB, instr.I64_MUL, instr.I64_SHL, instr.I64_SHR_U,
		instr.I64_DIV_S, instr.I64_DIV_U:
		return true
	default:
		return false
	}
}

// String returns the operation name.
func (o Op) String() string {
	switch o {
	case OpConst:
		return "const"
	case OpExec:
		return "exec"
	case OpLoad:
		return "load"
	case OpStore:
		return "store"
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

// String returns the storage name.
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

func (o Operation) name() string {
	switch o.Op {
	case OpExec:
		return instr.TypeOf(o.Code).Mnemonic
	default:
		return o.Op.String()
	}
}
