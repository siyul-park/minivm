package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return matMul(16) }
	registry.Register(registry.Spec{
		Name:   "mat-mul",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}

func matMul(n int32) int32 {
	a := make([]float64, n*n)
	b := make([]float64, n*n)
	out := make([]float64, n*n)

	for i := int32(0); i < n; i++ {
		for j := int32(0); j < n; j++ {
			a[i*n+j] = float64((i*7+j*3)%13) - 6.0
			b[i*n+j] = float64((i*5+j*11)%17) - 8.0
		}
	}

	for i := int32(0); i < n; i++ {
		for j := int32(0); j < n; j++ {
			var s float64
			for k := int32(0); k < n; k++ {
				s += a[i*n+k] * b[k*n+j]
			}
			out[i*n+j] = s
		}
	}

	var checksum float64
	for idx := int32(0); idx < n*n; idx++ {
		checksum += out[idx]
	}
	return int32(checksum * 1e6)
}
