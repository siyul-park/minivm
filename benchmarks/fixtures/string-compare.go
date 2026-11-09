package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return stringCompare() }
	registry.Register(registry.Spec{
		Name:   "string-compare",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}

func stringCompare() int32 {
	digits := func(v int32) string {
		if v == 0 {
			return "0"
		}
		var buf []byte
		for v > 0 {
			d := v % 10
			buf = append([]byte{byte(48 + d)}, buf...)
			v /= 10
		}
		return string(buf)
	}

	words := make([]string, 64)
	for i := range words {
		words[i] = digits(int32(int64(i) * 2654435761 % 997))
	}

	var count int32
	for r := int32(0); r < 256; r++ {
		key := digits(r * 31 % 997)
		for i := range words {
			if words[i] == key {
				count += int32(i) + 1
			}
		}
	}
	return count
}
