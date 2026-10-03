package interp_test

import (
	"context"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestWithThresholdMaterialization(t *testing.T) {
	t.Run("releases a dying ref parameter on exit", func(t *testing.T) {
		native(t)
		fnBuilder := instr.NewBuilder()
		fnBuilder.Emit(instr.I32_CONST, 42).Emit(instr.RETURN)
		fnCode, err := fnBuilder.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:  &types.FunctionType{Params: []types.Type{types.TypeString}, Returns: []types.Type{types.TypeI32}},
			Code: instr.Marshal(fnCode),
		}
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

		var runErr, popErr, refErr error
		var survivor types.Boxed
		var count int
		var released float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			survivor, popErr = vm.PopBoxed()
			if popErr != nil {
				return true
			}
			count, refErr = vm.RefCount(survivor.Ref())
			if refErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			released, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "release"})
			return released > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, refErr)
		require.Equal(t, 1, count)
	})

	t.Run("deoptimizes an unlowered operator that traps", func(t *testing.T) {
		native(t)
		const warm = 4000
		b := instr.NewBuilder()
		loop, done, inner, innerDone, same := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Emit(instr.CONST_GET, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 2*warm).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(inner)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(innerDone)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).Br(inner)
		b.Bind(innerDone)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, warm).Emit(instr.I32_NE).BrIf(same)
		b.Emit(instr.CONST_GET, 1).Emit(instr.LOCAL_SET, 2)
		b.Bind(same)
		// string.eq releases "x" before it finds the array at iteration warm.
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.CONST_GET, 0).Emit(instr.STRING_EQ).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code,
			program.WithLocals(types.TypeI32, types.TypeI32, types.TypeAny),
			program.WithConstants(types.String("x"), types.TypedArray[int32]{1}))

		threaded := interp.New(prog, interp.WithThreshold(-1))
		wantErr := threaded.Run(context.Background())
		require.ErrorIs(t, wantErr, interp.ErrTypeMismatch)
		x, err := threaded.Const(0)
		require.NoError(t, err)
		array, err := threaded.Const(1)
		require.NoError(t, err)
		wantX, err := threaded.RefCount(x.Ref())
		require.NoError(t, err)
		wantArray, err := threaded.RefCount(array.Ref())
		require.NoError(t, err)
		require.NoError(t, threaded.Close())

		var gotErr, xErr, arrayErr error
		var gotX, gotArray int
		var bridges float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			gotErr = vm.Run(context.Background())
			gotX, xErr = vm.RefCount(x.Ref())
			gotArray, arrayErr = vm.RefCount(array.Ref())
			vm.Reset()
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return bridges > 1
		})
		// A bridge that deoptimizes leaves native code for the rest of the Run.
		require.Greater(t, bridges, float64(1))
		require.True(t, errorsEqual(gotErr, wantErr), "got %v, want %v", gotErr, wantErr)
		require.NoError(t, xErr)
		require.NoError(t, arrayErr)
		require.Equal(t, wantX, gotX)
		require.Equal(t, wantArray, gotArray)
	})

	t.Run("deopts self-recursive fib at a frame limit", func(t *testing.T) {
		native(t)
		// The handler unwinds every native and interpreter frame the deopt
		// left above it and releases the operand stack, so RefCount after a
		// successful Run reflects only durable state, comparable across
		// threaded and native runs. (An uncaught error leaves abandoned
		// frames on both paths, by design, and is not comparable this way.)
		fib := fibFunction()
		b := instr.NewBuilder()
		loop, done, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 5).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		b.Bind(start).Emit(instr.I32_CONST, uint64(30)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
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
		require.Equal(t, 1, wantRC)

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
		// mid-depth deopt through the CSE'd, borrowed callee must neither
		// leak an extra retain nor drop the constant pool's own.
		require.Equal(t, wantRC, gotRC)
	})

	t.Run("deopts a callee with register-passed ref and i32 parameters", func(t *testing.T) {
		native(t)
		// rec(s, n) = n < 1 ? n : rec(s, n-1) + 1. The module warms rec(s, 2),
		// then calls rec(s, 30) under a handler: WithFrame(10) deopts it
		// several native activations deep.
		fb := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString, types.TypeI32}, Returns: []types.Type{types.TypeI32}})
		small := fb.Label()
		fb.Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_LT_S)).BrIf(small)
		fb.Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN),
		)
		fb.Bind(small).Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.RETURN))
		rec := fb.MustBuild()

		mb := instr.NewBuilder()
		loop, done, start, end, catch := mb.Label(), mb.Label(), mb.Label(), mb.Label(), mb.Label()
		mb.Bind(loop)
		mb.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1000).Emit(instr.I32_GE_S).BrIf(done)
		mb.Emit(instr.CONST_GET, 1).Emit(instr.I32_CONST, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		mb.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		mb.Br(loop)
		mb.Bind(done)
		mb.Bind(start).Emit(instr.CONST_GET, 1).Emit(instr.I32_CONST, 30).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		mb.Bind(end)
		mb.Bind(catch).Emit(instr.ERROR_CODE)
		mb.Try(start, end, catch, 1)
		code, err := mb.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(rec, types.String("s")), program.WithHandlers(mb.Handlers()...))

		wantVM := interp.New(prog, interp.WithFrame(8), interp.WithThreshold(-1))
		defer wantVM.Close()
		require.NoError(t, wantVM.Run(context.Background()))
		want, err := wantVM.Pop()
		require.NoError(t, err)
		wantConst, err := wantVM.Const(1)
		require.NoError(t, err)
		wantRC, err := wantVM.RefCount(wantConst.Ref())
		require.NoError(t, err)

		var runErr, popErr, rcErr error
		var got types.Value
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
			got, popErr = vm.Pop()
			c, _ := vm.Const(1)
			gotRC, rcErr = vm.RefCount(c.Ref())
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return exits > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, rcErr)
		require.Equal(t, want, got)
		require.Equal(t, wantRC, gotRC)
	})

	t.Run("deopts a dynamic call whose callee takes other parameters", func(t *testing.T) {
		native(t)
		prog := mixedProgram(t, 64, true)
		want := interp.New(prog, interp.WithThreshold(-1))
		require.NoError(t, want.Run(context.Background()))
		wantSum, wantCount, err := popLoop(want)
		require.NoError(t, err)
		require.NoError(t, want.Close())

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(8), interp.WithProfiler(profiler))
		defer vm.Close()
		entries := func() float64 {
			vm.Flush()
			v, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return v
		}
		for range 64 {
			require.NoError(t, vm.Run(context.Background()))
			sum, count, err := popLoop(vm)
			require.NoError(t, err)
			require.Equal(t, wantSum, sum)
			require.Equal(t, wantCount, count)
			vm.Reset()
			time.Sleep(time.Millisecond)
		}
		require.Positive(t, entries())
	})

	t.Run("deopts a dynamic call only once it runs", func(t *testing.T) {
		native(t)
		for _, c := range []struct {
			name  string
			final int
		}{
			{"it stays cold", -1},
			{"it runs", 3},
		} {
			b := instr.NewBuilder()
			loop, done := b.Label(), b.Label()
			b.Emit(instr.CONST_GET, 2).Emit(instr.LOCAL_SET, 2)
			b.Bind(loop)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(64)).Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 100).Emit(instr.I32_CONST, uint64(math.MaxUint32)).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
			b.Br(loop)
			b.Bind(done)
			b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 100).Emit(instr.I32_CONST, uint64(c.final)).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2)
			code, err := b.Assemble()
			require.NoError(t, err)
			coldBuilder := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString, types.TypeI32, types.TypeI32, types.TypeAny}, Returns: []types.Type{types.TypeI32}}).Locals(types.TypeI32, types.TypeI32)
			coldLoop, coldDone, hit, next := coldBuilder.Label(), coldBuilder.Label(), coldBuilder.Label(), coldBuilder.Label()
			coldBuilder.Bind(coldLoop).Emit(instr.New(instr.LOCAL_GET, 5), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_GE_S)).BrIf(coldDone)
			coldBuilder.Emit(instr.New(instr.LOCAL_GET, 5), instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_EQ)).BrIf(hit)
			coldBuilder.Emit(instr.New(instr.LOCAL_GET, 5)).Br(next)
			coldBuilder.Bind(hit).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 5), instr.New(instr.LOCAL_GET, 3), instr.New(instr.CALL))
			coldBuilder.Bind(next).Emit(
				instr.New(instr.LOCAL_GET, 4), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 4),
				instr.New(instr.LOCAL_GET, 5), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 5),
			).Br(coldLoop)
			coldBuilder.Bind(coldDone).Emit(instr.New(instr.LOCAL_GET, 4), instr.New(instr.RETURN))
			cold := coldBuilder.MustBuild()
			prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeI32, types.TypeString),
				program.WithConstants(cold, bumpFunction(1), types.String("s")))
			want := interp.New(prog, interp.WithThreshold(-1))
			require.NoError(t, want.Run(context.Background()), c.name)
			wantSum, wantCount, err := popLoop(want)
			require.NoError(t, err, c.name)
			require.NoError(t, want.Close())

			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
			metric := func(name string, labels ...prof.Label) float64 {
				vm.Flush()
				v, _ := profiler.Metric(name, labels...)
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
			entered := prof.Label{Key: "tier", Value: "baseline"}
			poll(t, func() bool {
				err := vm.Run(context.Background())
				vm.Reset()
				return err != nil || metric("vm_jit_entries_total", entered) > 0
			})
			for range 16 {
				run()
			}
			deopts := metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			if c.final < 0 {
				require.Zero(t, deopts, c.name)
			} else {
				require.Positive(t, deopts, c.name)
			}
			require.NoError(t, vm.Close())
		}
	})

	t.Run("deopts two native activations deep", func(t *testing.T) {
		native(t)
		ib := instr.NewBuilder()
		ib.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.STRING_EQ).Emit(instr.DROP)
		ib.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.STRING_CONCAT).Emit(instr.RETURN)
		icode, err := ib.Assemble()
		require.NoError(t, err)
		inner := &types.Function{
			Typ:  &types.FunctionType{Params: []types.Type{types.TypeString, types.TypeString}, Returns: []types.Type{types.TypeString}},
			Code: instr.Marshal(icode),
		}
		outerBuilder := instr.NewBuilder()
		outerBuilder.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
		outerCode, err := outerBuilder.Assemble()
		require.NoError(t, err)
		outer := &types.Function{
			Typ:  &types.FunctionType{Params: []types.Type{types.TypeString, types.TypeString}, Returns: []types.Type{types.TypeString}},
			Code: instr.Marshal(outerCode),
		}
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(20000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 3).Emit(instr.CONST_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 3).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32),
			program.WithConstants(inner, outer, types.String("deep-"), types.String("concat")))
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
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return exits > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, refErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
		require.Equal(t, 1, innerCount)
	})

	t.Run("deopts a deep recursion at its frame limit", func(t *testing.T) {
		native(t)

		// rec(n): a 20000-iteration loop (a resumed safepoint in every
		// activation), a dropped fresh struct (a resumed release), then
		// rec(n-1). Param 0 is n; local 1 the counter.
		record := types.NewStructType(types.NewStructField(types.TypeI32, types.FieldWithName("n")))
		fb := instr.NewBuilder()
		loop, done, small := fb.Label(), fb.Label(), fb.Label()
		fb.Bind(loop)
		fb.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 20000).Emit(instr.I32_GE_S).BrIf(done)
		fb.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		fb.Br(loop)
		fb.Bind(done)
		fb.Emit(instr.STRUCT_NEW_DEFAULT, 0).Emit(instr.DROP)
		fb.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_LT_S).BrIf(small)
		fb.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_SUB).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
		fb.Bind(small).Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
		fnCode, err := fb.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeI32},
			Code:   instr.Marshal(fnCode),
		}

		// The module warms rec(2), then calls rec(15) under a handler:
		// WithFrame(8) makes the native chain take ExitCall several
		// activations deep, and the handler catches the overflow.
		mb := instr.NewBuilder()
		wloop, wdone, start, end, catch := mb.Label(), mb.Label(), mb.Label(), mb.Label(), mb.Label()
		mb.Bind(wloop)
		mb.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 5).Emit(instr.I32_GE_S).BrIf(wdone)
		mb.Emit(instr.I32_CONST, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		mb.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		mb.Br(wloop)
		mb.Bind(wdone)
		mb.Bind(start).Emit(instr.I32_CONST, 15).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		mb.Bind(end)
		mb.Bind(catch).Emit(instr.ERROR_CODE)
		mb.Try(start, end, catch, 1)
		code, err := mb.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithTypes(record), program.WithConstants(fn), program.WithHandlers(mb.Handlers()...))

		wantVM := interp.New(prog, interp.WithFrame(8), interp.WithThreshold(-1))
		defer wantVM.Close()
		require.NoError(t, wantVM.Run(context.Background()))
		want, err := wantVM.Pop()
		require.NoError(t, err)

		var runErr, popErr error
		var got types.Value
		var safepoints, releases, calls float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithFrame(8), interp.WithProfiler(profiler))
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
			vm.Reset()
			vm.Flush()
			safepoints, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "safepoint"})
			releases, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "release"})
			calls, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return safepoints > 0 && releases > 0 && calls > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("returns a wide i64 from native code", func(t *testing.T) {
		native(t)
		fnBuilder := instr.NewBuilder()
		fnBuilder.Emit(instr.LOCAL_GET, 0).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL).Emit(instr.RETURN)
		fnCode, err := fnBuilder.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:  &types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI64}},
			Code: instr.Marshal(fnCode),
		}
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fn))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		result, err := vm.Pop()
		require.NoError(t, err)
		vm.Flush()

		deopts, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
		require.Zero(t, deopts)
		require.Equal(t, want, result)
	})

	t.Run("returns a wide i64 from native recursion", func(t *testing.T) {
		native(t)
		fnBuilder := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI64}})
		small := fnBuilder.Label()
		fnBuilder.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I64_CONST, 2), instr.New(instr.I64_LT_S)).BrIf(small)
		fnBuilder.Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I64_CONST, 1), instr.New(instr.I64_SUB), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I64_CONST, 2), instr.New(instr.I64_SUB), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.I64_ADD), instr.New(instr.RETURN),
		)
		fnBuilder.Bind(small).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I64_CONST, 1), instr.New(instr.I64_CONST, 50), instr.New(instr.I64_SHL), instr.New(instr.I64_ADD),
			instr.New(instr.RETURN),
		)
		fn := fnBuilder.MustBuild()
		b := instr.NewBuilder()
		b.Emit(instr.I64_CONST, uint64(20)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(fn))
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, entries float64
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
			vm.Reset()
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			baseline, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			optimized, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			entries = baseline + optimized
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	t.Run("passes a wide i64 argument into a native function", func(t *testing.T) {
		native(t)
		fn := wideArgFunction()
		b := instr.NewBuilder()
		for range 2000 {
			b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		}
		b.Emit(instr.I64_CONST, 1).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(fn))
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, entries float64
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
			vm.Reset()
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			baseline, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			optimized, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			entries = baseline + optimized
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	t.Run("passes a wide i64 register argument from a native caller", func(t *testing.T) {
		native(t)
		inner := wideArgFunction()
		outerBuilder := instr.NewBuilder()
		loop, done := outerBuilder.Label(), outerBuilder.Label()
		outerBuilder.Bind(loop)
		outerBuilder.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(done)
		outerBuilder.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		outerBuilder.Br(loop)
		outerBuilder.Bind(done)
		outerBuilder.Emit(instr.LOCAL_GET, 0).Emit(instr.I64_CONST, 1).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL).Emit(instr.I64_ADD)
		outerBuilder.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
		outerCode, err := outerBuilder.Assemble()
		if err != nil {
			panic(err)
		}
		outer := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI64}},
			Locals: []types.Type{types.TypeI32},
			Code:   instr.Marshal(outerCode),
		}
		b := instr.NewBuilder()
		for index := range 2 * 2000 {
			b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, uint64(index/2000)).Emit(instr.CALL).Emit(instr.DROP)
		}
		b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(inner, outer))
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, entries float64
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
			vm.Reset()
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			baseline, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			optimized, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			entries = baseline + optimized
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	t.Run("returns a wide i64 from an OSR unit", func(t *testing.T) {
		native(t)
		fnBuilder := instr.NewBuilder()
		loop, done := fnBuilder.Label(), fnBuilder.Label()
		fnBuilder.Bind(loop)
		fnBuilder.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_GE_S).BrIf(done)
		fnBuilder.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		fnBuilder.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		fnBuilder.Br(loop)
		fnBuilder.Bind(done)
		fnBuilder.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_TO_I64_S)
		fnBuilder.Emit(instr.I64_CONST, 1).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL).Emit(instr.I64_ADD)
		fnBuilder.Emit(instr.RETURN)
		fnCode, err := fnBuilder.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI64}},
			Locals: []types.Type{types.TypeI32, types.TypeI32},
			Code:   instr.Marshal(fnCode),
		}
		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, uint64(200_000)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(fn))
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, compiles float64
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
			vm.Reset()
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	t.Run("compiles a loop seeded with a wide i64", func(t *testing.T) {
		native(t)
		seed := int64(-3750763034362895579)
		for _, c := range []struct {
			name string
			prog *program.Program
		}{
			{"const", fnvProgram(t, 100_000, instr.I64_CONST, uint64(seed))},
			{"pool cell", fnvProgram(t, 100_000, instr.CONST_GET, 1, types.I64(seed))},
		} {
			want := runProgram(t, c.prog)

			var got types.Value
			var runErr, popErr error
			var deopts, compiles float64
			profiler := prof.New()
			vm := interp.New(c.prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
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
				vm.Reset()
				vm.Flush()
				deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
				compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
				return compiles > 0
			})
			require.NoError(t, runErr, c.name)
			require.NoError(t, popErr, c.name)
			require.Zero(t, deopts, c.name)
			require.Equal(t, want, got, c.name)
		}
	})

	t.Run("keeps a wide i64 constant's RefCount across a shape-guard deopt", func(t *testing.T) {
		native(t)
		seed := int64(-3750763034362895579)
		fnBuilder := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeI32)}, Returns: []types.Type{types.TypeI64}})
		fnBuilder.Emit(instr.New(instr.I64_CONST, uint64(seed)))
		fnBuilder.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.ARRAY_LEN), instr.New(instr.DROP))
		fnBuilder.Emit(instr.New(instr.RETURN))
		fn := fnBuilder.MustBuild()
		elem := types.NewArrayType(types.TypeI32)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(20000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 4).Emit(instr.ARRAY_NEW_DEFAULT, 0)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.GLOBAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithGlobals(elem),
			program.WithConstants(fn), program.WithTypes(elem))

		threaded := interp.New(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		hostVal, err := threaded.Marshal([]int32{1, 2, 3})
		require.NoError(t, err)
		hostAddr, err := threaded.Alloc(hostVal)
		require.NoError(t, err)
		require.NoError(t, threaded.SetGlobal(0, types.BoxRef(hostAddr)))
		require.NoError(t, threaded.Run(context.Background()))
		wantBoxed, err := threaded.PopBoxed()
		require.NoError(t, err)
		wantRC, err := threaded.RefCount(wantBoxed.Ref())
		require.NoError(t, err)
		require.Equal(t, 1, wantRC)

		var runErr, popErr, rcErr, marshalErr, allocErr, globalErr error
		var gotBoxed types.Boxed
		var gotRC int
		var exits float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			var hostVal types.Value
			hostVal, marshalErr = vm.Marshal([]int32{1, 2, 3})
			if marshalErr != nil {
				return true
			}
			var hostAddr int
			hostAddr, allocErr = vm.Alloc(hostVal)
			if allocErr != nil {
				return true
			}
			globalErr = vm.SetGlobal(0, types.BoxRef(hostAddr))
			if globalErr != nil {
				return true
			}
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			gotBoxed, popErr = vm.PopBoxed()
			if popErr != nil {
				return true
			}
			gotRC, rcErr = vm.RefCount(gotBoxed.Ref())
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return exits > 0
		})
		require.NoError(t, marshalErr)
		require.NoError(t, allocErr)
		require.NoError(t, globalErr)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, rcErr)
		require.Equal(t, wantRC, gotRC)
	})

	t.Run("promotes a narrow i64 accumulator in an OSR loop", func(t *testing.T) {
		native(t)
		fn := wideArgFunction()
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I64_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(200_000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I64_CONST, 5).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I64_XOR).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI64, types.TypeI32), program.WithConstants(fn))
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, compiles float64
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
			vm.Reset()
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	t.Run("promotes an i64 accumulator in an OSR FNV loop", func(t *testing.T) {
		native(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I64_CONST, uint64(0x1234567)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(100_000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_TO_I64_U).Emit(instr.I64_XOR)
		b.Emit(instr.I64_CONST, 1099511628211).Emit(instr.I64_MUL)
		b.Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI64, types.TypeI32))
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, compiles float64
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
			vm.Reset()
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		// OpComplete's own wide box used to deopt once (B); it now resumes.
		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	t.Run("boxes a wide store in a loop each iteration", func(t *testing.T) {
		native(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(50_000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I64_CONST, 1).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_TO_I64_S).Emit(instr.I64_ADD)
		b.Emit(instr.GLOBAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.GLOBAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithGlobals(types.TypeI64))
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, entries float64
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
			vm.Reset()
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			baseline, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			optimized, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			entries = baseline + optimized
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	t.Run("keeps a callee alive across a boxed wide i64 argument", func(t *testing.T) {
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
		var deopts, entries float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			if err = vm.Run(context.Background()); err != nil {
				return true
			}
			result, _ = vm.Pop()
			c, _ := vm.Const(0)
			rc, _ = vm.RefCount(c.Ref())
			vm.Reset()
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		})
		require.NoError(t, err)
		require.Zero(t, deopts)
		require.Equal(t, want, result)
		require.Equal(t, 1, rc)
	})

	t.Run("deopts a host array that fails its shape guard", func(t *testing.T) {
		native(t)
		ln := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeI32)}, Returns: []types.Type{types.TypeI32}})
		ln.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.ARRAY_LEN), instr.New(instr.RETURN))
		lnFn := ln.MustBuild()

		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(20000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.GLOBAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.GLOBAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithGlobals(types.NewArrayType(types.TypeI32)), program.WithConstants(lnFn))

		threaded := interp.New(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		hostVal, err := threaded.Marshal([]int32{1, 2, 3, 4, 5})
		require.NoError(t, err)
		hostAddr, err := threaded.Alloc(hostVal)
		require.NoError(t, err)
		require.NoError(t, threaded.SetGlobal(0, types.BoxRef(hostAddr)))
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(5), want)

		var runErr, popErr, marshalErr, allocErr, globalErr error
		var result types.Value
		var exits float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			hostVal, marshalErr := vm.Marshal([]int32{1, 2, 3, 4, 5})
			if marshalErr != nil {
				return true
			}
			hostAddr, allocErr := vm.Alloc(hostVal)
			if allocErr != nil {
				return true
			}
			globalErr = vm.SetGlobal(0, types.BoxRef(hostAddr))
			if globalErr != nil {
				return true
			}
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
		require.NoError(t, marshalErr)
		require.NoError(t, allocErr)
		require.NoError(t, globalErr)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("restores promoted locals when a callee deopts under a loop", func(t *testing.T) {
		native(t)
		const n = 200_000
		want := runProgram(t, deopt(t, n, n-10))
		prog := deopt(t, n, n-10)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		got, err := vm.Pop()
		require.NoError(t, err)
		vm.Flush()

		require.Equal(t, want, got)
		compiles, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
		require.Greater(t, compiles, float64(0))
		deopts, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
		require.Greater(t, deopts, float64(0))
	})

	t.Run("catches a deopt inside an OSR loop in a guest handler", func(t *testing.T) {
		native(t)
		// The OSR-eligible loop lives in a called function with no handlers
		// of its own; the module's handler wraps the call and catches the
		// real trap once it unwinds out of that frame.
		fnBuilder := instr.NewBuilder()
		loop, done := fnBuilder.Label(), fnBuilder.Label()
		fnBuilder.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0) // i = 0
		fnBuilder.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1) // sum = 0
		fnBuilder.Bind(loop)
		fnBuilder.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(2_000_000)).Emit(instr.I32_GE_S).BrIf(done)
		fnBuilder.Emit(instr.LOCAL_GET, 1)
		fnBuilder.Emit(instr.I32_CONST, 100)
		fnBuilder.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1_500_000)).Emit(instr.I32_SUB) // i-1_500_000
		fnBuilder.Emit(instr.I32_DIV_S)
		fnBuilder.Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		fnBuilder.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		fnBuilder.Br(loop)
		fnBuilder.Bind(done).Emit(instr.LOCAL_GET, 1).Emit(instr.RETURN)
		fnCode, err := fnBuilder.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:    &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeI32, types.TypeI32},
			Code:   instr.Marshal(fnCode),
		}
		b := instr.NewBuilder()
		start, end, catch := b.Label(), b.Label(), b.Label()
		b.Bind(start).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Bind(end)
		b.Bind(catch).Emit(instr.ERROR_CODE)
		b.Try(start, end, catch, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(fn), program.WithHandlers(b.Handlers()...))
		threaded := interp.New(prog, interp.WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		wantCode, err := threaded.PopBoxed()
		require.NoError(t, err)
		wantConst, err := threaded.Const(0)
		require.NoError(t, err)
		wantValue, err := threaded.RefCount(wantConst.Ref())
		require.NoError(t, err)

		var gotCode types.Boxed
		var gotValue int
		var exits float64
		var runErr, popErr, constErr, refErr error
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			gotCode, popErr = vm.PopBoxed()
			if popErr != nil {
				return true
			}
			str, err := vm.Const(0)
			constErr = err
			if constErr != nil {
				return true
			}
			gotValue, refErr = vm.RefCount(str.Ref())
			if refErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return exits > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, constErr)
		require.NoError(t, refErr)
		require.Equal(t, wantCode, gotCode)
		require.Equal(t, wantValue, gotValue)
	})

	t.Run("rebuilds a closure frame with its upvals after a declined bridge", func(t *testing.T) {
		native(t)
		const calls = 64
		fb := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeI1}, Returns: []types.Type{types.TypeI32}}).Captures(types.TypeI32)
		loop, done, slow := fb.Label(), fb.Label(), fb.Label()
		fb.Bind(loop).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_EQZ)).BrIf(done)
		fb.Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.UPVAL_SET, 0))
		fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB), instr.New(instr.LOCAL_SET, 0)).Br(loop)
		fb.Bind(done).Emit(instr.New(instr.LOCAL_GET, 1)).BrIf(slow)
		fb.Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.RETURN))
		fb.Bind(slow).Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW, 0), instr.New(instr.ARRAY_LEN))
		fb.Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.I32_ADD), instr.New(instr.UPVAL_GET, 0), instr.New(instr.I32_ADD), instr.New(instr.RETURN))
		gb := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Locals(types.TypeAny, types.TypeI32, types.TypeI32)
		loop, done = gb.Label(), gb.Label()
		gb.Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.CONST_GET, 0), instr.New(instr.CLOSURE_NEW), instr.New(instr.LOCAL_SET, 0))
		gb.Bind(loop).Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, calls), instr.New(instr.I32_GE_S)).BrIf(done)
		gb.Emit(instr.New(instr.I32_CONST, 100), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, calls-1), instr.New(instr.I32_EQ))
		gb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.CALL), instr.New(instr.LOCAL_SET, 2))
		gb.Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 1)).Br(loop)
		gb.Bind(done).Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.RETURN))
		b := instr.NewBuilder()
		b.Emit(instr.CONST_GET, 1).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(fb.MustBuild(), gb.MustBuild()), program.WithTypes(types.NewArrayType(types.TypeI32)))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		baseline := prof.Label{Key: "tier", Value: "baseline"}
		call := prof.Label{Key: "kind", Value: "call"}
		bridge := prof.Label{Key: "kind", Value: "bridge"}

		var got types.Value
		var runErr, popErr error
		var compiled, bridged, called float64
		poll(t, func() bool {
			vm.Flush()
			declined, exited := metric("vm_jit_exits_total", bridge), metric("vm_jit_exits_total", call)
			if runErr = vm.Run(context.Background()); runErr != nil {
				return true
			}
			if got, popErr = vm.Pop(); popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			// A closure runs natively only through a native caller: an
			// ARRAY_NEW bridge exit, which never resumes, with no call exit
			// means every closure call ran natively, the last one into it.
			compiled = metric("vm_jit_compiles_total", baseline, prof.Label{Key: "outcome", Value: "ok"})
			bridged, called = metric("vm_jit_exits_total", bridge)-declined, metric("vm_jit_exits_total", call)-exited
			return compiled >= 2 && bridged > 0 && called == 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		require.GreaterOrEqual(t, compiled, float64(2))
		require.Positive(t, bridged)
		require.Zero(t, called)
	})

	t.Run("faults on a directly called function's missing upval", func(t *testing.T) {
		native(t)
		const calls = 200
		fb := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeI1}, Returns: []types.Type{types.TypeI32}}).Captures(types.TypeI32)
		loop, done, read := fb.Label(), fb.Label(), fb.Label()
		fb.Bind(loop).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_EQZ)).BrIf(done)
		fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB), instr.New(instr.LOCAL_SET, 0)).Br(loop)
		fb.Bind(done).Emit(instr.New(instr.LOCAL_GET, 1)).BrIf(read)
		fb.Emit(instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN))
		fb.Bind(read).Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.RETURN))
		b := instr.NewBuilder()
		loop, done = b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, calls).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 1000).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, calls-1).Emit(instr.I32_EQ)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fb.MustBuild()))
		want := runProgramErr(t, prog, interp.WithThreshold(-1))
		require.Error(t, want)

		got := runProgramErr(t, prog, interp.WithThreshold(1))
		require.True(t, errorsEqual(got, want), "got %v, want %v", got, want)
	})

	t.Run("zeroes each unwritten local of a native function", func(t *testing.T) {
		native(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 20_000).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		for range zeroTypes {
			b.Emit(instr.DROP)
		}
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(zeroFunction()))

		var got []types.Value
		var entries float64
		var runErr, popErr error
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got = got[:0]
			for range zeroValues {
				var v types.Value
				if v, popErr = vm.Pop(); popErr != nil {
					return true
				}
				got = append(got, v)
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, zeroValues, got)
	})

	t.Run("zeroes each unwritten module local and global under OSR", func(t *testing.T) {
		native(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 200_000).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		for j := range zeroTypes {
			b.Emit(instr.LOCAL_GET, uint64(j+1))
		}
		for j := range zeroTypes {
			b.Emit(instr.GLOBAL_GET, uint64(j))
		}
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(append([]types.Type{types.TypeI32}, zeroTypes...)...), program.WithGlobals(zeroTypes...))
		want := slices.Concat(zeroValues, zeroValues)

		var got []types.Value
		var entries float64
		var runErr, popErr error
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(1), interp.WithProfiler(profiler))
		defer vm.Close()
		poll(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got = got[:0]
			for range want {
				var v types.Value
				if v, popErr = vm.Pop(); popErr != nil {
					return true
				}
				got = append(got, v)
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		})
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

}
