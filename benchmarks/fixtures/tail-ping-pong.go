package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return tailPingPong() }
	registry.Register(registry.Spec{
		Name:   "tail-ping-pong",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}

func tailPing(n, sum int32) int32 {
	if n == 0 {
		return sum
	}
	return pong(n-1, sum+n)
}

func pong(n, sum int32) int32 {
	if n == 0 {
		return sum
	}
	return tailPing(n-1, sum+n)
}

func tailPingPong() int32 {
	return tailPing(1000, 0)
}
