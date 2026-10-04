package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int64 { return xorshiftI64(256) }
	registry.Register(registry.Spec{
		Name:   "xorshift-i64",
		Result: func() types.Value { return types.I64(run()) },
		Native: registry.Native{I64: run},
	})
}

func xorshiftI64(n int32) int64 {
	const seed int64 = 2463534242
	values := make([]int64, n)
	x := seed
	for i := int32(0); i < n; i++ {
		x ^= x << 13
		x ^= int64(uint64(x) >> 7)
		x ^= x << 17
		values[i] = x
	}
	var sum int64
	for _, v := range values {
		sum += v
	}
	return sum
}
