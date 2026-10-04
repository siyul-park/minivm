package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return nQueens(7) }
	registry.Register(registry.Spec{
		Name:   "n-queens",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}
func nqueensSolve(row, n int32, cols, diag1, diag2 []bool) int32 {
	if row == n {
		return 1
	}
	var count int32
	for col := int32(0); col < n; col++ {
		d1 := row - col + n - 1
		d2 := row + col
		if !cols[col] && !diag1[d1] && !diag2[d2] {
			cols[col] = true
			diag1[d1] = true
			diag2[d2] = true
			count += nqueensSolve(row+1, n, cols, diag1, diag2)
			cols[col] = false
			diag1[d1] = false
			diag2[d2] = false
		}
	}
	return count
}

func nQueens(n int32) int32 {
	cols := make([]bool, n)
	diag1 := make([]bool, 2*n-1)
	diag2 := make([]bool, 2*n-1)
	return nqueensSolve(0, n, cols, diag1, diag2)
}
