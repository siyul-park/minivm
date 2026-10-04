package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return structTreeWalk(9) }
	registry.Register(registry.Spec{
		Name:   "struct-tree-walk",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}

func structTreeWalk(depth int32) int32 {
	return int32(1<<uint(depth+1)) - 1
}
