package bench

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/siyul-park/minivm/interp"
	"github.com/stretchr/testify/require"
)

func TestBranchTree(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}
	for _, trees := range []int{2, 30} {
		t.Run(fmt.Sprintf("trees_%d", trees), func(t *testing.T) {
			prog, row, score := branchyBatchTreeEvaluation(trees)
			vm := interp.New(
				prog,
				interp.WithTick(1),
				interp.WithThreshold(1),
			)
			defer vm.Close()
			ctx := context.Background()

			for n := 0; n < 256; n++ {
				for idx := range row {
					row[idx] = float64((n*13+idx*7)%19) / 19
				}
				require.NoError(t, vm.Run(ctx))
				got, err := vm.PopBoxed()
				require.NoError(t, err)
				require.InDelta(t, score(row), got.F64(), 1e-9, "warmup row %d", n)
				vm.Reset()
			}

			for n := 0; n < 128; n++ {
				for idx := range row {
					row[idx] = float64(((n+256)*13+idx*7)%19) / 19
				}
				require.NoError(t, vm.Run(ctx))
				got, err := vm.PopBoxed()
				require.NoError(t, err)
				require.InDelta(t, score(row), got.F64(), 1e-9, "check row %d", n)
				vm.Reset()
			}
		})
	}
}

// BenchmarkJITIssue101 tracks a LightGBM-style branchy batch path: many tiny
// tree-score functions called over one mutable f64 feature row.
func BenchmarkJITIssue101(b *testing.B) {
	prog, row, score := branchyBatchTreeEvaluation(30)

	b.Run("interp", func(b *testing.B) {
		vm := interp.New(prog, interp.WithThreshold(-1))
		defer vm.Close()
		ctx := context.Background()

		for idx := range row {
			row[idx] = float64((1+idx*5)%17) / 16
		}
		require.NoError(b, vm.Run(ctx))
		got, err := vm.PopBoxed()
		require.NoError(b, err)
		require.InDelta(b, score(row), got.F64(), 1e-9)
		vm.Reset()

		for n := 0; n < 512; n++ {
			for idx := range row {
				row[idx] = float64((n+idx*5)%17) / 16
			}
			require.NoError(b, vm.Run(ctx))
			vm.Reset()
		}

		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			for idx := range row {
				row[idx] = float64((n+idx*5)%17) / 16
			}
			if err := vm.Run(ctx); err != nil {
				b.StopTimer()
				require.NoError(b, err)
			}
			vm.Reset()
		}
		b.StopTimer()

		for idx := range row {
			row[idx] = float64((b.N+17+idx*5)%17) / 16
		}
		require.NoError(b, vm.Run(ctx))
		got, err = vm.PopBoxed()
		require.NoError(b, err)
		require.InDelta(b, score(row), got.F64(), 1e-9)
	})

	b.Run("jit", func(b *testing.B) {
		vm := interp.New(
			prog,
			interp.WithTick(1),
			interp.WithThreshold(1),
		)
		defer vm.Close()
		ctx := context.Background()

		for idx := range row {
			row[idx] = float64((1+idx*5)%17) / 16
		}
		require.NoError(b, vm.Run(ctx))
		got, err := vm.PopBoxed()
		require.NoError(b, err)
		require.InDelta(b, score(row), got.F64(), 1e-9)
		vm.Reset()

		for n := 0; n < 512; n++ {
			for idx := range row {
				row[idx] = float64((n+idx*5)%17) / 16
			}
			require.NoError(b, vm.Run(ctx))
			vm.Reset()
		}

		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			for idx := range row {
				row[idx] = float64((n+idx*5)%17) / 16
			}
			if err := vm.Run(ctx); err != nil {
				b.StopTimer()
				require.NoError(b, err)
			}
			vm.Reset()
		}
		b.StopTimer()

		for idx := range row {
			row[idx] = float64((b.N+17+idx*5)%17) / 16
		}
		require.NoError(b, vm.Run(ctx))
		got, err = vm.PopBoxed()
		require.NoError(b, err)
		require.InDelta(b, score(row), got.F64(), 1e-9)
	})
}
