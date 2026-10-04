package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return mandelbrot(16, 16, 50) }
	registry.Register(registry.Spec{
		Name:   "mandelbrot",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}
func mandelbrotEscapeCount(cr, ci float64, maxIter int32) int32 {
	var zr, zi float64
	for i := int32(0); i < maxIter; i++ {
		zr2 := zr * zr
		zi2 := zi * zi
		if zr2+zi2 > 4.0 {
			return i
		}
		newZr := zr2 - zi2 + cr
		newZi := 2.0*zr*zi + ci
		zr = newZr
		zi = newZi
	}
	return maxIter
}

func mandelbrot(width, height, maxIter int32) int32 {
	const xMin, xMax = -2.0, 1.0
	const yMin, yMax = -1.5, 1.5

	var total int32
	for py := int32(0); py < height; py++ {
		cy := yMin + (yMax-yMin)*float64(py)/float64(height-1)
		for px := int32(0); px < width; px++ {
			cx := xMin + (xMax-xMin)*float64(px)/float64(width-1)
			total += mandelbrotEscapeCount(cx, cy, maxIter)
		}
	}
	return total
}
