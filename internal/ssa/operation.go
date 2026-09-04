package ssa

import (
	"reflect"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// Op is what an Operation or a Terminator does. The guest instruction set is
// this IR's computation vocabulary: an operation the bytecode names is OpExec
// carrying that instr.Opcode, and every static fact about it is asked of instr.
// An Op of its own exists only where the guest has no word - a compile-time
// value or an interpreter slot, whose opcode identity the decoded Const or Slot
// subsumes; the block graph's own control flow; and what only an optimizing
// compiler has, from speculation guards to the state a deopt resumes into.
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

// Shape is the speculated container fact one heap operation is compiled
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

// Operation is one step inside a Block: a definition in a value graph, with no
// byte offset, no width, and no encoding. It is the same instruction that
// instr.Instruction encodes, in the other of the two forms, and it takes MLIR's
// noun rather than that one's so a reader can tell which form they hold. The
// frontend that decodes one into the other is the only place a raw operand
// becomes a Slot, a Const, a Shape, or an edge. Only the fields its Op names
// carry meaning; the rest stay zero.
type Operation struct {
	Op Op
	// Code is the bytecode operation an OpExec or OpBridge performs. Its
	// instr.Type states the operation's stack effect, and Args holds what it
	// pops in the reverse of that order, bottom of the stack first.
	Code   instr.Opcode
	Slot   Slot
	Const  types.Boxed
	Shape  Shape
	Frames []Frame

	Args []Value
	// State is the interpreter state this operation deoptimizes into, or
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
	// loaded. Const decides which, so the six bytecode opcodes that encode one
	// are not restated here.
	OpConst Op = iota
	// OpExec performs Code over its arguments: whatever that opcode computes,
	// reads, writes, or calls. Whether it is pure, overwrites a container, or
	// enters another function is instr's answer to give, not a distinction
	// this set repeats.
	OpExec
	// OpLoad reads Slot and OpStore writes it. Slot decides which storage, so
	// the eight bytecode opcodes over locals, globals, and upvalues are not
	// restated here, and a tee, which is one opcode doing both, splits into
	// the two operations it is.
	OpLoad
	OpStore
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

// name is how an operation is written: the mnemonic of the opcode it performs,
// or the name of the operation the IR invented.
func (o Operation) name() string {
	switch o.Op {
	case OpExec:
		return instr.TypeOf(o.Code).Mnemonic
	case OpBridge:
		return o.Op.String() + " " + instr.TypeOf(o.Code).Mnemonic
	default:
		return o.Op.String()
	}
}
