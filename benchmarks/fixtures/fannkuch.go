package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return fannkuch(6) }
	registry.Register(registry.Spec{
		Name:   "fannkuch",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}
func fannkuchPermute(a []int32, k, permcount, checksum, maxflips int32) (int32, int32, int32) {
	if k == 1 {
		flips := fannkuchCountFlips(a)
		if flips > maxflips {
			maxflips = flips
		}
		if permcount%2 == 0 {
			checksum += flips
		} else {
			checksum -= flips
		}
		return permcount + 1, checksum, maxflips
	}
	for i := int32(0); i < k; i++ {
		permcount, checksum, maxflips = fannkuchPermute(a, k-1, permcount, checksum, maxflips)
		if k%2 == 0 {
			a[i], a[k-1] = a[k-1], a[i]
		} else {
			a[0], a[k-1] = a[k-1], a[0]
		}
	}
	return permcount, checksum, maxflips
}

func fannkuchCountFlips(perm []int32) int32 {
	a := append([]int32(nil), perm...)
	var flips int32
	k := a[0]
	for k != 0 {
		i, j := int32(0), k
		for i < j {
			a[i], a[j] = a[j], a[i]
			i++
			j--
		}
		flips++
		k = a[0]
	}
	return flips
}

func fannkuch(n int32) int32 {
	a := make([]int32, n)
	for i := range a {
		a[i] = int32(i)
	}
	_, checksum, maxflips := fannkuchPermute(a, n, 0, 0, 0)
	return checksum*1000 + maxflips
}
