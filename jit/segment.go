package jit

import (
	"unsafe"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/types"
)

// Call is the per-invocation input for Invoke. Stack and Globals supply the
// VM stack and globals slice headers; Invoke reads &Stack[0]/&Globals[0]
// internally and does not retain the slices past its return. BP is the
// current frame's base index.
type Call struct {
	Stack   []types.Boxed
	Globals []types.Boxed
	BP      int
}

// Outcome is the result of Invoke. Returns are the raw native return values
// (callers convert via Ret); NextIP is the threaded resume IP written by the
// callable through ScratchNext (zero when the callable did not set it).
type Outcome struct {
	Returns []asm.Value
	NextIP  int
}

// Segment scratch layout shared by lowerers and interpreter adapters.
// Stack values use asm.Signature.Args/Returns; scratch carries VM context
// and control metadata only.
const (
	ScratchStack = iota
	ScratchGlobals
	ScratchBP
	ScratchNext
	ScratchCount
)

// Invoke packs scratch and args, invokes callable, and returns the parsed
// Outcome. Consumers retain ownership of stack/globals mutation; Invoke only
// hands the callable enough info to read them.
func Invoke(callable asm.Callable, in Call, args []asm.Value) (Outcome, error) {
	scratch := [ScratchCount]uint64{
		ScratchStack:   stackBase(in.Stack),
		ScratchGlobals: stackBase(in.Globals),
		ScratchBP:      uint64(in.BP),
	}
	returns, err := callable.Call(args, scratch[:])
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{Returns: returns, NextIP: int(scratch[ScratchNext])}, nil
}

// Arg converts a boxed VM stack value into the segment ABI value shape.
func Arg(v types.Boxed) asm.Value {
	return asm.I64(uint64(v))
}

// Ret converts a segment ABI return value into a boxed VM stack value.
func Ret(v asm.Value) types.Boxed {
	return types.Boxed(v.Bits())
}

// stackBase returns the address of s[0] packed into a uint64, or zero when
// s is empty so native code receives a well-defined sentinel rather than a
// wild pointer.
func stackBase(s []types.Boxed) uint64 {
	if len(s) == 0 {
		return 0
	}
	return uint64(uintptr(unsafe.Pointer(&s[0])))
}
