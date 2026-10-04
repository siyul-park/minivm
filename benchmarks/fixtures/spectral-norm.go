package fixtures

import (
	"math"

	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return spectralNorm(24, 2) }
	registry.Register(registry.Spec{
		Name:   "spectral-norm",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}
func spectralnormAtaTimesU(n int32, u, out, tmp []float64) {
	spectralnormATimesU(n, u, tmp)
	spectralnormAtTimesU(n, tmp, out)
}

func spectralnormATimesU(n int32, u, out []float64) {
	for i := int32(0); i < n; i++ {
		var s float64
		for j := int32(0); j < n; j++ {
			s += spectralnormEvalA(i, j) * u[j]
		}
		out[i] = s
	}
}

func spectralnormAtTimesU(n int32, u, out []float64) {
	for i := int32(0); i < n; i++ {
		var s float64
		for j := int32(0); j < n; j++ {
			s += spectralnormEvalA(j, i) * u[j]
		}
		out[i] = s
	}
}

func spectralnormEvalA(i, j int32) float64 {
	return 1.0 / float64((i+j)*(i+j+1)/2+i+1)
}

func spectralNorm(n, rounds int32) int32 {
	u := make([]float64, n)
	for i := range u {
		u[i] = 1.0
	}
	v := make([]float64, n)
	tmp := make([]float64, n)

	for it := int32(0); it < rounds; it++ {
		spectralnormAtaTimesU(n, u, v, tmp)
		spectralnormAtaTimesU(n, v, u, tmp)
	}

	var vbv, vv float64
	for i := int32(0); i < n; i++ {
		vbv += u[i] * v[i]
		vv += v[i] * v[i]
	}

	result := math.Sqrt(vbv / vv)
	return int32(result * 1e9)
}
