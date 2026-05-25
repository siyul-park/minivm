// Package bench provides reusable benchmark programs for minivm.
//
// This package is a separate Go module so that the main minivm packages carry
// no dependency on benchmark infrastructure.
package bench

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

// Fib returns a program that computes fibonacci(n) via recursive descent.
// The function is self-referential: it pushes itself via CONST_GET and calls
// recursively.  fib(35) = 9_227_465 with ~29.8 M recursive calls.
//
// BR_IF operands are byte offsets from the branch instruction start, matching
// the threaded compiler's encoding (see interp/threaded.go).
func Fib(n int32) *program.Program {
	fibFn := types.NewFunctionBuilder(&types.FunctionType{
		Params:  []types.Type{types.TypeI32},
		Returns: []types.Type{types.TypeI32},
	}).Emit(
		// if n < 2 { return n }
		instr.New(instr.LOCAL_GET, 0),
		instr.New(instr.I32_CONST, 2),
		instr.New(instr.I32_LT_S),
		instr.New(instr.BR_IF, 26), // jump forward 26 bytes to base-case LOCAL_GET
		// return fib(n-1) + fib(n-2)
		instr.New(instr.LOCAL_GET, 0),
		instr.New(instr.I32_CONST, 1),
		instr.New(instr.I32_SUB),
		instr.New(instr.CONST_GET, 0),
		instr.New(instr.CALL),
		instr.New(instr.LOCAL_GET, 0),
		instr.New(instr.I32_CONST, 2),
		instr.New(instr.I32_SUB),
		instr.New(instr.CONST_GET, 0),
		instr.New(instr.CALL),
		instr.New(instr.I32_ADD),
		instr.New(instr.RETURN),
		// base case: return n
		instr.New(instr.LOCAL_GET, 0),
		instr.New(instr.RETURN),
	).Build()

	return program.New(
		[]instr.Instruction{
			instr.New(instr.I32_CONST, uint64(n)),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		},
		program.WithConstants(fibFn),
	)
}
