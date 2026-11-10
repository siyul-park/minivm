package fixtures

import (
	"iter"

	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return coroutineGen(512) }
	registry.Register(registry.Spec{
		Name:   "coroutine-gen",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}

func coroutineGen(count int32) int32 {
	generate := func(yield func(int32) bool) {
		for index := int32(0); index < count; index++ {
			if !yield((index*index + 7*index) % 1009) {
				return
			}
		}
	}
	next, stop := iter.Pull(iter.Seq[int32](generate))
	defer stop()

	var sum int32
	for {
		value, ok := next()
		if !ok {
			return sum
		}
		sum = (sum + value) % 65521
	}
}
