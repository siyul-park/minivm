package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return tailSum(1000) }
	registry.Register(registry.Spec{
		Name:   "tail-sum",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}

func tailSum(n int32) int32 {
	sum := int32(0)
	for value := n; value > 0; value-- {
		sum += value
	}
	return sum
}
