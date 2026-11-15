package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return naiveChecksum(2048) }
	registry.Register(registry.Spec{
		Name:   "naive-checksum",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}

func naiveChecksum(n int32) int32 {
	var sum int32
	for i := int32(0); i < n; i++ {
		sum = sum*31 + 2*(i%1024)*(i%97) + i
	}
	return sum
}
