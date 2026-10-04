package fixtures

import (
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return sortStress(128, 2) }
	registry.Register(registry.Spec{
		Name:   "sort-stress",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}

func sortStress(n, rounds int32) int32 {
	makeList := func(n, seed int32) []int32 {
		xs := make([]int32, n)
		s := int64(seed)
		for i := int32(0); i < n; i++ {
			s = (s*1103515245 + 12345) % 2147483648
			xs[i] = int32(s % 1000000)
		}
		return xs
	}
	insertionSort := func(arr []int32) {
		for i := 1; i < len(arr); i++ {
			key := arr[i]
			j := i - 1
			for j >= 0 && arr[j] > key {
				arr[j+1] = arr[j]
				j--
			}
			arr[j+1] = key
		}
	}

	var checksum int32
	const seed int32 = 1
	for r := int32(0); r < rounds; r++ {
		xs := makeList(n, seed+r)
		insertionSort(xs)
		for i := int32(0); i < n; i++ {
			checksum += xs[i] * (i % 7)
		}
		checksum %= 1000000007
	}
	return checksum
}
