package fixtures

import (
	"strconv"

	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return mapString(256, 16384) }
	registry.Register(registry.Spec{
		Name:   "map-string",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}

func mapString(count int32, rounds int32) int32 {
	token := func(index int32) string {
		return strconv.Itoa(int(index*7919%65521)) + "-" + strconv.Itoa(int(index))
	}
	words := make([]string, count)
	probes := make([]string, count)
	absent := make([]string, count)
	for index := int32(0); index < count; index++ {
		words[index] = token(index)
		probes[index] = token(index)
		absent[index] = token(index + count)
	}

	counts := make(map[string]int32, 64)
	for index := int32(0); index < count; index++ {
		counts[words[index]] = index
	}
	counts["total"] = 0

	var sum int32
	for round := int32(0); round < rounds; round++ {
		probe := probes[round*7919%count]
		counts[probe] = counts[probe] + 1
		counts["total"] = counts["total"] + 1
		if _, ok := counts[absent[round%count]]; ok {
			sum += 1000
		}
	}
	for index := int32(0); index < count; index++ {
		sum += counts[words[index]]
	}
	return sum + counts["total"]*3 + int32(len(counts))
}
