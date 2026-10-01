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
	// Pops is Code's own operand count: the number of Frame.Stack's own
	// trailing entries, top frame, an ExitBridge resume reads as arguments.
	Pops int
	// Adopts is how many of those arguments, topmost, Code takes ownership
	// of (transform.Adopts): native code hands each its own reference.
	Adopts int
	// Callee is the function address an ExitCall calls.
	Callee int
	// Closure is where an ExitCall's closure over Callee lives, nil when the
	// call names Callee directly: the interpreter calls through it, and a
	// callee materialized from this call runs through it.
	Closure *Value
	// Owned reports whether the call site retained its callee's reference: the
	// interpreter must retain a borrowed callee's reference itself before
	// pushing it, so its own CALL has one of its own to release.
	Owned bool
	// Lent are the callee frame slots this call passes without a reference
	// of its own: materializing the callee, or the interpreter running the
	// call, retains each.
	Lent []int
	// Kept are the callee frame slots this call passes owned to a borrowed
	// parameter: native code releases each once the call returns to it, so
	// the interpreter running the call retains each for its callee frame.
	Kept []int
	// Frame is the interpreter frame at the exit. An ExitRelease has a zero
	// Frame: it neither reads nor rebuilds it.
	Frame Frame
	// Results are the kinds of the values an ExitBridge, or an ExitCall to a
	// callee with register-convention results, reads back from
	// Context.Results, in order.
	Results []types.Kind
	// Word is the value an ExitRelease or ExitBox hands to the interpreter:
	// the reference to release, or the raw i64 word to box.
	Word Value
}

// Kind is why native code exits.
type Kind uint8

// Frame is the interpreter frame at an exit.
type Frame struct {
	Address, IP, Returns int
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
	// last reference named by Word.
	ExitRelease
	// ExitCall suspends native code for the interpreter to run the call to
	// Callee, then resumes it with the call's results. Frames are the
	// caller's state after the call, which is also its state while a native
	// callee runs.
	ExitCall
	// ExitBox suspends native code for the interpreter to heap-box Word, a
	// wide i64 outside the inline range, into Context.Results[0].
	ExitBox
)

// Resumes reports whether native code continues after k: only ExitDeopt
// abandons the activation.
func (k Kind) Resumes() bool {
	return k != ExitDeopt
}

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
