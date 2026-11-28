package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return mapIter(1024, 32) }
	registry.Register(registry.Spec{
		Name:   "map-iter",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}

func mapIter(count int32, passes int32) int32 {
	key := func(index int32) int32 { return index * 7919 % 65521 }
	entries := make(map[int32]int32, count)
	for index := int32(0); index < count; index++ {
		entries[key(index)] = index
	}
	for index := int32(0); index < count; index += 4 {
		delete(entries, key(index))
	}

	walk := func() int32 {
		var sum int32
		for k := range entries {
			sum += k + entries[k]
		}
		return sum
	}

	var sum int32
	for pass := int32(0); pass < passes; pass++ {
		sum += walk()
	}
	return sum + int32(len(entries))
}
