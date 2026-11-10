package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return hostCall(1024) }
	registry.Register(registry.Spec{
		Name:   "host-call",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
		Host:   &registry.Host{Name: "mix", I32: mix},
	})
}

func hostCall(count int32) int32 {
	var acc int32
	for index := int32(0); index < count; index++ {
		acc = mix(acc, index)
	}
	return acc
}

func mix(a, b int32) int32 {
	return (a*31 + b) % 1000003
}
