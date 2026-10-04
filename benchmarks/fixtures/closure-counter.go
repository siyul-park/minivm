package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return closureCounter(128) }
	registry.Register(registry.Spec{
		Name:   "closure-counter",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}

func closureCounter(count int32) int32 {
	var value int32
	next := func() int32 {
		value++
		return value
	}
	for index := int32(0); index < count; index++ {
		value = next()
	}
	return value
}
