package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int64 { return fnv1a64(1024) }
	registry.Register(registry.Spec{
		Name:   "fnv1a64",
		Result: func() types.Value { return types.I64(run()) },
		Native: registry.Native{I64: run},
	})
}

func fnv1a64(n int32) int64 {
	hi, lo := int64(0xcbf29ce4), int64(0x84222325)
	h := hi<<32 | lo
	const prime int64 = 1099511628211
	for i := int32(0); i < n; i++ {
		h ^= int64(i & 0xff)
		h *= prime
	}
	return h
}
