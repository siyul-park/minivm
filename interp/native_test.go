package interp_test

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

// fibFunction is fib(n) = n < 2 ? n : fib(n-1) + fib(n-2), calling itself
// through constant 0.
func fibFunction() *types.Function {
	b := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
	small := b.Label()
	b.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_LT_S)).BrIf(small)
	b.Emit(
		instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_SUB), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		instr.New(instr.I32_ADD), instr.New(instr.RETURN),
	)
	b.Bind(small).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN))
	return b.MustBuild()
}

// fibCallsProgram calls fib(15) calls times, module locals [0] the counter
// and [1] the running sum, and leaves the sum on the stack.
func fibCallsProgram(t *testing.T, calls int) *program.Program {
	t.Helper()
	fib := fibFunction()
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(calls)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, 15).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 1)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeI32), program.WithConstants(fib))
}

// sumFunction is sum(n) = 0 + 1 + ... + n-1 over one parameter and two locals.
func sumFunction(t *testing.T) *types.Function {
	t.Helper()
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 2).Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	return &types.Function{
		Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
		Locals: []types.Type{types.TypeI32, types.TypeI32},
		Code:   instr.Marshal(code),
	}
}

// sumWarmProgram calls sum(1) warm times to give an async compile time to
// finish, then calls sum(n) once and leaves its result on the stack.
func sumWarmProgram(t *testing.T, warm, n int) *program.Program {
	t.Helper()
	sum := sumFunction(t)
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.I32_CONST, uint64(n)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(sum))
}

// sumTryProgram is sumWarmProgram whose final call sits in a Try region that
// catches any trap: a cancellation must still escape it.
func sumTryProgram(t *testing.T, warm, n int) *program.Program {
	t.Helper()
	sum := sumFunction(t)
	b := instr.NewBuilder()
	loop, done, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done)
	b.Bind(start).Emit(instr.I32_CONST, uint64(n)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	b.Bind(end)
	b.Bind(catch).Emit(instr.ERROR_CODE)
	b.Try(start, end, catch, 1)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(sum), program.WithHandlers(b.Handlers()...))
}

// dropFunction takes one string parameter and returns 42 without touching
// it, so RETURN releases the parameter slot without the function body ever
// bridging: a fresh, singly-owned string argument dies exactly there.
func dropFunction(t *testing.T) *types.Function {
	t.Helper()
	b := instr.NewBuilder()
	b.Emit(instr.I32_CONST, 42).Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	return &types.Function{
		Typ:  &types.FunctionType{Params: []types.Type{types.TypeString}, Returns: []types.Type{types.TypeI32}},
		Code: instr.Marshal(code),
	}
}

// outerInnerProgram calls outer(id) warm times, dropping the result each
// time, then calls it once more keeping the result: outer(s) returns
// inner(s) through a CALL of a function that never separately crosses the
// threshold, so once outer runs natively its own CALL always takes ExitCall
// through a zero natives entry.
func outerInnerProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	inner := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString}, Returns: []types.Type{types.TypeString}}).
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)).MustBuild()
	outer := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString}, Returns: []types.Type{types.TypeString}}).
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL), instr.New(instr.RETURN)).MustBuild()

	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(inner, outer, types.String("chain")))
}

// divFunction returns 10 / n (i32), so a zero divisor traps inside the
// function body threaded, and inside native code as an ExitDeopt at the
// I32_DIV_S check.
func divFunction(t *testing.T) *types.Function {
	t.Helper()
	b := instr.NewBuilder()
	b.Emit(instr.I32_CONST, 10).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_DIV_S).Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	return &types.Function{
		Typ:  &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
		Code: instr.Marshal(code),
	}
}

// divCaughtProgram warms divFunction with a nonzero divisor warm times, then
// calls it once more with a zero divisor inside a module-level Try that
// catches the trap and leaves the caught exception's error code on the
// stack. The one local (the warmup counter) is the Try region's entry depth.
func divCaughtProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	fn := divFunction(t)
	b := instr.NewBuilder()
	loop, done, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done)
	b.Bind(start).Emit(instr.I32_CONST, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	b.Bind(end)
	b.Bind(catch).Emit(instr.ERROR_CODE)
	b.Try(start, end, catch, 1)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fn), program.WithHandlers(b.Handlers()...))
}

// divUncaughtProgram is divCaughtProgram without a handler, so the same trap
// escapes Run as an error instead of landing on a guest catch.
func divUncaughtProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	fn := divFunction(t)
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.I32_CONST, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fn))
}

// concatFunction returns the STRING_CONCAT of its two parameters. arm64's
// machine lowers no string opcode, so a compiled call to it always bridges:
// the interpreter performs STRING_CONCAT itself once native code exits.
func concatFunction(t *testing.T) *types.Function {
	t.Helper()
	b := instr.NewBuilder()
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.STRING_CONCAT).Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	return &types.Function{
		Typ:  &types.FunctionType{Params: []types.Type{types.TypeString, types.TypeString}, Returns: []types.Type{types.TypeString}},
		Code: instr.Marshal(code),
	}
}

// concatProgram warms concatFunction with two throwaway strings warm times,
// then calls it once more and leaves its result on the stack.
func concatProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	fn := concatFunction(t)
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32),
		program.WithConstants(fn, types.String("bridge-"), types.String("concat")))
}

// dynamicConcatProgram exercises native.call's dynamic-CALL entry (release
// true): the callee reference is seeded into local 1 once, then loaded with
// LOCAL_GET (which retains) at each call site instead of an immediately
// preceding CONST_GET, so the threader's CONST_GET;CALL fusion never applies
// and every entry into concatFunction's native code goes through the
// non-fused path the fused-only concatProgram never exercises. Warms with
// two throwaway strings warm times, then calls once more and leaves the
// result on the stack.
func dynamicConcatProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	fn := concatFunction(t)
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Emit(instr.CONST_GET, 0).Emit(instr.LOCAL_SET, 1)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeAny),
		program.WithConstants(fn, types.String("dyn-"), types.String("call")))
}

// fibOverflowProgram warms fib with shallow calls, deep enough for recursion
// but shallow enough to fit WithFrame(limit), then calls fib(n) once with n
// deep enough to exceed limit: native recursion always exceeds ctx.Limit
// before the threaded recursion it continues into would itself overflow, so
// deoptimizing an ExitCall there must reach the identical frame count and
// error a pure threaded run reaches.
func fibOverflowProgram(t *testing.T, warm, n int) *program.Program {
	t.Helper()
	fib := fibFunction()
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, 5).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.I32_CONST, uint64(n)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fib))
}

// wideI64Function returns n << 50. A literal outside the inline-boxable
// range is not itself translatable (transform.Translate rejects it), so the
// width has to arise from computation: for n=1 the shift's result exceeds
// the inline range, and a wide i64 result deopts at its own RETURN, which
// this proves alongside the value.
func wideI64Function(t *testing.T) *types.Function {
	t.Helper()
	b := instr.NewBuilder()
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL).Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	return &types.Function{
		Typ:  &types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI64}},
		Code: instr.Marshal(code),
	}
}

// wideI64Program warms wideI64Function(1) warm times, dropping its result
// each time, then calls it once more and leaves the result on the stack.
func wideI64Program(t *testing.T, warm int) *program.Program {
	t.Helper()
	fn := wideI64Function(t)
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fn))
}

// divFailProgram warms divFunction with a nonzero divisor warm times, then
// calls it with a zero divisor fails times in a loop whose body is one Try
// region: every one of those native entries deoptimizes, so enough of them
// retire the address and it tiers up again from a fresh Baseline compile.
// Locals are [0]=warm counter, [1]=fail counter, so the Try region's entry
// depth (params + locals + live operands, per instr.Handler) is 2.
func divFailProgram(t *testing.T, warm, fails int) *program.Program {
	t.Helper()
	fn := divFunction(t)
	b := instr.NewBuilder()
	warmLoop, warmDone := b.Label(), b.Label()
	b.Bind(warmLoop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(warmDone)
	b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(warmLoop)
	b.Bind(warmDone)

	failLoop, failDone, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
	b.Bind(failLoop)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(fails)).Emit(instr.I32_GE_S).BrIf(failDone)
	b.Bind(start).Emit(instr.I32_CONST, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	b.Bind(end)
	b.Bind(catch).Emit(instr.ERROR_CODE).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Br(failLoop)
	b.Bind(failDone).Emit(instr.LOCAL_GET, 1)
	b.Try(start, end, catch, 2)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeI32),
		program.WithConstants(fn), program.WithHandlers(b.Handlers()...))
}

// nestedConcatOuterFunction calls concatFunction(a, b) through a constant
// callee, so a hot outer's native call enters concatFunction's native code
// directly, two native activations deep, before concatFunction's own
// STRING_CONCAT (arm64 lowers no string opcode) deopts them both; both then
// run to a normal RETURN threaded, unlike an error unwind.
func nestedConcatOuterFunction(t *testing.T) *types.Function {
	t.Helper()
	b := instr.NewBuilder()
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	return &types.Function{
		Typ:  &types.FunctionType{Params: []types.Type{types.TypeString, types.TypeString}, Returns: []types.Type{types.TypeString}},
		Code: instr.Marshal(code),
	}
}

// nestedConcatProgram warms only its caller (constant 1) directly, so every
// call to concatFunction (constant 0) goes through it: the early ones, while
// concatFunction is still uncompiled, deoptimize outer alone through
// ExitCall and replay threaded (which still counts toward concatFunction's
// own threshold); once concatFunction compiles, outer's later native calls
// enter it directly (a borrowed constant callee) before its own
// STRING_CONCAT deopts both activations. The final call leaves its result
// on the stack.
func nestedConcatProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	inner := concatFunction(t)
	outer := nestedConcatOuterFunction(t)
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 3).Emit(instr.CONST_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 3).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32),
		program.WithConstants(inner, outer, types.String("deep-"), types.String("concat")))
}

func TestWithThreshold(t *testing.T) {
	t.Run("compiles a hot recursive function and enters its native code", func(t *testing.T) {
		native(t)
		prog := fibCallsProgram(t, 20)
		want := runProgram(t, prog)

		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			require.NoError(t, vm.Run(context.Background()))
			result, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, result)
			vm.Flush()
			entries, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return entries > 0
		}, 2*time.Second, time.Millisecond)
	})

	t.Run("a loop function reaches a safepoint and refills its budget", func(t *testing.T) {
		native(t)
		prog := sumWarmProgram(t, 1000, 200000)
		want := runProgram(t, prog)

		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			require.NoError(t, vm.Run(context.Background()))
			result, err := vm.Pop()
			require.NoError(t, err)
			if result != want {
				return false
			}
			vm.Flush()
			exits, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "safepoint"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
	})

	t.Run("releases a dying ref parameter through ExitRelease", func(t *testing.T) {
		native(t)
		fn := dropFunction(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 500).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.STRING_CONCAT)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.STRING_CONCAT)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32),
			program.WithConstants(fn, types.String("native-"), types.String("release")))

		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			require.NoError(t, vm.Run(context.Background()))
			survivor, err := vm.PopBoxed()
			require.NoError(t, err)
			count, err := vm.RefCount(survivor.Ref())
			require.NoError(t, err)
			require.Equal(t, 1, count)
			vm.Flush()
			released, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "release"})
			return released > 0
		}, 5*time.Second, time.Millisecond)
	})

	t.Run("a native division by zero is caught by a guest handler, matching threaded", func(t *testing.T) {
		native(t)
		prog := divCaughtProgram(t, 1000)
		want := runProgram(t, prog)

		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			require.NoError(t, vm.Run(context.Background()))
			result, err := vm.Pop()
			require.NoError(t, err)
			if result != want {
				return false
			}
			vm.Flush()
			exits, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
	})

	t.Run("an uncaught native division by zero reports the same stack trace as threaded", func(t *testing.T) {
		native(t)
		prog := divUncaughtProgram(t, 1000)
		wantErr := runProgramErr(t, prog)
		require.Error(t, wantErr)

		require.Eventually(t, func() bool {
			vm := interp.New(prog, interp.WithThreshold(0))
			defer vm.Close()
			err := vm.Run(context.Background())
			return err != nil && errorsEqual(err, wantErr)
		}, 5*time.Second, time.Millisecond)
	})

	t.Run("a bridged STRING_CONCAT inside compiled code matches threaded, including RefCount", func(t *testing.T) {
		native(t)
		prog := concatProgram(t, 1000)
		wantValue, wantCount := runProgramString(t, prog)

		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			require.NoError(t, vm.Run(context.Background()))
			value, count, err := popString(vm)
			require.NoError(t, err)
			if value != wantValue || count != wantCount {
				return false
			}
			vm.Flush()
			exits, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
	})

	t.Run("a bridged call entered dynamically (unfused) matches threaded, including RefCount", func(t *testing.T) {
		native(t)
		prog := dynamicConcatProgram(t, 1000)
		wantValue, wantCount := runProgramString(t, prog)

		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			require.NoError(t, vm.Run(context.Background()))
			value, count, err := popString(vm)
			require.NoError(t, err)
			if value != wantValue || count != wantCount {
				return false
			}
			vm.Flush()
			exits, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
	})

	t.Run("recursion past a small frame limit deoptimizes an ExitCall, matching threaded", func(t *testing.T) {
		native(t)
		prog := fibOverflowProgram(t, 1000, 30)
		wantErr := runProgramErr(t, prog, interp.WithFrame(10))
		require.Error(t, wantErr)

		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithFrame(10), interp.WithProfiler(profiler))
			defer vm.Close()
			err := vm.Run(context.Background())
			if err == nil || !overflowEqual(err, wantErr) {
				return false
			}
			vm.Flush()
			exits, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
	})

	t.Run("a native call to an uncompiled function deoptimizes an ExitCall, matching threaded", func(t *testing.T) {
		native(t)
		prog := outerInnerProgram(t, 1000)
		wantValue, wantCount := runProgramString(t, prog)

		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			require.NoError(t, vm.Run(context.Background()))
			value, count, err := popString(vm)
			require.NoError(t, err)
			if value != wantValue || count != wantCount {
				return false
			}
			vm.Flush()
			exits, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
	})

	t.Run("a deopt two native activations deep matches threaded, including the borrowed callee's RefCount", func(t *testing.T) {
		native(t)
		prog := nestedConcatProgram(t, 20000)
		wantValue, wantCount := runProgramString(t, prog)

		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			require.NoError(t, vm.Run(context.Background()))
			value, count, err := popString(vm)
			require.NoError(t, err)
			if value != wantValue || count != wantCount {
				return false
			}
			// concatFunction (constant 0, address 1) is retained exactly
			// once by its caller's own call site and used only there, so it
			// is borrowed: the deopt that rebuilds both activations, which
			// then run to a normal RETURN threaded, must not leave it
			// over- or under-counted.
			innerCount, rcErr := vm.RefCount(1)
			require.NoError(t, rcErr)
			if innerCount != 1 {
				return false
			}
			vm.Flush()
			exits, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
	})

	t.Run("a borrowed callee's RefCount survives an ExitCall replay, matching threaded", func(t *testing.T) {
		native(t)
		prog := outerInnerProgram(t, 20000)
		wantValue, wantCount := runProgramString(t, prog)

		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			require.NoError(t, vm.Run(context.Background()))
			value, count, err := popString(vm)
			require.NoError(t, err)
			if value != wantValue || count != wantCount {
				return false
			}
			// inner (constant 0, address 1) is never itself warm enough to
			// compile, so outer's every native call to it replays threaded
			// through ExitCall; the replayed CALL releases what it adopts,
			// so the borrowed reference deopt hands it must be real.
			innerCount, rcErr := vm.RefCount(1)
			require.NoError(t, rcErr)
			if innerCount != 1 {
				return false
			}
			vm.Flush()
			exits, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
	})

	t.Run("a wide i64 result from native code matches threaded", func(t *testing.T) {
		native(t)
		prog := wideI64Program(t, 1000)
		want := runProgram(t, prog)

		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			require.NoError(t, vm.Run(context.Background()))
			result, err := vm.Pop()
			require.NoError(t, err)
			if result != want {
				return false
			}
			vm.Flush()
			exits, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
	})

	t.Run("a hot function tiers up to Optimized after enough native entries", func(t *testing.T) {
		native(t)
		prog := fibCallsProgram(t, 2000)
		want := runProgram(t, prog)

		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			require.NoError(t, vm.Run(context.Background()))
			result, err := vm.Pop()
			require.NoError(t, err)
			if result != want {
				return false
			}
			vm.Flush()
			compiles, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles > 0
		}, 5*time.Second, time.Millisecond)
	})

	t.Run("a function that deoptimizes every call is retired and stops entering native code", func(t *testing.T) {
		native(t)
		// warm is large enough (matching sumWarmProgram/sumTryProgram) that the
		// async Baseline compile reliably finishes during warmup, so every one
		// of fails' native entries deoptimizes: without retirement, every one
		// of them reaches native code and vm_jit_exits_total{kind=deopt} equals
		// fails exactly; retirement bounds it to a handful of refute-sized
		// cycles instead.
		const fails = 200
		prog := divFailProgram(t, 200_000, fails)
		want := runProgram(t, prog)

		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			require.NoError(t, vm.Run(context.Background()))
			result, err := vm.Pop()
			require.NoError(t, err)
			if result != want {
				return false
			}
			vm.Flush()
			deopts, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return deopts > 0 && deopts < fails/2
		}, 5*time.Second, time.Millisecond)
	})

	t.Run("a cancelled context during a native loop escapes guest handlers as the context error", func(t *testing.T) {
		native(t)
		// warm is large enough that the async Baseline compile reliably
		// finishes partway through the threaded warmup loop, so later
		// warmup calls and the huge final call both run natively; the huge
		// call's own loop then takes many safepoints, giving the cancellation
		// timer a wide native window to land in. The 1s delay is generous
		// enough to clear the threaded warmup loop even under -race, whose
		// instrumentation (unlike the native loop itself) slows it down.
		prog := sumTryProgram(t, 200_000, 2_000_000_000)

		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				time.Sleep(time.Second)
				cancel()
			}()
			err := vm.Run(ctx)
			if !errors.Is(err, context.Canceled) {
				return false
			}
			vm.Flush()
			exits, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "safepoint"})
			return exits > 0
		}, 20*time.Second, time.Second)
	})

	t.Run("a function allocated after construction runs interpreted", func(t *testing.T) {
		native(t)
		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 7).Emit(instr.RETURN)
		code, err := b.Assemble()
		require.NoError(t, err)
		fn := &types.Function{Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}}, Code: instr.Marshal(code)}
		b = instr.NewBuilder()
		b.Emit(instr.GLOBAL_GET, 0).Emit(instr.CALL)
		code, err = b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithGlobals(types.TypeAny))

		vm := interp.New(prog, interp.WithThreshold(0))
		defer vm.Close()
		addr, err := vm.Alloc(fn)
		require.NoError(t, err)
		require.NoError(t, vm.SetGlobal(0, types.BoxRef(addr)))
		require.NoError(t, vm.Run(context.Background()))
		result, err := vm.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(7), result)
	})

	t.Run("a wide i64 argument deopting at a borrowed call keeps the callee alive", func(t *testing.T) {
		native(t)
		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 1).Emit(instr.RETURN)
		code, err := b.Assemble()
		require.NoError(t, err)
		callee := &types.Function{Typ: &types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI32}}, Code: instr.Marshal(code)}
		b = instr.NewBuilder()
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
		code, err = b.Assemble()
		require.NoError(t, err)
		caller := &types.Function{Typ: &types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI32}}, Code: instr.Marshal(code)}
		b = instr.NewBuilder()
		warm, warmed := b.Label(), b.Label()
		b.Bind(warm)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 20000).Emit(instr.I32_GE_S).BrIf(warmed)
		b.Emit(instr.I64_CONST, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(warm)
		b.Bind(warmed).Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 20000).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I64_CONST, 0).Emit(instr.CONST_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
		b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 1).Emit(instr.CALL).Emit(instr.I32_ADD)
		code, err = b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(callee, caller))
		want := runProgram(t, prog)

		var result types.Value
		var rc int
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			if err = vm.Run(context.Background()); err != nil {
				return true
			}
			result, _ = vm.Pop()
			c, _ := vm.Const(0)
			rc, _ = vm.RefCount(c.Ref())
			vm.Flush()
			deopts, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return deopts > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, err)
		require.Equal(t, want, result)
		require.Equal(t, 1, rc)
	})

	t.Run("stays off by default: no vm_jit metrics are reported", func(t *testing.T) {
		profiler := prof.New()
		vm := interp.New(fibCallsProgram(t, 3), interp.WithProfiler(profiler))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		vm.Flush()

		for _, m := range profiler.Metrics() {
			require.NotContains(t, m.Name, "vm_jit_")
		}
	})
}

// native skips a case that runs native code off arm64.
func native(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native execution needs arm64")
	}
}

// runProgram runs prog threaded and returns the single value it leaves on
// the stack.
func runProgram(t *testing.T, prog *program.Program) types.Value {
	t.Helper()
	vm := interp.New(prog)
	defer vm.Close()
	require.NoError(t, vm.Run(context.Background()))
	v, err := vm.Pop()
	require.NoError(t, err)
	return v
}

// runProgramErr runs prog threaded with opts and returns Run's error.
func runProgramErr(t *testing.T, prog *program.Program, opts ...interp.Option) error {
	t.Helper()
	vm := interp.New(prog, opts...)
	defer vm.Close()
	return vm.Run(context.Background())
}

// runProgramString runs prog threaded and returns the single stack ref
// value it leaves, as its string content and live RefCount.
func runProgramString(t *testing.T, prog *program.Program) (string, int) {
	t.Helper()
	vm := interp.New(prog)
	defer vm.Close()
	require.NoError(t, vm.Run(context.Background()))
	value, count, err := popString(vm)
	require.NoError(t, err)
	return value, count
}

// popString pops vm's single stack value, returning its string content and
// live RefCount. It does not release the reference PopBoxed transfers, since
// the caller inspects RefCount before the VM closes.
func popString(vm *interp.Interpreter) (string, int, error) {
	boxed, err := vm.PopBoxed()
	if err != nil {
		return "", 0, err
	}
	loaded, err := vm.Load(boxed.Ref())
	if err != nil {
		return "", 0, err
	}
	s, ok := loaded.(types.String)
	if !ok {
		return "", 0, interp.ErrTypeMismatch
	}
	count, err := vm.RefCount(boxed.Ref())
	if err != nil {
		return "", 0, err
	}
	return string(s), count, nil
}

// errorsEqual reports whether got and want both carry an *interp.RuntimeError
// with the same cause and the same call stack.
func errorsEqual(got, want error) bool {
	var g, w *interp.RuntimeError
	return errors.As(got, &g) && errors.As(want, &w) && reflect.DeepEqual(g, w)
}

// overflowEqual reports whether got and want both carry an
// *interp.RuntimeError over the same ErrFrameOverflow with the same
// function at every stack level. It ignores the innermost frame's IP: a
// deoptimized frame runs exact code, so a trap at a fused CONST_GET;CALL
// call site (fib's own recursive call) resumes at CALL's own IP rather than
// the fused unit's, unlike the threaded baseline which never separates them.
func overflowEqual(got, want error) bool {
	var g, w *interp.RuntimeError
	if !errors.As(got, &g) || !errors.As(want, &w) {
		return false
	}
	if !errors.Is(g.Err, interp.ErrFrameOverflow) || !errors.Is(w.Err, interp.ErrFrameOverflow) {
		return false
	}
	if len(g.Frames) != len(w.Frames) {
		return false
	}
	for i := range g.Frames {
		if g.Frames[i].Func != w.Frames[i].Func {
			return false
		}
		if i > 0 && g.Frames[i].IP != w.Frames[i].IP {
			return false
		}
	}
	return true
}
