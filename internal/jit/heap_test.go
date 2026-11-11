package jit_test

import (
	"reflect"
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

	t.Run("boxes I64 with a data word that is itself a pointer to the value", func(t *testing.T) {
		var v types.Value = types.I64(1 << 50)
		type face struct {
			tab  uintptr
			data unsafe.Pointer
		}
		f := (*face)(unsafe.Pointer(&v))

		require.Equal(t, f.tab, jit.Itab(v))
		require.Equal(t, int64(1<<50), *(*int64)(f.data))
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
	text := "hello"
	require.Equal(t, len(text), *(*int)(unsafe.Add(unsafe.Pointer(&text), jit.OffsetStringLen)))
}

func TestMap_Layout(t *testing.T) {
	typ := types.NewMapType(types.TypeI32(), types.TypeI32())

	t.Run("pins every table field to its offset", func(t *testing.T) {
		require.Equal(t, uintptr(8), jit.OffsetMapZero)
		require.Equal(t, uintptr(16), jit.OffsetMapCtrl)
		require.Equal(t, uintptr(40), jit.OffsetMapSlots)
		require.Equal(t, uintptr(64), jit.OffsetMapMask)
		require.Equal(t, uintptr(72), jit.OffsetMapShift)
		require.Equal(t, uintptr(80), jit.OffsetMapSeed)
		require.Equal(t, uintptr(88), jit.OffsetMapCount)
		require.Equal(t, uintptr(96), jit.OffsetMapUsed)
	})

	t.Run("reads the fields of every key kind's table", func(t *testing.T) {
		keys := []types.Value{
			types.NewTypedMap[bool](typ, 0), types.NewTypedMap[int8](typ, 0), types.NewTypedMap[int32](typ, 0), types.NewTypedMap[int64](typ, 0),
		}
		for _, m := range keys {
			v := reflect.ValueOf(m).Elem()
			table := v.FieldByName("table")
			base := unsafe.Pointer(reflect.ValueOf(m).Pointer())
			word := func(offset uintptr) uint64 { return *(*uint64)(unsafe.Add(base, offset)) }

			require.Equal(t, uint64(v.FieldByName("Zero").Uint()), word(jit.OffsetMapZero))
			require.Equal(t, uint64(table.FieldByName("mask").Uint()), word(jit.OffsetMapMask))
			require.Equal(t, uint64(table.FieldByName("shift").Uint()), word(jit.OffsetMapShift))
			require.Equal(t, uint64(table.FieldByName("seed").Uint()), word(jit.OffsetMapSeed))
			require.Equal(t, uint64(table.FieldByName("count").Int()), word(jit.OffsetMapCount))
			require.Equal(t, uint64(table.FieldByName("used").Int()), word(jit.OffsetMapUsed))
			require.Equal(t, uint64(table.FieldByName("ctrl").Pointer()), word(jit.OffsetMapCtrl))
			require.Equal(t, uint64(table.FieldByName("slots").Pointer()), word(jit.OffsetMapSlots))
		}
	})

	t.Run("states and hashes as the table does", func(t *testing.T) {
		m := types.NewTypedMap[int64](typ, 0)
		const key = 12345
		m.Set(key, types.BoxI32(7))
		table := reflect.ValueOf(m).Elem().FieldByName("table")
		seed := table.FieldByName("seed").Uint()
		home := ((uint64(key) ^ seed) * jit.MapHash >> table.FieldByName("shift").Uint()) & table.FieldByName("mask").Uint()

		ctrl := table.FieldByName("ctrl")
		slots := table.FieldByName("slots")
		require.Equal(t, uint64(jit.SlotFull), ctrl.Index(int(home)).Uint())
		require.Equal(t, uint64(key), slots.Index(int(2*home)).Uint())
		require.Equal(t, uint64(types.BoxI32(7)), slots.Index(int(2*home+1)).Uint())

		m.Delete(key)
		require.Equal(t, uint64(jit.SlotDead), table.FieldByName("ctrl").Index(int(home)).Uint())
		require.Zero(t, jit.SlotEmpty)
	})
}
