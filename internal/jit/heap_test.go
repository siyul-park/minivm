package jit_test

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/types"
)

func TestItab(t *testing.T) {
	t.Run("equals the first word of the interface", func(t *testing.T) {
		var v types.Value = (*types.Struct)(nil)
		word := *(*uintptr)(unsafe.Pointer(&v))

		require.Equal(t, word, jit.Itab(v))
	})

	t.Run("distinguishes concrete types sharing the same interface", func(t *testing.T) {
		array := jit.Itab(types.TypedArray[int32](nil))
		other := jit.Itab(types.TypedArray[int64](nil))
		structItab := jit.Itab((*types.Struct)(nil))

		require.NotZero(t, array)
		require.NotZero(t, other)
		require.NotZero(t, structItab)
		require.NotEqual(t, array, other)
		require.NotEqual(t, array, structItab)
	})
}

func TestHeap_Layout(t *testing.T) {
	require.Equal(t, unsafe.Offsetof(types.Array{}.Elems), jit.OffsetArrayElems)
	require.Equal(t, unsafe.Offsetof(types.Struct{}.Typ), jit.OffsetStructTyp)
	require.Equal(t, unsafe.Offsetof(types.Struct{}.Data), jit.OffsetStructData)
	require.Equal(t, unsafe.Offsetof(types.StructType{}.Fields), jit.OffsetStructTypeFields)
	require.Equal(t, unsafe.Sizeof(struct{ a, b uintptr }{}), jit.SizeofValue)
}
