package interp_test

import (
	"context"
	"math"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestWithThresholdCallouts(t *testing.T) {
	t.Run("reports threaded's heap exhaustion from a callout with threaded's counts", func(t *testing.T) {
		native(t)
		// Each fresh one-element []any holds the previous one, so every array
		// stays live until the heap limit; an inner count to 8 gives each
		// callout enough native work to pay for it.
		b := instr.NewBuilder()
		loop, inner, next := b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, 0).Emit(instr.DUP)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_GET, 0).Emit(instr.ARRAY_SET)
		b.Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(inner)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 8).Emit(instr.I32_GE_S).BrIf(next)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(inner)
		b.Bind(next)
		b.Br(loop)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeAny, types.TypeI32), program.WithTypes(types.NewArrayType(types.TypeAny)))

		threaded := interp.New(prog, interp.WithThreshold(-1), interp.WithHeapLimit(300))
		defer threaded.Close()
		wantErr := threaded.Run(context.Background())
		require.ErrorIs(t, wantErr, interp.ErrHeapExhausted)
		// Address 0 is null, whose count is no reference count: threaded
		// retainBox counts it up, releaseBox never down, native code neither.
		wantCounts := make([]int, threaded.HeapLen()-1)
		for j := range wantCounts {
			wantCounts[j], _ = threaded.RefCount(j + 1)
		}

		var runErr error
		var bridges float64
		var counts []int
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler), interp.WithHeapLimit(300))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			counts = make([]int, vm.HeapLen()-1)
			for j := range counts {
				counts[j], _ = vm.RefCount(j + 1)
			}
			vm.Reset()
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return bridges > 0
		})
		require.Equal(t, wantErr, runErr)
		require.Equal(t, wantCounts, counts)
	})

	t.Run("treats NaN as unordered in float comparisons", func(t *testing.T) {
		native(t)
		const warm = 1000
		nan := uint64(math.Float32bits(float32(math.NaN())))
		b := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}})
		for _, op := range []instr.Opcode{instr.F32_EQ, instr.F32_NE, instr.F32_LE} {
			b.Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 0))
			b.Emit(instr.New(instr.F32_CONST, nan), instr.New(instr.F32_CONST, nan), instr.New(op), instr.New(instr.SELECT))
			if op != instr.F32_EQ {
				b.Emit(instr.New(instr.I32_ADD))
			}
		}
		fn := b.Emit(instr.New(instr.RETURN)).MustBuild()

		module := instr.NewBuilder()
		loop, done := module.Label(), module.Label()
		module.Bind(loop)
		module.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, warm).Emit(instr.I32_GE_S).BrIf(done)
		module.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		module.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		module.Br(loop)
		module.Bind(done).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := module.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fn))
		want := runProgram(t, prog)
		require.Equal(t, types.I32(1), want)

		var runErr, popErr error
		var result types.Value
		var entries float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("catches a native division by zero in a guest handler", func(t *testing.T) {
		native(t)
		fn := divFunction(t)
		b := instr.NewBuilder()
		loop, done, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
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
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fn), program.WithHandlers(b.Handlers()...))
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var exits float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return exits > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("reports an uncaught division by zero's stack trace", func(t *testing.T) {
		native(t)
		fn := divFunction(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.I32_CONST, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fn))
		wantErr := runProgramErr(t, prog, interp.WithThreshold(-1))
		require.Error(t, wantErr)

		var gotErr error
		vm := interp.New(prog, interp.WithThreshold(1))
		defer vm.Close()
		poll(t, func() bool {
			gotErr = vm.Run(context.Background())
			vm.Reset()
			return gotErr != nil
		})
		require.Error(t, gotErr)
		require.True(t, errorsEqual(gotErr, wantErr))
	})

	t.Run("bridges STRING_CONCAT", func(t *testing.T) {
		native(t)
		fn := concatFunction(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32),
			program.WithConstants(fn, types.String("bridge-"), types.String("concat")))
		wantValue, wantCount := runProgramString(t, prog)

		var runErr, popErr error
		var value string
		var count int
		var exits float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return exits > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
	})

	t.Run("bridges STRUCT_NEW", func(t *testing.T) {
		native(t)
		record := node()
		b := instr.NewBuilder()
		loop, done, inner, innerDone := b.Label(), b.Label(), b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.STRUCT_NEW_DEFAULT, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(200_000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0)
		b.Emit(instr.CONST_GET, 0)
		b.Emit(instr.STRUCT_NEW, 0)
		b.Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(inner)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(innerDone)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Br(inner)
		b.Bind(innerDone)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.STRUCT_GET)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32, record, types.TypeI32), program.WithTypes(record),
			program.WithConstants(types.String("held")))
		wantValue, wantCount := runProgramString(t, prog)

		var runErr, popErr error
		var value string
		var count int
		var bridges, deopts float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return bridges > 1
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
		// Every STRUCT_NEW bridge resumes: none of them fall back to a deopt.
		require.Equal(t, float64(0), deopts)
	})

	t.Run("bridges ARRAY_NEW_DEFAULT", func(t *testing.T) {
		native(t)
		const length = 4
		elem := types.NewArrayType(types.TypeI32)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(200_000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, length).Emit(instr.ARRAY_NEW_DEFAULT, 0)
		b.Emit(instr.ARRAY_LEN)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 1)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeI32), program.WithTypes(elem))
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var bridges, deopts float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return bridges > 1
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
		require.Equal(t, float64(0), deopts)
	})

	t.Run("declines a trapping ARRAY_NEW_DEFAULT", func(t *testing.T) {
		native(t)
		elem := types.NewArrayType(types.TypeI32)
		b := instr.NewBuilder()
		loop, done, bad, length, inner, innerDone := b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(200_000)+1).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(200_000)).Emit(instr.I32_EQ).BrIf(bad)
		b.Emit(instr.I32_CONST, 1).Br(length)
		b.Bind(bad).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_SUB)
		b.Bind(length)
		b.Emit(instr.ARRAY_NEW_DEFAULT, 0).Emit(instr.ARRAY_LEN).Emit(instr.DROP)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(inner)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(innerDone)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(inner)
		b.Bind(innerDone)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeI32), program.WithTypes(elem))
		wantErr := runProgramErr(t, prog, interp.WithThreshold(-1))
		require.Error(t, wantErr)

		// A declined bridge's own exit is still kind=bridge (the metric
		// names the exit, not its outcome), so only error/stack-trace parity
		// with threaded execution proves native code correctly handed the
		// trapping instruction back instead of silently accepting it.
		var gotErr error
		var bridges float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			gotErr = vm.Run(context.Background())
			if gotErr == nil {
				return false
			}
			vm.Reset()
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return bridges > 1
		})
		require.Error(t, gotErr)
		require.True(t, errorsEqual(gotErr, wantErr))
	})

	t.Run("bridges string ops", func(t *testing.T) {
		native(t)
		prog := texts(t, 20_000)
		threaded := interp.New(prog, interp.WithThreshold(-1))
		require.NoError(t, threaded.Run(context.Background()))
		wantValue, wantCount, err := popString(threaded)
		require.NoError(t, err)
		ab, err := threaded.Const(1)
		require.NoError(t, err)
		wantConst, err := threaded.RefCount(ab.Ref())
		require.NoError(t, err)
		require.NoError(t, threaded.Close())

		var runErr, popErr, constErr error
		var value string
		var count, constCount int
		var bridges, entries float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			constCount, constErr = vm.RefCount(ab.Ref())
			vm.Reset()
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return bridges > entries
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, constErr)
		// An entry that declined its first bridge could not reach a second one.
		require.Greater(t, bridges, entries)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
		require.Equal(t, wantConst, constCount)
	})

	t.Run("declines a string op that exhausts the heap", func(t *testing.T) {
		native(t)
		const limit = 20_000
		prog := texts(t, 2*limit)
		threaded := interp.New(prog, interp.WithHeapLimit(limit), interp.WithThreshold(-1))
		wantErr := threaded.Run(context.Background())
		require.ErrorIs(t, wantErr, interp.ErrHeapExhausted)
		ab, err := threaded.Const(1)
		require.NoError(t, err)
		wantConst, err := threaded.RefCount(ab.Ref())
		require.NoError(t, err)
		require.NoError(t, threaded.Close())

		var gotErr, constErr error
		var constCount int
		var bridges, entries float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithHeapLimit(limit), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			gotErr = vm.Run(context.Background())
			constCount, constErr = vm.RefCount(ab.Ref())
			vm.Reset()
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return bridges > entries
		})
		require.Greater(t, bridges, entries)
		require.True(t, errorsEqual(gotErr, wantErr), "got %v, want %v", gotErr, wantErr)
		require.NoError(t, constErr)
		require.Equal(t, wantConst, constCount)
	})

	t.Run("bridges unlowered operators in a loop", func(t *testing.T) {
		native(t)
		dict, list := types.NewMapType(types.TypeI32, types.TypeI32), types.NewArrayType(types.TypeI32)
		wide, named := types.NewMapType(types.TypeI32, types.TypeI64), types.NewMapType(types.TypeString, types.TypeI32)
		b := instr.NewBuilder()
		loop, done, first, firstDone, second, secondDone, third, thirdDone := b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.MAP_NEW_DEFAULT, 0).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.I32_CONST, 0).Emit(instr.MAP_NEW_DEFAULT, 3).Emit(instr.LOCAL_SET, 5)
		b.Emit(instr.I32_CONST, 0).Emit(instr.MAP_NEW_DEFAULT, 4).Emit(instr.LOCAL_SET, 6)
		b.Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_NEW_DEFAULT, 1).Emit(instr.LOCAL_SET, 3)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 300).Emit(instr.I32_GE_S).BrIf(done)
		// Each group of bridges follows an inner loop's native work, enough
		// to pay for the bridges (jit.Ledger).
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(first)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 16).Emit(instr.I32_GE_S).BrIf(firstDone)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).Br(first)
		b.Bind(firstDone)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_SET)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_GET)
		b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_APPEND).Emit(instr.LOCAL_SET, 3)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CONST_GET, 1).Emit(instr.STRING_EQ)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_LOOKUP).Emit(instr.I32_ADD)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_SLICE).Emit(instr.ARRAY_LEN)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(second)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 16).Emit(instr.I32_GE_S).BrIf(secondDone)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).Br(second)
		b.Bind(secondDone)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_FILL)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_COPY)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_DELETE)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 7).Emit(instr.ERROR_NEW).Emit(instr.ERROR_CODE)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.CONST_GET, 0).Emit(instr.REF_TEST, 2)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 3).Emit(instr.REF_EQ)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_DELETE)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(third)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 16).Emit(instr.I32_GE_S).BrIf(thirdDone)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).Br(third)
		b.Bind(thirdDone)
		// A wide i64 crosses the bridge boxed on the heap both ways.
		b.Emit(instr.LOCAL_GET, 5).Emit(instr.LOCAL_GET, 0).Emit(instr.I64_CONST, 1<<60).Emit(instr.MAP_SET)
		b.Emit(instr.LOCAL_GET, 5).Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_GET)
		b.Emit(instr.I64_CONST, 58).Emit(instr.I64_SHR_U).Emit(instr.I64_TO_I32)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 5).Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_DELETE)
		// map.delete adopts its key: native code hands it the reference.
		b.Emit(instr.LOCAL_GET, 6).Emit(instr.CONST_GET, 0).Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_SET)
		b.Emit(instr.LOCAL_GET, 6).Emit(instr.CONST_GET, 0).Emit(instr.MAP_DELETE)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 4).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_APPEND)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code,
			program.WithLocals(types.TypeI32, types.TypeI32, dict, list, types.TypeI32, wide, named),
			program.WithTypes(dict, list, types.TypeString, wide, named),
			program.WithConstants(types.String("x"), types.String("y")))

		threaded := interp.New(prog, interp.WithThreshold(-1))
		require.NoError(t, threaded.Run(context.Background()))
		boxed, err := threaded.PopBoxed()
		require.NoError(t, err)
		want, err := threaded.Load(boxed.Ref())
		require.NoError(t, err)
		wantCount, err := threaded.RefCount(boxed.Ref())
		require.NoError(t, err)
		x, err := threaded.Const(0)
		require.NoError(t, err)
		wantConst, err := threaded.RefCount(x.Ref())
		require.NoError(t, err)
		require.NoError(t, threaded.Close())

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name, key, value string) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, prof.Label{Key: key, Value: value})
			return v
		}

		var runErr error
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || metric("vm_jit_entries_total", "tier", "optimized") > 0
		})
		require.NoError(t, runErr)

		entries := metric("vm_jit_entries_total", "tier", "optimized")
		bridges := metric("vm_jit_exits_total", "kind", "bridge")
		for range 8 {
			require.NoError(t, vm.Run(context.Background()))
			boxed, err := vm.PopBoxed()
			require.NoError(t, err)
			got, err := vm.Load(boxed.Ref())
			require.NoError(t, err)
			count, err := vm.RefCount(boxed.Ref())
			require.NoError(t, err)
			constCount, err := vm.RefCount(x.Ref())
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.Equal(t, wantCount, count)
			require.Equal(t, wantConst, constCount)
			vm.Reset()
		}
		require.Greater(t, metric("vm_jit_entries_total", "tier", "optimized"), entries)
		require.Greater(t, metric("vm_jit_exits_total", "kind", "bridge"), bridges)
		require.Zero(t, metric("vm_jit_exits_total", "kind", "deopt"))
	})

	t.Run("selects between two refs", func(t *testing.T) {
		native(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 300).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CONST_GET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_AND)
		b.Emit(instr.SELECT).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(types.String("x"), types.String("y")))

		threaded := interp.New(prog, interp.WithThreshold(-1))
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		x, err := threaded.Const(0)
		require.NoError(t, err)
		y, err := threaded.Const(1)
		require.NoError(t, err)
		wantX, err := threaded.RefCount(x.Ref())
		require.NoError(t, err)
		wantY, err := threaded.RefCount(y.Ref())
		require.NoError(t, err)
		require.NoError(t, threaded.Close())

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name, key, value string) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, prof.Label{Key: key, Value: value})
			return v
		}

		var runErr error
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || metric("vm_jit_entries_total", "tier", "optimized") > 0
		})
		require.NoError(t, runErr)

		for range 8 {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			xCount, err := vm.RefCount(x.Ref())
			require.NoError(t, err)
			yCount, err := vm.RefCount(y.Ref())
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.Equal(t, wantX, xCount)
			require.Equal(t, wantY, yCount)
			vm.Reset()
		}
		require.Greater(t, metric("vm_jit_entries_total", "tier", "optimized"), 0.0)
	})

	t.Run("keeps a callee native while its native calls amortize bridges", func(t *testing.T) {
		native(t)
		// The loop adds f(i) for i < n. f(0) bridges three string ops with no
		// native work between them; every other f(i) loops four times first,
		// amortizing its bridges.
		const n = 2000
		fb := instr.NewBuilder()
		work, loop, done := fb.Label(), fb.Label(), fb.Label()
		fb.Emit(instr.LOCAL_GET, 0).BrIf(work)
		fb.Emit(instr.CONST_GET, 1).Emit(instr.STRING_ENCODE_UTF32).Emit(instr.STRING_NEW_UTF32).Emit(instr.STRING_LEN).Emit(instr.RETURN)
		fb.Bind(work).Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		fb.Bind(loop)
		fb.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(done)
		fb.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		fb.Br(loop)
		fb.Bind(done).Emit(instr.CONST_GET, 1).Emit(instr.STRING_ENCODE_UTF32).Emit(instr.STRING_NEW_UTF32).Emit(instr.STRING_LEN).Emit(instr.RETURN)
		fcode, err := fb.Assemble()
		require.NoError(t, err)
		f := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeI32},
			Code:   instr.Marshal(fcode),
		}

		b := instr.NewBuilder()
		header, exit, inner, innerDone := b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(header)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(exit)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(inner)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(innerDone)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Br(inner)
		b.Bind(innerDone)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(header)
		b.Bind(exit).Emit(instr.LOCAL_GET, 1)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeI32, types.TypeI32), program.WithConstants(f, types.String("ab")))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()

		// f(0) runs threaded-entered at most once per Run; enough Runs pass
		// for its bridges to reach the retirement limit several times over.
		const runs = 16
		var got types.Value
		var runErr, popErr error
		var round int
		var entries, prior float64
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			round++
			total, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			entries, prior = total-prior, total
			return round >= runs && entries < n/2
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		require.GreaterOrEqual(t, round, runs)
		// A retired f leaves the loop's native call nothing to enter, so the
		// loop runs threaded and enters f's OSR code for nearly every f(i).
		require.Less(t, entries, float64(n/2))
	})

	t.Run("releases a ref slot's old value on store", func(t *testing.T) {
		native(t)
		const n = 200_000
		prog := store(t, n)

		threaded := interp.New(store(t, n), interp.WithThreshold(-1))
		require.NoError(t, threaded.Run(context.Background()))
		wantConst, err := threaded.Const(0)
		require.NoError(t, err)
		want, err := threaded.RefCount(wantConst.Ref())
		require.NoError(t, err)
		require.NoError(t, threaded.Close())

		vm := interp.New(prog, interp.WithThreshold(1))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		gotConst, err := vm.Const(0)
		require.NoError(t, err)
		got, err := vm.RefCount(gotConst.Ref())
		require.NoError(t, err)

		require.Equal(t, want, got)
	})

	t.Run("bridges a dynamically entered call", func(t *testing.T) {
		native(t)
		fn := concatFunction(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.CONST_GET, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeAny),
			program.WithConstants(fn, types.String("dyn-"), types.String("call")))
		wantValue, wantCount := runProgramString(t, prog)

		var runErr, popErr error
		var value string
		var count int
		var exits float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return exits > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
	})

	t.Run("overflows a small frame limit inside a served call", func(t *testing.T) {
		native(t)
		fib := fibFunction()
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 5).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.I32_CONST, uint64(30)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fib))
		wantErr := runProgramErr(t, prog, interp.WithFrame(8), interp.WithThreshold(-1))
		require.Error(t, wantErr)

		var gotErr error
		var exits float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithFrame(8), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			gotErr = vm.Run(context.Background())
			if gotErr == nil {
				return false
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return exits > 0
		})
		require.Error(t, gotErr)
		// The innermost frame's IP is ignored: a deoptimized frame runs exact
		// code, so a trap at a fused CONST_GET;CALL call site (fib's own
		// recursive call) resumes at CALL's own IP rather than the fused
		// unit's, unlike the threaded baseline which never separates them.
		var got, want *interp.RuntimeError
		require.ErrorAs(t, gotErr, &got)
		require.ErrorAs(t, wantErr, &want)
		require.ErrorIs(t, got.Err, interp.ErrFrameOverflow)
		require.ErrorIs(t, want.Err, interp.ErrFrameOverflow)
		require.Len(t, got.Frames, len(want.Frames))
		for i := range got.Frames {
			require.Equal(t, want.Frames[i].Func, got.Frames[i].Func)
			if i > 0 {
				require.Equal(t, want.Frames[i].IP, got.Frames[i].IP)
			}
		}
	})

	t.Run("resumes a caller after a call to a function without native code", func(t *testing.T) {
		native(t)
		s := types.String("s")
		deep := []interp.Option{interp.WithFrame(1024), interp.WithStack(1 << 14)}
		sumrecBuilder := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString, types.TypeI32}, Returns: []types.Type{types.TypeI32}})
		zero := sumrecBuilder.Label()
		sumrecBuilder.Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_EQZ)).BrIf(zero)
		sumrecBuilder.Emit(
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, uint64(1)), instr.New(instr.CALL), instr.New(instr.I32_ADD), instr.New(instr.RETURN),
		)
		sumrecBuilder.Bind(zero).Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN))
		sumrec := sumrecBuilder.MustBuild()
		for _, c := range []struct {
			name string
			prog *program.Program
			opts []interp.Option
		}{
			{"a callee that never compiles", loopProgram(t, 8, 64, 64, false, guardFunction(1000, false), loopFunction(0, false), s), nil},
			{"a callee that never compiles, passed an owned argument", loopProgram(t, 8, 64, 64, false, guardFunction(1000, false), loopFunction(0, true), s), nil},
			{"a callee that never compiles calling native code that calls it again", loopProgram(t, 8, 64, 64, false, guardFunction(1000, false), loopFunction(4, false), s, relayFunction(0, false), relayFunction(3, true)), nil},
			{"recursion past the native activation limit", loopProgram(t, 8, 10, 600, false, guardFunction(1000, false), sumrec, s), deep},
		} {
			want := interp.New(c.prog, append([]interp.Option{interp.WithThreshold(-1)}, c.opts...)...)
			require.NoError(t, want.Run(context.Background()), c.name)
			wantSum, wantCount, err := popLoop(want)
			require.NoError(t, err, c.name)
			require.NoError(t, want.Close())

			profiler := prof.New()
			vm := interp.New(c.prog, append([]interp.Option{interp.WithThreshold(1), interp.WithProfiler(profiler)}, c.opts...)...)
			exits := func(kind string) float64 {
				vm.Flush()
				v, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: kind})
				return v
			}
			run := func() {
				require.NoError(t, vm.Run(context.Background()), c.name)
				sum, count, err := popLoop(vm)
				require.NoError(t, err, c.name)
				require.Equal(t, wantSum, sum, c.name)
				require.Equal(t, wantCount, count, c.name)
				vm.Reset()
			}
			poll(t, func() bool {
				err := vm.Run(context.Background())
				vm.Reset()
				return err != nil || exits("call") > 0
			})
			for range 16 {
				run()
			}
			before := exits("call")
			for range 16 {
				run()
			}
			require.Greater(t, exits("call"), before, c.name)
			require.Zero(t, exits("deopt"), c.name)
			require.NoError(t, vm.Close())
		}
	})

	t.Run("resumes a dynamic call through call exits", func(t *testing.T) {
		native(t)
		prog := mixedProgram(t, 64, false)
		want := interp.New(prog, interp.WithThreshold(-1))
		require.NoError(t, want.Run(context.Background()))
		wantSum, wantCount, err := popLoop(want)
		require.NoError(t, err)
		require.NoError(t, want.Close())

		// Both callees are seen before the caller compiles.
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(8), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		entries := func() float64 {
			return metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"}) + metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
		}
		run := func() {
			require.NoError(t, vm.Run(context.Background()))
			sum, count, err := popLoop(vm)
			require.NoError(t, err)
			require.Equal(t, wantSum, sum)
			require.Equal(t, wantCount, count)
			vm.Reset()
		}
		call, deopt := prof.Label{Key: "kind", Value: "call"}, prof.Label{Key: "kind", Value: "deopt"}
		poll(t, func() bool {
			err := vm.Run(context.Background())
			vm.Reset()
			return err != nil || metric("vm_jit_exits_total", call) > 0
		})
		beforeEntries, beforeCalls := entries(), metric("vm_jit_exits_total", call)
		for range 16 {
			run()
		}
		require.Greater(t, entries(), beforeEntries)
		require.Greater(t, metric("vm_jit_exits_total", call), beforeCalls)
		require.Zero(t, metric("vm_jit_exits_total", deopt))
		// The caller of the dynamic call compiles instead of being refused.
		require.Zero(t, metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "unsupported"}))
	})

	t.Run("reports an uncaught trap in a resumed callee", func(t *testing.T) {
		native(t)
		prog := loopProgram(t, 8, 50, 100, false, guardFunction(60, false), loopFunction(0, false), types.String("s"))
		wantErr := runProgramErr(t, prog, interp.WithThreshold(-1))
		require.ErrorIs(t, wantErr, interp.ErrDivideByZero)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		var gotErr error
		var exits float64
		poll(t, func() bool {
			gotErr = vm.Run(context.Background())
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return !errorsEqual(gotErr, wantErr) || exits > 0
		})
		require.True(t, errorsEqual(gotErr, wantErr), "%v != %v", gotErr, wantErr)
		require.Positive(t, exits)
	})

	t.Run("serves a trapping, throwing, or coroutine callee by call exit", func(t *testing.T) {
		native(t)
		s := types.String("s")
		// A coroutine's CALL returns a handle, not the results the native
		// caller's own call site types: the caller cannot resume.
		co := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString, types.TypeI32}, Returns: []types.Type{types.TypeAny}}).
			Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.YIELD), instr.New(instr.RETURN)).MustBuild()
		drive := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString, types.TypeI32}, Returns: []types.Type{types.TypeI32}}).Locals(types.TypeI32)
		loop, done := drive.Label(), drive.Label()
		drive.Bind(loop).Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_GE_S)).BrIf(done)
		drive.Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 2), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL), instr.New(instr.DROP),
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2),
		).Br(loop)
		drive.Bind(done).Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.RETURN))
		for _, c := range []struct {
			name string
			prog *program.Program
		}{
			{"coroutine", loopProgram(t, 64, 8, 8, false, co, drive.MustBuild(), s)},
			{"trap", loopProgram(t, 8, 50, 100, true, guardFunction(60, false), loopFunction(0, false), s)},
			{"trap with an owned argument", loopProgram(t, 8, 50, 100, true, guardFunction(60, false), loopFunction(0, true), s)},
			{"throw", loopProgram(t, 8, 50, 100, true, guardFunction(60, true), loopFunction(0, false), s)},
		} {
			want := interp.New(c.prog, interp.WithThreshold(-1))
			require.NoError(t, want.Run(context.Background()), c.name)
			wantSum, wantCount, err := popLoop(want)
			require.NoError(t, err, c.name)
			require.NoError(t, want.Close())

			profiler := prof.New()
			vm := interp.New(c.prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
			exits := func() float64 {
				vm.Flush()
				v, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
				return v
			}
			poll(t, func() bool {
				err := vm.Run(context.Background())
				vm.Reset()
				return err != nil || exits() > 0
			})
			require.NoError(t, vm.Run(context.Background()), c.name)
			sum, count, err := popLoop(vm)
			require.NoError(t, err, c.name)
			require.Equal(t, wantSum, sum, c.name)
			require.Equal(t, wantCount, count, c.name)
			require.Positive(t, exits(), c.name)
			require.NoError(t, vm.Close())
		}
	})

	t.Run("keeps a borrowed callee's RefCount across a served call", func(t *testing.T) {
		native(t)
		// inner never compiles, so native outer always serves its call.
		held := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString}, Returns: []types.Type{types.TypeString}})
		refuse(held)
		inner := held.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)).MustBuild()
		outer := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString}, Returns: []types.Type{types.TypeString}}).
			Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL), instr.New(instr.RETURN)).MustBuild()

		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(20000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(inner, outer, types.String("chain")))
		wantValue, wantCount := runProgramString(t, prog)

		var runErr, popErr, refErr error
		var value string
		var count, innerCount int
		var exits float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			innerCount, refErr = vm.RefCount(1)
			if refErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return exits > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, refErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
		require.Equal(t, 1, innerCount)
	})

	t.Run("passes a borrowed parameter through native recursion", func(t *testing.T) {
		native(t)
		// self (param 1) is borrowed at both of indirectFibFunction's own
		// dynamic CALLs: a native caller never retains it, so a deopt at the
		// frame limit exercises the served call's own retain of it.
		fib := indirectFibFunction()
		b := instr.NewBuilder()
		loop, done, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 5).Emit(instr.CONST_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		b.Bind(start).Emit(instr.I32_CONST, uint64(30)).Emit(instr.CONST_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Bind(end)
		b.Bind(catch).Emit(instr.ERROR_CODE)
		b.Try(start, end, catch, 1)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fib), program.WithHandlers(b.Handlers()...))
		wantVM := interp.New(prog, interp.WithFrame(8), interp.WithThreshold(-1))
		defer wantVM.Close()
		require.NoError(t, wantVM.Run(context.Background()))
		wantConst, err := wantVM.Const(0)
		require.NoError(t, err)
		wantRC, err := wantVM.RefCount(wantConst.Ref())
		require.NoError(t, err)

		var runErr, rcErr error
		var gotRC int
		var exits float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithFrame(8), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			c, _ := vm.Const(0)
			gotRC, rcErr = vm.RefCount(c.Ref())
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return exits > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, rcErr)
		// The module's own constant pool is fib's only live reference: a
		// mid-depth deopt through the borrowed self parameter must neither
		// leak an extra retain nor drop the constant pool's own.
		require.Equal(t, wantRC, gotRC)
	})

	t.Run("transfers an owned argument lent to a borrowed parameter", func(t *testing.T) {
		native(t)
		prog := lentProgram(t, 3000)

		wantVM := interp.New(lentProgram(t, 3000), interp.WithThreshold(-1))
		defer wantVM.Close()
		require.NoError(t, wantVM.Run(context.Background()))
		want, err := wantVM.Pop()
		require.NoError(t, err)
		wantInc, err := wantVM.Const(2)
		require.NoError(t, err)
		wantIncRC, err := wantVM.RefCount(wantInc.Ref())
		require.NoError(t, err)
		wantDec, err := wantVM.Const(3)
		require.NoError(t, err)
		wantDecRC, err := wantVM.RefCount(wantDec.Ref())
		require.NoError(t, err)

		var runErr, popErr, rcErr error
		var got types.Value
		var gotIncRC, gotDecRC int
		var deopts float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			gotInc, err := vm.Const(2)
			if err != nil {
				popErr = err
				return true
			}
			gotIncRC, rcErr = vm.RefCount(gotInc.Ref())
			if rcErr != nil {
				return true
			}
			gotDec, err := vm.Const(3)
			if err != nil {
				popErr = err
				return true
			}
			gotDecRC, rcErr = vm.RefCount(gotDec.Ref())
			if rcErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return deopts >= 1
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, rcErr)
		require.Equal(t, want, got)
		// If the !Owned filter in compile.call's Lent were missing, a's own
		// global-backed argument would be retained twice at the deopt: once
		// by a's own translator-side owning of it, once by a wrongly
		// populated Lent entry for apply's borrowed param 1.
		require.Equal(t, wantIncRC, gotIncRC)
		require.Equal(t, wantDecRC, gotDecRC)
		require.GreaterOrEqual(t, deopts, float64(1))
		require.Less(t, deopts, float64(3000))
	})

	t.Run("catches a native callee's throw in its native caller", func(t *testing.T) {
		native(t)
		const calls, batches = 32, 64
		thrower := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
		raise := thrower.Label()
		thrower.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 15), instr.New(instr.I32_AND), instr.New(instr.I32_CONST, 15), instr.New(instr.I32_EQ)).BrIf(raise)
		thrower.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN))
		thrower.Bind(raise).Emit(instr.New(instr.CONST_GET, 2), instr.New(instr.THROW))

		guard := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
		start, end, catch := guard.Label(), guard.Label(), guard.Label()
		guard.Bind(start).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.CALL))
		guard.Bind(end).Emit(instr.New(instr.RETURN))
		guard.Bind(catch).Emit(instr.New(instr.STRING_LEN), instr.New(instr.RETURN))
		guard.Try(start, end, catch, 1)

		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 0)
		for x := range calls {
			b.Emit(instr.I32_CONST, uint64(x)).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.I32_ADD)
		}
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(guard.MustBuild(), thrower.MustBuild(), types.String("boom!")))

		threaded := interp.New(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		counts := func(vm *interp.Interpreter) []int {
			var out []int
			for index := range 3 {
				c, err := vm.Const(index)
				require.NoError(t, err)
				count, err := vm.RefCount(c.Ref())
				require.NoError(t, err)
				out = append(out, count)
			}
			return out
		}
		wantCounts := counts(threaded)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		entries := func() float64 {
			return metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"}) +
				metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
		}
		var runErr error
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"}) > 0
		})
		require.NoError(t, runErr)

		before := entries()
		for range batches {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.Equal(t, wantCounts, counts(vm))
			vm.Reset()
		}
		require.GreaterOrEqual(t, entries()-before, float64(batches*calls))
		// Neither the caller nor its callee is refused.
		require.Zero(t, metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "unsupported"}))
	})

	t.Run("runs a function allocated after construction interpreted", func(t *testing.T) {
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

		vm := interp.New(prog, interp.WithThreshold(1))
		defer vm.Close()
		addr, err := vm.Alloc(fn)
		require.NoError(t, err)
		require.NoError(t, vm.SetGlobal(0, types.BoxRef(addr)))
		require.NoError(t, vm.Run(context.Background()))
		result, err := vm.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(7), result)
	})

	t.Run("sums a typed i32 array in a calling function", func(t *testing.T) {
		native(t)
		identBuilder := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
		identBuilder.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN))
		ident := identBuilder.MustBuild()
		sum := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.NewArrayType(types.TypeI32)}, Returns: []types.Type{types.TypeI32},
		})
		header, done := sum.Label(), sum.Label()
		sum.Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 1))
		sum.Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 2))
		sum.Bind(header)
		sum.Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 0), instr.New(instr.ARRAY_LEN), instr.New(instr.I32_GE_S)).BrIf(done)
		sum.Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.CALL), instr.New(instr.DROP))
		sum.Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.ARRAY_GET), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2))
		sum.Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 1))
		sum.Br(header)
		sum.Bind(done).Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.RETURN))
		sumFn := sum.MustBuild()
		sumFn.Locals = []types.Type{types.TypeI32, types.TypeI32}

		arr := types.NewArray(types.NewArrayType(types.TypeI32), types.BoxI32(10), types.BoxI32(20), types.BoxI32(30), types.BoxI32(40))
		b := instr.NewBuilder()
		loop, loopDone := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(20000)).Emit(instr.I32_GE_S).BrIf(loopDone)
		b.Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(loopDone).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(sumFn, ident, arr))
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var entries float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("reads and writes a ref array element", func(t *testing.T) {
		native(t)
		// Each interpreter gets its own freshly built program: array.set
		// mutates the array constant in place, and program.Program shares
		// that *types.Array Go object across every interp.New call that
		// reuses it, so a program run to completion once must not be reused
		// for a second run (want, then every retry below).
		wantValue, wantCount := runProgramString(t, refArrayProgram(t, 20000))

		var runErr, popErr error
		var value string
		var count int
		var entries float64
		profiler := prof.New()
		vm := interp.New(refArrayProgram(t, 20000), interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
	})

	t.Run("walks a struct tree", func(t *testing.T) {
		native(t)
		// Each interpreter gets its own freshly built program: the module's
		// own linking code (struct.set) mutates the leaf/mid/root struct
		// constants in place, another instance of the refArrayProgram
		// cross-run-reuse hazard above.
		want := runProgram(t, structTreeProgram(t, 20000))

		var runErr, popErr error
		var result types.Value
		var compiles float64
		profiler := prof.New()
		vm := interp.New(structTreeProgram(t, 20000), interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			// The module loop may enter by OSR before walk's first Go entry,
			// calling walk natively: walk compiled and any native entry.
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles > 0 && nativeEntries(profiler) > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("catches an out-of-bounds array.get in a guest handler", func(t *testing.T) {
		native(t)
		at := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.NewArrayType(types.TypeI32), types.TypeI32}, Returns: []types.Type{types.TypeI32},
		})
		at.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.ARRAY_GET), instr.New(instr.RETURN))
		atFn := at.MustBuild()
		arr := types.NewArray(types.NewArrayType(types.TypeI32), types.BoxI32(1), types.BoxI32(2), types.BoxI32(3))

		b := instr.NewBuilder()
		warmLoop, warmDone, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(warmLoop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(20000)).Emit(instr.I32_GE_S).BrIf(warmDone)
		b.Emit(instr.CONST_GET, 1).Emit(instr.I32_CONST, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(warmLoop)
		b.Bind(warmDone)
		b.Bind(start).Emit(instr.CONST_GET, 1).Emit(instr.I32_CONST, 99).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Bind(end)
		b.Bind(catch).Emit(instr.ERROR_CODE)
		b.Try(start, end, catch, 1)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(atFn, arr), program.WithHandlers(b.Handlers()...))
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var exits float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return exits > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("runs an indirect self call through a parameter", func(t *testing.T) {
		native(t)
		// Both dynamic sites are recorded before fib compiles.
		prog := indirectFibCallsProgram(t, 20, 50)

		wantVM := interp.New(indirectFibCallsProgram(t, 20, 50), interp.WithThreshold(-1))
		defer wantVM.Close()
		require.NoError(t, wantVM.Run(context.Background()))
		want, err := wantVM.Pop()
		require.NoError(t, err)
		wantConst, err := wantVM.Const(0)
		require.NoError(t, err)
		wantRC, err := wantVM.RefCount(wantConst.Ref())
		require.NoError(t, err)

		var runErr, popErr, rcErr error
		var got types.Value
		var gotRC int
		var compiles, entries, deopts float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(100), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			gotConst, err := vm.Const(0)
			if err != nil {
				popErr = err
				return true
			}
			gotRC, rcErr = vm.RefCount(gotConst.Ref())
			if rcErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "ok"})
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return compiles > 0 && entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, rcErr)
		require.Equal(t, want, got)
		require.Equal(t, wantRC, gotRC)
		require.Zero(t, deopts)
	})

	t.Run("traps a closure callee's site until native code records it", func(t *testing.T) {
		native(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.CONST_GET, 0).Emit(instr.CLOSURE_NEW).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(50)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.CONST_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		// wrap calls a closure through param 1, a dynamic CALL: native never
		// records a closure callee (call's own hook only reaches a
		// *types.Function target), so this site never speculates.
		wrap := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeAny}, Returns: []types.Type{types.TypeI32}})
		wrap.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.CALL), instr.New(instr.RETURN))
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeAny),
			program.WithConstants(incFunction(), wrap.MustBuild()))
		want := runProgram(t, prog)

		vm := interp.New(prog, interp.WithThreshold(1))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		got, err := vm.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	t.Run("calls closures natively", func(t *testing.T) {
		native(t)
		prog := counterProgram(t)

		threaded := interp.New(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		wantCounter, err := threaded.Pop()
		require.NoError(t, err)
		wantValue, wantCount, err := popString(threaded)
		require.NoError(t, err)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, label prof.Label) float64 {
			v, _ := profiler.Metric(name, label)
			return v
		}
		entry := prof.Label{Key: "tier", Value: "optimized"}
		deopt, call := prof.Label{Key: "kind", Value: "deopt"}, prof.Label{Key: "kind", Value: "call"}

		var counter types.Value
		var value string
		var count int
		var runErr, popErr error
		var compiled, entries, deopts, called float64
		poll(t, func() bool {
			vm.Flush()
			entered, deopted, exited := metric("vm_jit_entries_total", entry), metric("vm_jit_exits_total", deopt), metric("vm_jit_exits_total", call)
			if runErr = vm.Run(context.Background()); runErr != nil {
				return true
			}
			if counter, popErr = vm.Pop(); popErr != nil {
				return true
			}
			if value, count, popErr = popString(vm); popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, deopts, called = metric("vm_jit_entries_total", entry)-entered, metric("vm_jit_exits_total", deopt)-deopted, metric("vm_jit_exits_total", call)-exited
			// The closure body is the only Baseline unit: compiled, it is
			// called natively, or the Run takes a call exit.
			compiled, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiled > 0 && entries > 0 && deopts == 0 && called == 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, wantCounter, counter)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
		require.Positive(t, compiled)
		require.Positive(t, entries)
		require.Zero(t, deopts)
		require.Zero(t, called)
	})

	t.Run("bridges CLOSURE_NEW", func(t *testing.T) {
		native(t)
		const n = 200_000
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeString}}).
			Captures(types.TypeString).
			Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.RETURN)).
			MustBuild()
		b := instr.NewBuilder()
		loop, done, inner, innerDone := b.Label(), b.Label(), b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, n).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CONST_GET, 1).Emit(instr.CLOSURE_NEW).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(inner)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(innerDone)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Br(inner)
		b.Bind(innerDone)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.CONST_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeAny, types.TypeI32), program.WithConstants(types.String("held"), fn))
		wantValue, wantCount := runProgramString(t, prog)

		var runErr, popErr error
		var value string
		var count int
		var bridges, deopts float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			if runErr = vm.Run(context.Background()); runErr != nil {
				return true
			}
			if value, count, popErr = popString(vm); popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			// A declined bridge deopts and its site retires within a few
			// entries; only resumed ones number in the thousands.
			return bridges > 1000
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
		require.Greater(t, bridges, float64(1000))
		require.Zero(t, deopts)
	})

	t.Run("computes every primitive operator bit for bit", func(t *testing.T) {
		native(t)
		w32 := func(v int32) uint64 { return uint64(uint32(v)) }
		w64 := func(v int64) uint64 { return uint64(v) }
		f32 := func(v float32) uint64 { return uint64(math.Float32bits(v)) }
		f64 := math.Float64bits
		nan32, neg32 := float32(math.NaN()), float32(math.Copysign(0, -1))
		i32, i64, t32, t64 := types.TypeI32, types.TypeI64, types.TypeF32, types.TypeF64

		for _, c := range []struct {
			code   instr.Opcode
			params []types.Type
			args   [][]uint64
		}{
			{instr.I32_CLZ, []types.Type{i32}, [][]uint64{{w32(0)}, {w32(-1)}, {w32(math.MinInt32)}}},
			{instr.I32_CTZ, []types.Type{i32}, [][]uint64{{w32(0)}, {w32(-1)}, {w32(math.MinInt32)}}},
			{instr.I32_POPCNT, []types.Type{i32}, [][]uint64{{w32(0)}, {w32(-1)}, {w32(0x55555555)}}},
			{instr.I32_ROTL, []types.Type{i32, i32}, [][]uint64{{w32(0x12345678), w32(4)}, {w32(0x12345678), w32(-1)}, {w32(0x12345678), w32(37)}}},
			{instr.I32_ROTR, []types.Type{i32, i32}, [][]uint64{{w32(0x12345678), w32(4)}, {w32(0x12345678), w32(-1)}, {w32(0x12345678), w32(37)}}},
			{instr.I64_CLZ, []types.Type{i64}, [][]uint64{{w64(0)}, {w64(-1)}, {w64(1 << 47)}}},
			{instr.I64_CTZ, []types.Type{i64}, [][]uint64{{w64(0)}, {w64(-1)}, {w64(1 << 47)}}},
			{instr.I64_POPCNT, []types.Type{i64}, [][]uint64{{w64(0)}, {w64(-1)}, {w64(0x555555555555)}}},
			{instr.I64_ROTL, []types.Type{i64, i64}, [][]uint64{{w64(0x123456789ABC), w64(4)}, {w64(0x123456789ABC), w64(-1)}, {w64(0x123456789ABC), w64(69)}}},
			{instr.I64_ROTR, []types.Type{i64, i64}, [][]uint64{{w64(0x123456789ABC), w64(4)}, {w64(0x123456789ABC), w64(-1)}, {w64(0x123456789ABC), w64(69)}}},
			{instr.F32_COPYSIGN, []types.Type{t32, t32}, [][]uint64{{f32(1.5), f32(-1)}, {f32(nan32), f32(-1)}, {f32(0), f32(neg32)}}},
			{instr.F64_COPYSIGN, []types.Type{t64, t64}, [][]uint64{{f64(1.5), f64(-1)}, {f64(math.NaN()), f64(-1)}, {f64(0), f64(math.Copysign(0, -1))}}},
			{instr.F32_REM, []types.Type{t32, t32}, [][]uint64{{f32(5.5), f32(2)}, {f32(-5.5), f32(2)}}},
			{instr.F32_MOD, []types.Type{t32, t32}, [][]uint64{{f32(5.5), f32(2)}, {f32(-5.5), f32(2)}}},
			{instr.F64_REM, []types.Type{t64, t64}, [][]uint64{{f64(5.5), f64(2)}, {f64(-5.5), f64(2)}}},
			{instr.F64_MOD, []types.Type{t64, t64}, [][]uint64{{f64(5.5), f64(2)}, {f64(-5.5), f64(2)}}},
		} {
			fb := types.NewFunctionBuilder(&types.FunctionType{Params: c.params, Returns: c.params[:1]})
			for i := range c.params {
				fb.Emit(instr.New(instr.LOCAL_GET, uint64(i)))
			}
			fn := fb.Emit(instr.New(c.code), instr.New(instr.RETURN)).MustBuild()

			for _, args := range c.args {
				// The module calls fn with args 1000 times so fn compiles,
				// then once more for the result.
				b := instr.NewBuilder()
				loop, done := b.Label(), b.Label()
				call := func() {
					for i, arg := range args {
						b.Emit(map[types.Kind]instr.Opcode{types.KindI32: instr.I32_CONST, types.KindI64: instr.I64_CONST, types.KindF32: instr.F32_CONST, types.KindF64: instr.F64_CONST}[c.params[i].Kind()], arg)
					}
					b.Emit(instr.CONST_GET, 0).Emit(instr.CALL)
				}
				b.Bind(loop).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1000).Emit(instr.I32_GE_S).BrIf(done)
				call()
				b.Emit(instr.DROP).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
				b.Bind(done)
				call()
				code, err := b.Assemble()
				require.NoError(t, err)
				prog := program.New(code, program.WithLocals(i32), program.WithConstants(fn))

				// Boxed words compare bits: NaN payloads and the sign of zero.
				threaded := interp.New(prog, interp.WithThreshold(-1))
				require.NoError(t, threaded.Run(context.Background()))
				want, err := threaded.PopBoxed()
				require.NoError(t, err)
				require.NoError(t, threaded.Close())

				var got types.Boxed
				var entries, deopts float64
				var runErr, popErr error
				profiler := prof.New()
				vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
				defer vm.Close()
				poll(t, func() bool {
					if runErr = vm.Run(context.Background()); runErr != nil {
						return true
					}
					if got, popErr = vm.PopBoxed(); popErr != nil {
						return true
					}
					vm.Reset()
					vm.Flush()
					entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
					deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
					return entries > 0
				})
				require.NoError(t, runErr, "%s %x", c.code, args)
				require.NoError(t, popErr, "%s %x", c.code, args)
				require.Equal(t, want, got, "%s %x", c.code, args)
				require.Positive(t, entries, "%s %x", c.code, args)
				require.Zero(t, deopts, "%s %x", c.code, args)
			}
		}
	})

}
