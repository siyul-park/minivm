package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return binaryTrees(4, 6) }
	registry.Register(registry.Spec{
		Name:   "binary-trees",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}

func binaryTrees(minDepth, maxDepth int32) int32 {
	type node struct {
		item        int32
		left, right *node
	}
	var build func(item, depth int32) *node
	build = func(item, depth int32) *node {
		n := &node{item: item}
		if depth > 0 {
			n.left = build(2*item-1, depth-1)
			n.right = build(2*item, depth-1)
		}
		return n
	}
	var check func(t *node) int32
	check = func(t *node) int32 {
		if t == nil {
			return 0
		}
		if t.left == nil {
			return t.item
		}
		return t.item + check(t.left) - check(t.right)
	}

	stretchTree := build(0, maxDepth+1)
	checksum := check(stretchTree)

	longLivedTree := build(0, maxDepth)

	for depth := minDepth; depth <= maxDepth; depth += 2 {
		iterations := int32(1)
		for shift := int32(0); shift < maxDepth-depth+minDepth; shift++ {
			iterations *= 2
		}
		var acc int32
		for i := int32(1); i <= iterations; i++ {
			acc += check(build(i, depth))
			acc += check(build(-i, depth))
		}
		checksum += acc
	}

	checksum += check(longLivedTree)
	return checksum
}
