package interp_test

import (
	"context"
	"runtime"
	"strings"
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

// outerInnerProgram calls outer(1) warm times; outer(n) returns inner(n)
// through a CALL of a function that never separately crosses the threshold,
// so once outer runs natively it always takes ExitCall.
func outerInnerProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	inner := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}}).
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)).MustBuild()
	outer := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}}).
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL), instr.New(instr.RETURN)).MustBuild()

	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(inner, outer))
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

	t.Run("reports a clear error when native code needs deoptimization", func(t *testing.T) {
		native(t)
		prog := outerInnerProgram(t, 1000)

		require.Eventually(t, func() bool {
			vm := interp.New(prog, interp.WithThreshold(0))
			defer vm.Close()
			err := vm.Run(context.Background())
			return err != nil && strings.Contains(err.Error(), "requires deoptimization")
		}, 5*time.Second, time.Millisecond)
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
