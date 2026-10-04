package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return allocationGraph(128) }
	registry.Register(registry.Spec{
		Name:   "allocation-graph",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}

func allocationGraph(depth int32) int32 {
	type node struct{ next *node }
	root := &node{}
	for index := int32(1); index < depth; index++ {
		root = &node{next: root}
	}
	if root == nil {
		return 0
	}
	return depth
}
