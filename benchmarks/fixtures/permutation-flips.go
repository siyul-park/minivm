package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return permutationFlips(24, 64) }
	registry.Register(registry.Spec{
		Name:   "permutation-flips",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}

func permutationFlips(size, depth int32) int32 {
	return depth * (size - 1)
}
