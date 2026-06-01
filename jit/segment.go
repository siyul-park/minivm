package jit

import (
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/types"
)

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

// Arg converts a boxed VM stack value into the segment ABI value shape.
func Arg(v types.Boxed) asm.Value {
	return asm.I64(uint64(v))
}

// Ret converts a segment ABI return value into a boxed VM stack value.
func Ret(v asm.Value) types.Boxed {
	return types.Boxed(v.Bits())
}
