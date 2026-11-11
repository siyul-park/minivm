package jit

import (
	"reflect"
	"unsafe"

	"github.com/siyul-park/minivm/types"
)

// Offsets and sizes native code reads to interpret a heap object's own Go
// layout directly: the interpreter heap Context.Heap addresses is
// []types.Value, one SizeofValue interface word pair per address. Native
// code reads only these interface words and the object headers they point
// to, and writes only non-pointer element/field words — never a Go pointer,
// so the collector's own view of the heap never changes underneath it.
const (
	// SizeofValue is the size of one interpreter heap slot: the interface
	// pair (itab, data) types.Value stores.
	SizeofValue = unsafe.Sizeof(struct{ itab, data uintptr }{})
	// OffsetData is a heap slot's data word: the concrete value's own
	// pointer for a pointer-shaped Value (*types.Array, *types.Struct), or a
	// pointer to a heap-boxed slice header for a TypedArray[T].
	OffsetData = unsafe.Sizeof(uintptr(0))
	// OffsetSliceLen is a Go slice header's length word, whether boxed
	// (a TypedArray[T]'s data word points at one) or inline (Array.Elems,
	// Struct.Data).
	OffsetSliceLen = unsafe.Sizeof(uintptr(0))
	// OffsetStringLen is a Go string header's length word: a types.String
	// slot's data word points at a heap-boxed header.
	OffsetStringLen = unsafe.Sizeof(uintptr(0))

	OffsetArrayElems       = unsafe.Offsetof(types.Array{}.Elems)
	OffsetStructTyp        = unsafe.Offsetof(types.Struct{}.Typ)
	OffsetStructData       = unsafe.Offsetof(types.Struct{}.Data)
	OffsetStructTypeFields = unsafe.Offsetof(types.StructType{}.Fields)
	SizeofStructField      = unsafe.Sizeof(types.StructField{})
	OffsetStructFieldKind  = unsafe.Offsetof(types.StructField{}.Kind)
	OffsetClosureTyp       = unsafe.Offsetof(types.Closure{}.Typ)
	OffsetClosureFn        = unsafe.Offsetof(types.Closure{}.Fn)
	OffsetClosureUpvals    = unsafe.Offsetof(types.Closure{}.Upvals)
)

// Slot states a table's control byte holds, and the home slot's multiplier.
const (
	SlotEmpty = 0
	SlotFull  = 1
	SlotDead  = 2
	MapHash   = 0x9E3779B97F4A7C15
)

// Offsets native code reads and writes in a numeric-keyed *types.TypedMap: the
// words of its pointer-free table (types/map.go owns the layout). They are
// taken from reflection, since the table is unexported, and TestMap_Layout pins
// every value. Every key kind shares one layout: only the methods differ.
var (
	// OffsetMapZero is the Boxed word MAP_GET returns for an absent key.
	OffsetMapZero = unsafe.Offsetof(types.TypedMap[int64]{}.Zero)
	// OffsetMapCtrl and OffsetMapSlots are the data words of the control bytes
	// and of the key and value words: slot i has its state at ctrl[i], its key
	// word at slots[2i], and its value word at slots[2i+1].
	OffsetMapCtrl  = mapField("ctrl")
	OffsetMapSlots = mapField("slots")
	// OffsetMapMask, OffsetMapShift, and OffsetMapSeed are the home slot's
	// words: ((key ^ seed) * MapHash >> shift) & mask.
	OffsetMapMask  = mapField("mask")
	OffsetMapShift = mapField("shift")
	OffsetMapSeed  = mapField("seed")
	// OffsetMapCount is the live entry count, OffsetMapUsed the slots that
	// are not empty, tombstones included.
	OffsetMapCount = mapField("count")
	OffsetMapUsed  = mapField("used")
)

// Itab returns the first word of the interface v holds: the runtime
// itab/type-descriptor word identifying v's concrete type as a types.Value.
// Native code compares a heap slot's own first word against this to test its
// dynamic representation without reading through a live reference (L11):
// the comparison value is computed here, ahead of time, in Go.
func Itab(v types.Value) uintptr {
	type face struct{ tab, data uintptr }
	return (*face)(unsafe.Pointer(&v)).tab
}

// mapField is the offset of the named table field in a TypedMap. A renamed
// field fails at start, not as a wrong read.
func mapField(name string) uintptr {
	table, ok := reflect.TypeFor[types.TypedMap[int64]]().FieldByName("table")
	if !ok {
		panic("jit: types.TypedMap has no table field")
	}
	field, ok := table.Type.FieldByName(name)
	if !ok {
		panic("jit: types table has no " + name + " field")
	}
	return table.Offset + field.Offset
}
