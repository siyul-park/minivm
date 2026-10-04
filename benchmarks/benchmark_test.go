package benchmarks_test

import (
	"context"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	_ "github.com/siyul-park/minivm/benchmarks/fixtures"
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestKernels(t *testing.T) {
	for _, spec := range registry.All() {
		spec := spec
		t.Run(spec.Name, func(t *testing.T) {
			prog := readProgram(spec)
			require.NoError(t, program.Verify(prog))
			result := spec.Result()
			for _, mode := range []struct {
				name string
				opts []interp.Option
			}{
				{name: "threaded", opts: []interp.Option{interp.WithThreshold(-1)}},
				{name: "jit"},
			} {
				t.Run(mode.name, func(t *testing.T) {
					vm := interp.New(prog, mode.opts...)
					defer vm.Close()
					const minimum = 5
					const maximum = 50
					const budget = 200 * time.Millisecond
					start := time.Now()
					for round := 0; round < minimum || (round < maximum && time.Since(start) < budget); round++ {
						require.NoError(t, vm.Run(t.Context()))
						got, err := vm.PopBoxed()
						require.NoError(t, err)
						require.Equal(t, result, resolve(t, vm, got))
						vm.Reset()
					}
				})
			}
		})
	}
}

func BenchmarkKernels(b *testing.B) {
	for _, spec := range registry.All() {
		spec := spec
		b.Run(spec.Name, func(b *testing.B) {
			prog := readProgram(spec)
			result := spec.Result()
			benchmarkVM(b, prog, result)
			benchmarkNative(b, spec, result)
			benchmarkExternal(b, spec, result)
		})
	}
}

func readProgram(spec registry.Spec) *program.Program {
	if spec.Source.MVM != "" {
		prog, err := program.Parse(strings.NewReader(spec.Source.MVM))
		if err != nil {
			panic("parse fixture " + spec.Name + ".mvm: " + err.Error())
		}
		return prog
	}
	if spec.Program == nil {
		panic("missing minivm fixture for " + spec.Name)
	}
	return spec.Program()
}

func benchmarkVM(b *testing.B, prog *program.Program, want types.Value) {
	b.Helper()
	for _, mode := range []struct {
		name string
		opts []interp.Option
	}{
		{name: "threaded", opts: []interp.Option{interp.WithThreshold(-1)}},
		{name: "jit"},
	} {
		b.Run(mode.name, func(b *testing.B) {
			vm := interp.New(prog, mode.opts...)
			defer vm.Close()
			ctx := context.Background()

			require.NoError(b, vm.Run(ctx))
			got, err := vm.PopBoxed()
			require.NoError(b, err)
			require.Equal(b, want, resolve(b, vm, got))
			vm.Reset()

			const warmups = 4
			const allocations = 32
			byteSamples := make([]uint64, 0, allocations)
			allocSamples := make([]uint64, 0, allocations)
			for index := range warmups + allocations {
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				require.NoError(b, vm.Run(ctx))
				runtime.ReadMemStats(&after)
				got, err := vm.PopBoxed()
				require.NoError(b, err)
				require.Equal(b, want, resolve(b, vm, got))
				vm.Reset()
				if index >= warmups {
					byteSamples = append(byteSamples, after.TotalAlloc-before.TotalAlloc)
					allocSamples = append(allocSamples, after.Mallocs-before.Mallocs)
				}
			}
			slices.Sort(byteSamples)
			slices.Sort(allocSamples)
			bytes := byteSamples[len(byteSamples)/2]
			allocs := allocSamples[len(allocSamples)/2]

			const samples = 4096
			var overhead time.Duration
			for range samples {
				start := time.Now()
				overhead += time.Since(start)
			}
			overhead /= samples

			var runErr, popErr error
			var elapsed time.Duration
			var value types.Boxed
			b.ReportAllocs()
			b.ResetTimer()
			for first := true; b.Loop(); first = false {
				if !first {
					vm.Reset()
				}
				start := time.Now()
				runErr = vm.Run(ctx)
				elapsed += time.Since(start)
				if runErr != nil {
					break
				}
				value, popErr = vm.PopBoxed()
				if popErr != nil {
					break
				}
			}
			elapsed -= min(elapsed, overhead*time.Duration(b.N))
			b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
			b.ReportMetric(float64(bytes), "B/op")
			b.ReportMetric(float64(allocs), "allocs/op")
			require.NoError(b, runErr)
			require.NoError(b, popErr)
			require.Equal(b, want, resolve(b, vm, value))
		})
	}
}

func benchmarkNative(b *testing.B, spec registry.Spec, result types.Value) {
	switch result.Kind() {
	case types.KindI64:
		if spec.Native.I64 != nil {
			want, ok := result.(types.I64)
			require.True(b, ok)
			benchmarkInt64(b, spec.Native.I64, want)
		}
	default:
		if spec.Native.I32 != nil {
			want, ok := result.(types.I32)
			require.True(b, ok)
			benchmarkInt32(b, spec.Native.I32, want)
		}
	}
}

func benchmarkInt32(b *testing.B, run func() int32, want types.I32) {
	b.Run("native", func(b *testing.B) {
		require.Equal(b, int32(want), run())
		var value int32
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			value = run()
		}
		b.StopTimer()
		require.Equal(b, int32(want), value)
	})
}

func benchmarkInt64(b *testing.B, run func() int64, want types.I64) {
	b.Run("native", func(b *testing.B) {
		require.Equal(b, int64(want), run())
		var value int64
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			value = run()
		}
		b.StopTimer()
		require.Equal(b, int64(want), value)
	})
}

func resolve(tb testing.TB, vm *interp.Interpreter, got types.Boxed) types.Value {
	tb.Helper()
	if got.Kind() != types.KindRef {
		return types.Unbox(got)
	}
	value, err := vm.Load(got.Ref())
	require.NoError(tb, err)
	require.NoError(tb, vm.Release(got.Ref()))
	return value
}
