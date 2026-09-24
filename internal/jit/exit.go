package jit

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/types"
)

// Exit maps one native exit to its interpreter state.
type Exit struct {
	Kind Kind
	// Code is the opcode resumed by an ExitBridge.
	Code instr.Opcode
	// Adopts is the number of popped operands transferred to Code.
	Adopts int
	// Pops is Code's own operand count: the number of Frame.Stack's own
	// trailing entries, top frame, an ExitBridge resume reads as arguments.
	Pops int
	// Callee is the function address resumed by an ExitCall.
	Callee int
	// Owned reports whether the call site retained Callee's reference: an
	// ExitCall replay must retainBox a borrowed Callee before pushing it, so
	// the interpreter's own CALL has a reference of its own to release.
	Owned bool
	// Frames are the interpreter frames at the exit, innermost last. An
	// ExitRelease has none: it neither reads nor rebuilds them.
	Frames []Frame
	// Results are the kinds of the values an ExitBridge reads back from
	// Context.Results, or an ExitCall from the callee's frame base, in order.
	Results []types.Kind
	// Word is the value an ExitRelease or ExitBox hands to the interpreter:
	// the reference to release, or the raw i64 word to box.
	Word Value
}

// Kind is why native code exits.
type Kind uint8

// Frame is one interpreter frame at an exit.
type Frame struct {
	Address, Base, IP, Returns int
	// Stack is the operand stack, bottom first.
	Stack []Operand
	// Locals are slots the interpreter writes back before resuming.
	Locals []Local
}

// Operand is one operand stack entry at an exit.
type Operand struct {
	Value
	// Owned reports whether the entry carries its own reference count.
	Owned bool
}

// Local is one slot value at an exit.
type Local struct {
	Index int
	Value
}

// Value is where a native value lives at an exit and how it boxes: a
// register in the saved register file or a spill slot of the activation.
type Value struct {
	Kind types.Kind
	Loc  asm.Loc
}

const (
	// ExitDeopt abandons native code: the interpreter rebuilds Frames and
	// continues threaded.
	ExitDeopt Kind = iota
	// ExitBridge suspends native code for the interpreter to perform Code,
	// then resumes it.
	ExitBridge
	// ExitSafepoint suspends native code at a loop header for the
	// interpreter to run its safepoint and refill Context.Budget.
	ExitSafepoint
	// ExitRelease suspends native code for the interpreter to release the
	// last reference to Release.
	ExitRelease
	// ExitCall suspends native code for the interpreter to call Callee; it
	// resumes once the callee returns. Frames are the caller's state after
	// the call, which is also its state while a native callee runs.
	ExitCall
	// ExitBox suspends native code for the interpreter to heap-box Word, a
	// wide i64 outside the inline range, into Context.Results[0].
	ExitBox
)

// String returns the exit kind name.
func (k Kind) String() string {
	switch k {
	case ExitDeopt:
		return "deopt"
	case ExitBridge:
		return "bridge"
	case ExitSafepoint:
		return "safepoint"
	case ExitRelease:
		return "release"
	case ExitCall:
		return "call"
	case ExitBox:
		return "box"
	default:
		return "invalid"
	}
}
