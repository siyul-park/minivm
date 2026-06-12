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
// BR_IF operands are byte offsets from the branch instruction end, matching
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

// indirectFibViaLocal returns a recursive fibonacci program where each
// recursive call flows the function ref through a local before CALL.
func indirectFibViaLocal(n int32) *program.Program {
	fibCode := []instr.Instruction{
		instr.New(instr.LOCAL_GET, 0),
		instr.New(instr.I32_CONST, 2),
		instr.New(instr.I32_LT_S),
		instr.New(instr.BR_IF, 0),
		instr.New(instr.CONST_GET, 0),
		instr.New(instr.LOCAL_SET, 1),
		instr.New(instr.LOCAL_GET, 0),
		instr.New(instr.I32_CONST, 1),
		instr.New(instr.I32_SUB),
		instr.New(instr.LOCAL_GET, 1),
		instr.New(instr.CALL),
		instr.New(instr.CONST_GET, 0),
		instr.New(instr.LOCAL_SET, 1),
		instr.New(instr.LOCAL_GET, 0),
		instr.New(instr.I32_CONST, 2),
		instr.New(instr.I32_SUB),
		instr.New(instr.LOCAL_GET, 1),
		instr.New(instr.CALL),
		instr.New(instr.I32_ADD),
		instr.New(instr.RETURN),
		instr.New(instr.LOCAL_GET, 0),
		instr.New(instr.RETURN),
	}
	patchBranch(fibCode, 3, 20)

	fibFn := types.NewFunctionBuilder(&types.FunctionType{
		Params:  []types.Type{types.TypeI32},
		Returns: []types.Type{types.TypeI32},
	}).WithLocals(types.TypeRef).Emit(fibCode...).Build()

	return program.New(
		[]instr.Instruction{
			instr.New(instr.I32_CONST, uint64(n)),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		},
		program.WithConstants(fibFn),
	)
}

// closureCounter returns a program that creates one closure and runs a hot
// upvalue increment loop in the closure body.
func closureCounter(iterations int32) *program.Program {
	body := []instr.Instruction{
		instr.New(instr.I32_CONST, uint64(iterations)),
		instr.New(instr.LOCAL_SET, 0),
		instr.New(instr.UPVAL_GET, 0),
		instr.New(instr.I32_CONST, 1),
		instr.New(instr.I32_ADD),
		instr.New(instr.UPVAL_SET, 0),
		instr.New(instr.LOCAL_GET, 0),
		instr.New(instr.I32_CONST, 1),
		instr.New(instr.I32_SUB),
		instr.New(instr.LOCAL_TEE, 0),
		instr.New(instr.BR_IF, 0),
		instr.New(instr.UPVAL_GET, 0),
		instr.New(instr.RETURN),
	}
	patchBranch(body, 10, 2)

	fn := types.NewFunctionBuilder(&types.FunctionType{
		Returns: []types.Type{types.TypeI32},
	}).WithLocals(types.TypeI32).WithCaptures(types.TypeI32).Emit(body...).Build()

	return program.New(
		[]instr.Instruction{
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CLOSURE_NEW),
			instr.New(instr.CALL),
		},
		program.WithConstants(fn),
	)
}

// typedArraySum returns a program that passes a rooted []i32 array into a hot
// function and sums it through ARRAY_LEN/ARRAY_GET.
func typedArraySum(size int32) *program.Program {
	array := make(types.TypedArray[int32], size)
	for j := range array {
		array[j] = int32(j + 1)
	}

	sumCode := []instr.Instruction{
		instr.New(instr.I32_CONST, 0),
		instr.New(instr.LOCAL_SET, 1),
		instr.New(instr.I32_CONST, 0),
		instr.New(instr.LOCAL_SET, 2),
		instr.New(instr.LOCAL_GET, 0),
		instr.New(instr.ARRAY_LEN),
		instr.New(instr.LOCAL_SET, 3),
		instr.New(instr.LOCAL_GET, 2),
		instr.New(instr.LOCAL_GET, 3),
		instr.New(instr.I32_GE_S),
		instr.New(instr.BR_IF, 0),
		instr.New(instr.LOCAL_GET, 1),
		instr.New(instr.LOCAL_GET, 0),
		instr.New(instr.LOCAL_GET, 2),
		instr.New(instr.ARRAY_GET),
		instr.New(instr.I32_ADD),
		instr.New(instr.LOCAL_SET, 1),
		instr.New(instr.LOCAL_GET, 2),
		instr.New(instr.I32_CONST, 1),
		instr.New(instr.I32_ADD),
		instr.New(instr.LOCAL_SET, 2),
		instr.New(instr.BR, 0),
		instr.New(instr.LOCAL_GET, 1),
		instr.New(instr.RETURN),
	}
	patchBranch(sumCode, 10, 22)
	patchBranch(sumCode, 21, 7)

	sumFn := types.NewFunctionBuilder(&types.FunctionType{
		Params:  []types.Type{types.TypeRef},
		Returns: []types.Type{types.TypeI32},
	}).WithLocals(types.TypeI32, types.TypeI32, types.TypeI32).Emit(sumCode...).Build()

	return program.New(
		[]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CONST_GET, 1),
			instr.New(instr.CALL),
		},
		program.WithConstants(array, sumFn),
	)
}

func patchBranch(code []instr.Instruction, branch int, target int) {
	start := len(instr.Marshal(code[:branch]))
	end := start + len(code[branch])
	dst := len(instr.Marshal(code[:target]))
	offset := int16(dst - end)
	code[branch].SetOperand(0, uint64(uint16(offset)))
}
