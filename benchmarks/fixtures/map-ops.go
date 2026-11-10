package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return mapOps(256) }
	registry.Register(registry.Spec{
		Name:   "map-ops",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}

func mapOps(count int32) int32 {
	key := func(index int32) int32 { return 1<<27 + index*7919%65521 }
	entries := make(map[int32]int32, count)
	for index := int32(0); index < count; index++ {
		entries[key(index)] = index
	}

	var sum int32
	for index := int32(0); index < count; index++ {
		value, ok := entries[key(index)+index%2*65521]
		if ok {
			sum += value + 1
		}
	}
	for index := int32(0); index < count; index += 2 {
		entries[key(index)] = index*3 + 1
	}
	for index := int32(0); index < count; index += 4 {
		delete(entries, key(index))
	}
	for index := int32(0); index < count; index++ {
		sum += entries[key(index)]
	}
	return sum*1024 + int32(len(entries))
}
