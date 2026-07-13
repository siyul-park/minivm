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

func TestMemory_TypedArraySum(t *testing.T) {
	prog := typedArraySum(256)
	require.NoError(t, program.Verify(prog))
	vm := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
	defer vm.Close()

	require.NoError(t, vm.Run(context.Background()))
	value, err := vm.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(typedArraySumReference(256)), value)
}

func TestMemory_AllocationGraph(t *testing.T) {
	const depth = 128
	prog := allocationGraph(depth)
	require.NoError(t, program.Verify(prog))
	vm := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
	defer vm.Close()

	require.NoError(t, vm.Run(context.Background()))
	root, err := vm.Local(0)
	require.NoError(t, err)
	ref := root.Ref()
	for index := 0; index < depth; index++ {
		value, err := vm.Load(ref)
		require.NoError(t, err)
		array, ok := value.(*types.Array)
		require.True(t, ok)
		require.Len(t, array.Elems, 1)
		if index+1 == depth {
			require.True(t, types.IsNull(array.Elems[0]))
			break
		}
		ref = array.Elems[0].Ref()
	}
	value, err := vm.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(depth), value)
}

func BenchmarkMemory_TypedArraySum(b *testing.B) {
	prog := typedArraySum(256)
	require.NoError(b, program.Verify(prog))

	b.Run("threaded", func(b *testing.B) {
		vm := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		defer vm.Close()
		require.NoError(b, vm.Run(context.Background()))
		value, err := vm.Pop()
		require.NoError(b, err)
		require.Equal(b, types.I32(typedArraySumReference(256)), value)
		vm.Reset()

		var runErr, popErr error
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			runErr = vm.Run(context.Background())
			b.StopTimer()
			value, popErr = vm.Pop()
			vm.Reset()
			b.StartTimer()
		}
		b.StopTimer()
		require.NoError(b, runErr)
		require.NoError(b, popErr)
		require.Equal(b, types.I32(typedArraySumReference(256)), value)
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
				require.Equal(b, types.I32(typedArraySumReference(256)), value)
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
			require.Equal(b, types.I32(typedArraySumReference(256)), value)
			vm.Reset()

			var runErr, popErr error
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				runErr = vm.Run(ctx)
				b.StopTimer()
				value, popErr = vm.Pop()
				vm.Reset()
				b.StartTimer()
			}
			b.StopTimer()
			require.NoError(b, runErr)
			require.NoError(b, popErr)
			require.Equal(b, types.I32(typedArraySumReference(256)), value)
		})
	}
}

func BenchmarkMemory_AllocationGraph(b *testing.B) {
	prog := allocationGraph(128)
	require.NoError(b, program.Verify(prog))

	b.Run("threaded", func(b *testing.B) {
		vm := interp.New(prog, interp.WithTick(1), interp.WithThreshold(-1))
		defer vm.Close()
		require.NoError(b, vm.Run(context.Background()))
		value, err := vm.Pop()
		require.NoError(b, err)
		require.Equal(b, types.I32(128), value)
		vm.Reset()

		var runErr, popErr error
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			runErr = vm.Run(context.Background())
			b.StopTimer()
			value, popErr = vm.Pop()
			vm.Reset()
			b.StartTimer()
		}
		b.StopTimer()
		require.NoError(b, runErr)
		require.NoError(b, popErr)
		require.Equal(b, types.I32(128), value)
	})
}
