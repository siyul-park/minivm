package benchmarks

import (
	"context"
	"runtime"
	"testing"

	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestNumeric_BranchTree(t *testing.T) {
	prog, want := branchTree(37, 96)
	require.NoError(t, program.Verify(prog))
	vm := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
	defer vm.Close()

	require.NoError(t, vm.Run(context.Background()))
	value, err := vm.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(want), value)
}

func BenchmarkNumeric_BranchTree(b *testing.B) {
	prog, want := branchTree(37, 96)
	require.NoError(b, program.Verify(prog))

	b.Run("threaded", func(b *testing.B) {
		ctx := context.Background()
		vm := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		defer vm.Close()
		require.NoError(b, vm.Run(ctx))
		value, err := vm.Pop()
		require.NoError(b, err)
		require.Equal(b, types.I32(want), value)
		vm.Reset()

		benchmarkRun(b, vm, types.BoxI32(want))
	})

	if runtime.GOARCH == "arm64" {
		b.Run("jit_warm", func(b *testing.B) {
			ctx := context.Background()
			cache := interp.NewCache(prog)
			defer cache.Close()
			profile := prof.New()
			warm := interp.New(prog, interp.WithCache(cache), interp.WithProfiler(profile), interp.WithTick(1), interp.WithThreshold(0))
			for range 2 {
				require.NoError(b, warm.Run(ctx))
				value, err := warm.Pop()
				require.NoError(b, err)
				require.Equal(b, types.I32(want), value)
				warm.Reset()
			}
			require.NoError(b, warm.Close())
			var emits, entries float64
			for _, metric := range profile.Metrics() {
				switch metric.Name {
				case "vm_jit_entry_emits_total":
					emits += metric.Value
				case "vm_jit_native_entries_total":
					entries += metric.Value
				}
			}
			require.Greater(b, emits, float64(0))
			require.Greater(b, entries, float64(0))

			vm := interp.New(prog, interp.WithCache(cache), interp.WithThreshold(0))
			defer vm.Close()
			require.NoError(b, vm.Run(ctx))
			value, err := vm.Pop()
			require.NoError(b, err)
			require.Equal(b, types.I32(want), value)
			vm.Reset()

			benchmarkRun(b, vm, types.BoxI32(want))
		})
	}

	b.Run("pool", func(b *testing.B) {
		ctx := context.Background()
		pool := interp.NewPool(prog, 1, interp.WithTick(1), interp.WithThreshold(-1))
		defer pool.Close()
		vm, err := pool.Get(ctx)
		require.NoError(b, err)
		require.NoError(b, vm.Run(ctx))
		value, err := vm.Pop()
		require.NoError(b, err)
		require.Equal(b, types.I32(want), value)
		pool.Put(vm)

		var runErr, popErr error
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			vm, err := pool.Get(ctx)
			if err != nil {
				runErr = err
				continue
			}
			runErr = vm.Run(ctx)
			value, popErr = vm.Pop()
			pool.Put(vm)
		}
		require.NoError(b, runErr)
		require.NoError(b, popErr)
		require.Equal(b, types.I32(want), value)
	})
}
