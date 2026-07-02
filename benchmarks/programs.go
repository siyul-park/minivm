// Package bench provides reusable benchmark programs for minivm.
//
// This package is a separate Go module so that the main minivm packages carry
// no dependency on benchmark infrastructure.
package bench

import (
	"math"

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

// batchTreeEvaluation returns a batch-prediction-shaped program: a top-level
// accumulator calls many tiny tree-score functions over one stable feature row.
func batchTreeEvaluation(trees int) (*program.Program, types.Value) {
	features := []int32{15, 35, 75, 125, 175, 225, 275, 325}
	fns := make([]types.Value, 0, trees)
	var want int32

	for tree := range trees {
		feature := tree % len(features)
		weight := int32(tree%7 + 1)
		bias := -int32(tree%5 + 1)
		want += features[feature]*weight + bias

		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params:  []types.Type{types.TypeI32},
			Returns: []types.Type{types.TypeI32},
		})
		fn.Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, uint64(uint32(weight))),
			instr.New(instr.I32_MUL),
			instr.New(instr.I32_CONST, uint64(uint32(bias))),
			instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
		)
		fns = append(fns, fn.MustBuild())
	}

	b := program.NewBuilder()
	b.Emit(instr.I32_CONST, 0)
	for tree, fn := range fns {
		feature := tree % len(features)
		b.Emit(instr.I32_CONST, uint64(uint32(features[feature]))).
			ConstGet(fn).
			Emit(instr.CALL).
			Emit(instr.I32_ADD)
	}
	return build(b), types.I32(want)
}

// branchyBatchTreeEvaluation returns a mutable-row LightGBM-shaped program:
// a top-level eval function calls many tiny branchy tree functions over one
// shared f64 feature row.
func branchyBatchTreeEvaluation(trees int) (*program.Program, []float64, func([]float64) float64) {
	row := make([]float64, 8)
	features := make([][3]int, trees)
	thresholds := make([][3]float64, trees)
	leaves := make([][4]float64, trees)
	constants := make([]types.Value, 0, trees+2)
	constants = append(constants, types.TypedArray[float64](row))

	for tree := range trees {
		features[tree] = [3]int{
			tree % len(row),
			(tree*3 + 1) % len(row),
			(tree*5 + 2) % len(row),
		}
		thresholds[tree] = [3]float64{
			0.35 + float64(tree%4)*0.1,
			0.25 + float64((tree+1)%4)*0.1,
			0.45 + float64((tree+2)%4)*0.1,
		}
		base := float64(tree+1) * 0.01
		leaves[tree] = [4]float64{base, -base * 0.5, base * 1.5, -base}

		fn := types.NewFunctionBuilder(nil).
			WithParams(types.TypeF64Array).
			WithReturns(types.TypeF64)
		left := fn.Label()
		leftLow := fn.Label()
		rightLow := fn.Label()
		fn.Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, uint64(uint32(features[tree][0])))).
			Emit(instr.New(instr.ARRAY_GET)).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(thresholds[tree][0]))).
			Emit(instr.New(instr.F64_LE)).
			BrIf(left).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, uint64(uint32(features[tree][2])))).
			Emit(instr.New(instr.ARRAY_GET)).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(thresholds[tree][2]))).
			Emit(instr.New(instr.F64_LE)).
			BrIf(rightLow).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(leaves[tree][2]))).
			Emit(instr.New(instr.RETURN)).
			Bind(rightLow).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(leaves[tree][3]))).
			Emit(instr.New(instr.RETURN)).
			Bind(left).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.I32_CONST, uint64(uint32(features[tree][1])))).
			Emit(instr.New(instr.ARRAY_GET)).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(thresholds[tree][1]))).
			Emit(instr.New(instr.F64_LE)).
			BrIf(leftLow).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(leaves[tree][0]))).
			Emit(instr.New(instr.RETURN)).
			Bind(leftLow).
			Emit(instr.New(instr.F64_CONST, math.Float64bits(leaves[tree][1]))).
			Emit(instr.New(instr.RETURN))
		constants = append(constants, fn.MustBuild())
	}

	eval := types.NewFunctionBuilder(nil).
		WithParams(types.TypeF64Array).
		WithReturns(types.TypeF64).
		WithLocals(types.TypeF64)
	eval.Emit(instr.New(instr.F64_CONST, 0)).
		Emit(instr.New(instr.LOCAL_SET, 1))
	for tree := range trees {
		eval.Emit(instr.New(instr.LOCAL_GET, 1)).
			Emit(instr.New(instr.LOCAL_GET, 0)).
			Emit(instr.New(instr.CONST_GET, uint64(tree+1))).
			Emit(instr.New(instr.CALL)).
			Emit(instr.New(instr.F64_ADD)).
			Emit(instr.New(instr.LOCAL_SET, 1))
	}
	eval.Emit(instr.New(instr.LOCAL_GET, 1)).
		Emit(instr.New(instr.RETURN))
	constants = append(constants, eval.MustBuild())

	score := func(row []float64) float64 {
		var out float64
		for tree := range trees {
			switch {
			case row[features[tree][0]] <= thresholds[tree][0] && row[features[tree][1]] <= thresholds[tree][1]:
				out += leaves[tree][1]
			case row[features[tree][0]] <= thresholds[tree][0]:
				out += leaves[tree][0]
			case row[features[tree][2]] <= thresholds[tree][2]:
				out += leaves[tree][3]
			default:
				out += leaves[tree][2]
			}
		}
		return out
	}

	return program.New([]instr.Instruction{
		instr.New(instr.CONST_GET, 0),
		instr.New(instr.CONST_GET, uint64(len(constants)-1)),
		instr.New(instr.CALL),
	}, program.WithConstants(constants...)), row, score
}

func build(b *program.Builder) *program.Program {
	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}
