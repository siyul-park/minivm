package bench

import (
	"context"
	"testing"

	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

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
		got, err := vm.Pop()
		require.NoError(b, err)
		require.InDelta(b, score(row), float64(got.(types.F64)), 1e-9)
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
		got, err = vm.Pop()
		require.NoError(b, err)
		require.InDelta(b, score(row), float64(got.(types.F64)), 1e-9)
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
		got, err := vm.Pop()
		require.NoError(b, err)
		require.InDelta(b, score(row), float64(got.(types.F64)), 1e-9)
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
		got, err = vm.Pop()
		require.NoError(b, err)
		require.InDelta(b, score(row), float64(got.(types.F64)), 1e-9)
	})
}
