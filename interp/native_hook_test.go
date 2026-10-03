package interp_test

import (
	"context"
	"errors"
	"maps"
	"math"
	"slices"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestWithThresholdHook(t *testing.T) {
	t.Run("enters native code under a hook or fuel", func(t *testing.T) {
		native(t)
		// The last program's module code ends in its CALL: its frame waits at
		// the end of its code.
		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 20).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		for _, prog := range []*program.Program{
			fibCallsProgram(t, 64),
			iterativeFibProgram(t, 1<<16),
			program.New(code, program.WithConstants(fibFunction())),
		} {
			want := runProgram(t, prog)
			for _, opt := range []interp.Option{
				interp.WithHook(func(*interp.Interpreter) error { return nil }),
				interp.WithFuel(math.MaxUint64),
			} {
				profiler := prof.New()
				vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler), opt)
				var runErr, popErr error
				var result types.Value
				run := func() bool {
					if runErr = vm.Run(context.Background()); runErr == nil {
						result, popErr = vm.Pop()
					}
					vm.Reset()
					vm.Flush()
					return runErr == nil && popErr == nil && result == want
				}
				poll(t, func() bool {
					return !run() || nativeEntries(profiler) > 0
				})
				// Native runs reach ticks at varying points: run a few more.
				for range 64 {
					if !run() {
						break
					}
				}
				require.NoError(t, vm.Close())
				require.NoError(t, runErr)
				require.NoError(t, popErr)
				require.Equal(t, want, result)
			}
		}
	})

	t.Run("shows a hook threaded state inside a native loop and keeps its writes", func(t *testing.T) {
		native(t)
		const n = 1 << 20
		prog := iterativeFibProgram(t, n)
		// The loop header follows the three local initializations; the
		// locals hold through its exit test, the next four instructions.
		var bounds [11]int
		for k := 1; k < len(bounds); k++ {
			bounds[k] = bounds[k-1] + instr.Instruction(prog.Code[bounds[k-1]:]).Width()
		}
		header, body := bounds[6], bounds[10]
		fibs := make([]int32, n+2)
		fibs[1] = 1
		for k := 2; k < len(fibs); k++ {
			fibs[k] = fibs[k-1] + fibs[k-2]
		}

		// At its 64th call between header and body the hook stops the loop
		// by writing its counter; until then it checks a = fib(i) and b =
		// fib(i+1). Past the header the exit test has read i already, so
		// one more iteration runs.
		seen, stop, at, torn := 0, -1, 0, false
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler), interp.WithHook(func(vm *interp.Interpreter) error {
			ip := vm.IP()
			if ip < header || ip > body || stop >= 0 {
				return nil
			}
			var locals [3]int32
			for slot := range locals {
				v, err := vm.Local(slot)
				if err != nil {
					return err
				}
				locals[slot] = v.I32()
			}
			i := locals[0]
			torn = torn || i < 0 || i >= n || locals[1] != fibs[i] || locals[2] != fibs[i+1]
			if seen++; seen == 64 {
				stop, at = int(i), ip
				return vm.SetLocal(0, types.BoxI32(n))
			}
			return nil
		}))
		defer vm.Close()
		var runErr, popErr error
		var result types.Value
		run := func() {
			seen, stop = 0, -1
			if runErr = vm.Run(context.Background()); runErr == nil {
				result, popErr = vm.Pop()
			}
			vm.Reset()
			vm.Flush()
		}
		poll(t, func() bool {
			run()
			return runErr != nil || popErr != nil || nativeEntries(profiler) > 0
		})
		entries := nativeEntries(profiler)
		run()

		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Greater(t, nativeEntries(profiler), entries)
		require.False(t, torn)
		require.Positive(t, stop)
		want := fibs[stop+1]
		if at == header {
			want = fibs[stop]
		}
		require.Equal(t, types.I32(want), result)
	})

	t.Run("shows a hook the native callers of a call it runs", func(t *testing.T) {
		native(t)
		// The module loop calls f(i) = g(i); g never compiles, so f's native
		// code exits to call it.
		g := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
		refuse(g)
		g.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN))
		f := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}}).
			Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.CALL), instr.New(instr.RETURN)).
			MustBuild()
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1<<12).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 1)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeI32), program.WithConstants(f, g.MustBuild()))
		// While g runs, the module frame waits past its CALL.
		resume := 0
		for instr.Instruction(prog.Code[resume:]).Opcode() != instr.CALL {
			resume += instr.Instruction(prog.Code[resume:]).Width()
		}
		resume++

		ips := map[int]int{}
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithTick(2), interp.WithProfiler(profiler), interp.WithHook(func(vm *interp.Interpreter) error {
			callee, err := vm.Const(1)
			if err != nil || vm.Func() != callee.Ref() {
				return err
			}
			_, ip, _, err := vm.Frame(2)
			ips[ip]++
			return err
		}))
		defer vm.Close()
		var runErr error
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			vm.Flush()
			return runErr != nil || nativeEntries(profiler) > 0
		})
		require.NoError(t, runErr)
		clear(ips)
		require.NoError(t, vm.Run(context.Background()))

		require.Equal(t, []int{resume}, slices.Collect(maps.Keys(ips)))
	})

	t.Run("stops a Run with a hook's error inside native code", func(t *testing.T) {
		native(t)
		prog := iterativeFibProgram(t, 1<<20)
		stop := errors.New("stop")
		calls := 0
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler), interp.WithHook(func(*interp.Interpreter) error {
			if calls++; calls == 256 {
				return stop
			}
			return nil
		}))
		defer vm.Close()
		var runErr error
		poll(t, func() bool {
			calls = 0
			runErr = vm.Run(context.Background())
			vm.Reset()
			vm.Flush()
			return runErr != stop || nativeEntries(profiler) > 0
		})
		require.Equal(t, stop, runErr)
	})

}
