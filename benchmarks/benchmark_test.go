package benchmarks

import (
	"context"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

type benchmarkComparison struct {
	native  func() int32
	wazero  string
	args    []uint64
	scripts benchmarkScripts
	values  []int32
}

type benchmarkScripts struct {
	tengo     string
	gopherLua string
	goja      string
	gpython   string
	yaegi     string
}

var benchmarkCompare = func(*testing.B, benchmarkComparison, int32) {}

func benchmarkVM(b *testing.B, prog *program.Program, want types.Boxed) {
	b.Helper()
	modes := []struct {
		name      string
		threshold []int
	}{
		{name: "default"},
		{name: "threaded", threshold: []int{-1}},
		{name: "jit", threshold: []int{0}},
	}
	for _, mode := range modes {
		b.Run(mode.name, func(b *testing.B) {
			var vm *interp.Interpreter
			if len(mode.threshold) == 0 {
				vm = interp.New(prog)
			} else {
				vm = interp.New(prog, interp.WithThreshold(mode.threshold[0]))
			}
			defer vm.Close()
			ctx := context.Background()

			require.NoError(b, vm.Run(ctx))
			value, err := vm.PopBoxed()
			require.NoError(b, err)
			require.Equal(b, want, value)
			vm.Reset()

			const warmups = 4
			const allocations = 32
			byteSamples := make([]uint64, 0, allocations)
			allocSamples := make([]uint64, 0, allocations)
			for index := range warmups + allocations {
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				runErr := vm.Run(ctx)
				runtime.ReadMemStats(&after)
				require.NoError(b, runErr)
				value, popErr := vm.PopBoxed()
				require.NoError(b, popErr)
				require.Equal(b, want, value)
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
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
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
				vm.Reset()
			}
			elapsed -= min(elapsed, overhead*time.Duration(b.N))
			b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
			b.ReportMetric(float64(bytes), "B/op")
			b.ReportMetric(float64(allocs), "allocs/op")
			require.NoError(b, runErr)
			require.NoError(b, popErr)
			require.Equal(b, want, value)
		})
	}
}
