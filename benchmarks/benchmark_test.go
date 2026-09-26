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
		want types.Value
	}{
		{"IterativeFib", iterativeFib(30), types.I32(832040)},
		{"Sieve", sieve(256), types.I32(54)},
		{"RecursiveFib20", recursiveFib(20), types.I32(6765)},
		{"RecursiveFib35", recursiveFib(35), types.I32(9227465)},
		{"IndirectRecursiveFib", indirectRecursiveFib(20), types.I32(6765)},
		{"TailSum", tailSum(1000), types.I32(500500)},
		{"TailPingPong", tailPingPong(1000), types.I32(500500)},
		{"ClosureCounter", closureCounter(128), types.I32(128)},
		{"NQueens", nqueens(7), types.I32(40)},
		{"Fannkuch", fannkuch(6), types.I32(fannkuchReference(6))},
		{"TypedArraySum", typedArraySum(256), types.I32(32896)},
		{"AllocationGraph", allocationGraph(128), types.I32(128)},
		{"PermutationFlips", permutationFlips(24, 64), types.I32(1472)},
		{"StructTreeWalk", structTreeWalk(9), types.I32(1023)},
		{"BinaryTrees", binaryTrees(4, 6), types.I32(binaryTreesReference(4, 6))},
		{"SortStress", sortStress(128, 2), types.I32(sortStressReference(128, 2))},
		{"StringBuild", stringBuild(512), types.I32(stringBuildReference(512))},
		{"BranchTree", branch, types.I32(branchWant)},
		{"NBody", nbody(100), types.I32(nbodyReference(100))},
		{"SpectralNorm", spectralnorm(24, 2), types.I32(spectralnormReference(24, 2))},
		{"Mandelbrot", mandelbrot(16, 16, 50), types.I32(mandelbrotReference(16, 16, 50))},
		{"MatMul", matmul(16), types.I32(matmulReference(16))},
		{"I64WideFib", i64WideFib(25), types.I64(i64WideFibReference(25))},
		{"FNV1a64", fnv1a64(1024), types.I64(fnv1a64Reference(1024))},
		{"XorShiftI64", xorShiftI64(256), types.I64(xorShiftI64Reference(256))},
	}
	modes := []struct {
		name string
		opts []interp.Option
	}{
		{name: "threaded"},
		{name: "jit", opts: []interp.Option{interp.WithThreshold(0)}},
	}
	// A jit run must also cover native code published after the first runs
	// and promoted to Optimized: at least minimum rounds, then more until
	// maximum rounds or budget elapses.
	const minimum, maximum, budget = 5, 50, 200 * time.Millisecond
	for _, kernel := range kernels {
		for _, mode := range modes {
			name, prog, want, opts := kernel.name+"/"+mode.name, kernel.prog, kernel.want, mode.opts
			t.Run(name, func(t *testing.T) {
				require.NoError(t, program.Verify(prog))
				vm := interp.New(prog, opts...)
				defer vm.Close()
				start := time.Now()
				for round := 0; round < minimum || (round < maximum && time.Since(start) < budget); round++ {
					require.NoError(t, vm.Run(t.Context()))
					value, err := vm.PopBoxed()
					require.NoError(t, err)
					require.Equal(t, want, resolve(t, vm, value))
					vm.Reset()
				}
			})
		}
	}
}

func benchmarkVM(b *testing.B, prog *program.Program, want types.Value) {
	b.Helper()
	modes := []struct {
		name string
		opts []interp.Option
	}{
		{name: "threaded"},
		{name: "jit", opts: []interp.Option{interp.WithThreshold(0)}},
	}
	for _, mode := range modes {
		b.Run(mode.name, func(b *testing.B) {
			vm := interp.New(prog, mode.opts...)
			defer vm.Close()
			ctx := context.Background()

			require.NoError(b, vm.Run(ctx))
			value, err := vm.PopBoxed()
			require.NoError(b, err)
			require.Equal(b, want, resolve(b, vm, value))
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
				require.Equal(b, want, resolve(b, vm, value))
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
			// Reset runs before the next iteration's Run rather than right
			// after Pop, so a wide result's heap ref (Pop transfers, not
			// releases, it) stays live past the loop for the final check
			// below; the allocation profile per iteration is unchanged.
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

// resolve converts a PopBoxed word into its types.Value, dereferencing a
// KindRef through vm's heap (a wide i64 result lives there, since inline i64
// is a 49-bit payload). PopBoxed transfers a KindRef's ownership to the
// caller, same as Pop; Load then Release keeps that transfer balanced.
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
