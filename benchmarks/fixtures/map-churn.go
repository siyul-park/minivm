package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return mapChurn(16384) }
	registry.Register(registry.Spec{
		Name:   "map-churn",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}

func mapChurn(count int32) int32 {
	key := func(index int32) int32 { return index * 7919 % 65521 }
	entries := make(map[int32]int32, 1024)

	var sum int32
	for index := int32(0); index < count; index++ {
		entries[key(index)] = index
		if index >= 512 {
			delete(entries, key(index-512))
		}
		if index >= 256 {
			sum += entries[key(index-256)]
		}
	}
	return sum + int32(len(entries))
}
