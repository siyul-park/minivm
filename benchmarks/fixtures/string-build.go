package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return stringBuild(512) }
	registry.Register(registry.Spec{
		Name:   "string-build",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}

func stringBuild(n int32) int32 {
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

	var big string
	var tokenChecksum int32
	for i := int32(0); i < n; i++ {
		tok := digits(int32(int64(i) * 2654435761 % 99999))
		for j := 0; j < len(tok); j++ {
			tokenChecksum += int32(tok[j]) * int32(j+1)
		}
		tokenChecksum %= 1000000007
		big = big + tok + " "
	}
	return tokenChecksum + int32(len(big))
}
