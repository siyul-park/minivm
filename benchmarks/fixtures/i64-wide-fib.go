package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int64 { return i64WideFib(25) }
	registry.Register(registry.Spec{
		Name:   "i64-wide-fib",
		Result: func() types.Value { return types.I64(run()) },
		Native: registry.Native{I64: run},
	})
}

func i64WideFib(n int64) int64 {
	if n < 2 {
		return n + (1 << 50)
	}
	return i64WideFib(n-1) + i64WideFib(n-2)
}
