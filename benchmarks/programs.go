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
func Fib(n int32) *program.Program {
	fib := types.NewFunctionBuilder(&types.FunctionType{
		Params:  []types.Type{types.TypeI32},
		Returns: []types.Type{types.TypeI32},
	})
	base := fib.Label()
	fn := fib.
		// if n < 2 { return n }
		Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_LT_S),
		).BrIf(base).
		// return fib(n-1) + fib(n-2)
		Emit(
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
		).
		// base case: return n
		Bind(base).Emit(
		instr.New(instr.LOCAL_GET, 0),
		instr.New(instr.RETURN),
	).MustBuild()

	b := program.NewBuilder()
	b.Emit(instr.I32_CONST, uint64(n)).ConstGet(fn).Emit(instr.CALL)
	return build(b)
}

// indirectFibViaLocal returns a recursive fibonacci program where each
// recursive call flows the function ref through a local before CALL.
func indirectFibViaLocal(n int32) *program.Program {
	fib := types.NewFunctionBuilder(&types.FunctionType{
		Params:  []types.Type{types.TypeI32},
		Returns: []types.Type{types.TypeI32},
	}).WithLocals(types.TypeRef)
	base := fib.Label()
	fn := fib.
		Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_LT_S),
		).BrIf(base).
		Emit(
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
		).
		Bind(base).Emit(
		instr.New(instr.LOCAL_GET, 0),
		instr.New(instr.RETURN),
	).MustBuild()

	b := program.NewBuilder()
	b.Emit(instr.I32_CONST, uint64(n)).ConstGet(fn).Emit(instr.CALL)
	return build(b)
}

// closureCounter returns a program that creates one closure and runs a hot
// upvalue increment loop in the closure body.
func closureCounter(iterations int32) *program.Program {
	body := types.NewFunctionBuilder(&types.FunctionType{
		Returns: []types.Type{types.TypeI32},
	}).WithLocals(types.TypeI32).WithCaptures(types.TypeI32)
	loop := body.Label()
	fn := body.
		Emit(
			instr.New(instr.I32_CONST, uint64(iterations)),
			instr.New(instr.LOCAL_SET, 0),
		).
		Bind(loop).Emit(
		instr.New(instr.UPVAL_GET, 0),
		instr.New(instr.I32_CONST, 1),
		instr.New(instr.I32_ADD),
		instr.New(instr.UPVAL_SET, 0),
		instr.New(instr.LOCAL_GET, 0),
		instr.New(instr.I32_CONST, 1),
		instr.New(instr.I32_SUB),
		instr.New(instr.LOCAL_TEE, 0),
	).BrIf(loop).
		Emit(
			instr.New(instr.UPVAL_GET, 0),
			instr.New(instr.RETURN),
		).MustBuild()

	b := program.NewBuilder()
	b.Emit(instr.I32_CONST, 0).ConstGet(fn).Emit(instr.CLOSURE_NEW).Emit(instr.CALL)
	return build(b)
}

// typedArraySum returns a program that passes a rooted []i32 array into a hot
// function and sums it through ARRAY_LEN/ARRAY_GET.
func typedArraySum(size int32) *program.Program {
	array := make(types.TypedArray[int32], size)
	for j := range array {
		array[j] = int32(j + 1)
	}

	sum := types.NewFunctionBuilder(&types.FunctionType{
		Params:  []types.Type{types.TypeRef},
		Returns: []types.Type{types.TypeI32},
	}).WithLocals(types.TypeI32, types.TypeI32, types.TypeI32)
	loop := sum.Label()
	end := sum.Label()
	fn := sum.
		Emit(
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.LOCAL_SET, 2),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.ARRAY_LEN),
			instr.New(instr.LOCAL_SET, 3),
		).
		Bind(loop).Emit(
		instr.New(instr.LOCAL_GET, 2),
		instr.New(instr.LOCAL_GET, 3),
		instr.New(instr.I32_GE_S),
	).BrIf(end).
		Emit(
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
		).Br(loop).
		Bind(end).Emit(
		instr.New(instr.LOCAL_GET, 1),
		instr.New(instr.RETURN),
	).MustBuild()

	b := program.NewBuilder()
	b.ConstGet(array).ConstGet(fn).Emit(instr.CALL)
	return build(b)
}

func build(b *program.Builder) *program.Program {
	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}
