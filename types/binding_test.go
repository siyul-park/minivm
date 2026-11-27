package types_test

import (
	"testing"

	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestBinding_Heap(t *testing.T) {
	heap := types.NewHeap(1, 0, types.Hooks{})
	arrayType := types.NewArrayType(types.TypeAny())
	structType := types.NewStructType(types.NewStructField(types.TypeAny()))
	mapType := types.NewMapType(types.TypeI32(), types.TypeI32())

	for name, tt := range map[string]struct {
		bound, unbound interface{ Heap() *types.Heap }
	}{
		"array":  {types.NewArray(heap, arrayType, 0), types.NewArray(nil, arrayType, 0)},
		"struct": {types.NewStruct(heap, structType), types.NewStruct(nil, structType)},
		"map":    {types.NewMap(heap, mapType, 0), types.NewMap(nil, mapType, 0)},
	} {
		t.Run(name, func(t *testing.T) {
			require.Same(t, heap, tt.bound.Heap())
			require.Nil(t, tt.unbound.Heap())
		})
	}
}
