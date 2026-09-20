package benchmarks_test

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
	cpython   string
	yaegi     string
}

var benchmarkCompare = func(*testing.B, benchmarkComparison, int32) {}

func TestKernels(t *testing.T) {
	branch, branchWant := branchTree(37, 96)
	kernels := []struct {
		name string
		prog *program.Program
		want int32
	}{
		{"IterativeFib", iterativeFib(30), 832040},
		{"Sieve", sieve(256), 54},
		{"RecursiveFib20", recursiveFib(20), 6765},
		{"RecursiveFib35", recursiveFib(35), 9227465},
		{"IndirectRecursiveFib", indirectRecursiveFib(20), 6765},
		{"TailSum", tailSum(1000), 500500},
		{"TailPingPong", tailPingPong(1000), 500500},
		{"ClosureCounter", closureCounter(128), 128},
		{"NQueens", nqueens(7), 40},
		{"Fannkuch", fannkuch(6), fannkuchReference(6)},
		{"TypedArraySum", typedArraySum(256), 32896},
		{"AllocationGraph", allocationGraph(128), 128},
		{"PermutationFlips", permutationFlips(24, 64), 1472},
		{"StructTreeWalk", structTreeWalk(9), 1023},
		{"BinaryTrees", binaryTrees(4, 6), binaryTreesReference(4, 6)},
		{"SortStress", sortStress(128, 2), sortStressReference(128, 2)},
		{"StringBuild", stringBuild(512), stringBuildReference(512)},
		{"BranchTree", branch, branchWant},
		{"NBody", nbody(100), nbodyReference(100)},
		{"SpectralNorm", spectralnorm(24, 2), spectralnormReference(24, 2)},
		{"Mandelbrot", mandelbrot(16, 16, 50), mandelbrotReference(16, 16, 50)},
		{"MatMul", matmul(16), matmulReference(16)},
	}
	modes := []struct {
		name string
		opts []interp.Option
	}{
		{name: "threaded"},
	}
	for _, kernel := range kernels {
		for _, mode := range modes {
			name, prog, want, opts := kernel.name+"/"+mode.name, kernel.prog, kernel.want, mode.opts
			t.Run(name, func(t *testing.T) {
				require.NoError(t, program.Verify(prog))
				vm := interp.New(prog, opts...)
				defer vm.Close()
				for range 2 {
					require.NoError(t, vm.Run(t.Context()))
					value, err := vm.PopBoxed()
					require.NoError(t, err)
					require.Equal(t, types.BoxI32(want), value)
					vm.Reset()
				}
			})
		}
	}
}

func benchmarkVM(b *testing.B, prog *program.Program, want types.Boxed) {
	b.Helper()
	modes := []struct {
		name string
		opts []interp.Option
	}{
		{name: "threaded"},
	}
	for _, mode := range modes {
		b.Run(mode.name, func(b *testing.B) {
			vm := interp.New(prog, mode.opts...)
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
