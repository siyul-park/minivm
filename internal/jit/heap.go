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

	// OffsetMapTyp is a Map's declared *types.MapType, whose KeyKind and
	// ElemKind bytes the map guard reads as one 16-bit word at
	// OffsetMapTypeKinds. A key kind alone does not fix the element: one Map
	// layout serves map[i32]string and map[i32]i32 alike.
	OffsetMapTyp       = unsafe.Offsetof(types.Map{}.Typ)
	OffsetMapTypeKinds = unsafe.Offsetof(types.MapType{}.KeyKind)
	// OffsetMapZero is the Boxed word MAP_GET returns for an absent key.
	OffsetMapZero = unsafe.Offsetof(types.Map{}.Zero)
)

// Control bytes of a table slot, and the hash multiplier. An empty slot is 0, a
// dead one SlotDead; a full one is SlotFull with the key's seven tag bits
// below it.
const (
	SlotFull = 0x80
	SlotDead = 1
	MapHash  = 0x9E3779B97F4A7C15
)

// Offsets native code reads and writes in a *types.Map: the
// words of its pointer-free table (types/map.go owns the layout). They are
// taken from reflection, since the table is unexported, and TestHeap_Layout pins
// every value. Every key kind shares one layout.
var (
	// OffsetMapCtrl, OffsetMapSlots, OffsetMapOrder, and OffsetMapPos are the
	// data words of the control bytes, of the key and value words, of the
	// insertion order, and of its back-pointers: slot i has its state at
	// ctrl[i], its key word at slots[2i], and its value word at slots[2i+1];
	// order[p] is the slot of the p-th insert, and pos[i] the position of slot
	// i's latest insert.
	OffsetMapCtrl  = mapField("ctrl")
	OffsetMapSlots = mapField("slots")
	OffsetMapOrder = mapField("order")
	OffsetMapPos   = mapField("pos")
	// OffsetMapMask, OffsetMapShift, and OffsetMapSeed are the hash words: the
	// hash is (key ^ seed) * MapHash, the home slot is hash >> shift & mask,
	// and the tag is hash >> (shift-7) & 0x7F.
	OffsetMapMask  = mapField("mask")
	OffsetMapShift = mapField("shift")
	OffsetMapSeed  = mapField("seed")
	// OffsetMapCount is the live entry count, OffsetMapUsed the order
	// positions in use, which bounds the slots that are not empty.
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

// mapField is the offset of the named table field in a types.Map. A renamed
// field fails at start, not as a wrong read.
func mapField(name string) uintptr {
	table, ok := reflect.TypeFor[types.Map]().FieldByName("table")
	if !ok {
		panic("jit: types.Map has no table field")
	}
	field, ok := table.Type.FieldByName(name)
	if !ok {
		panic("jit: types table has no " + name + " field")
	}
	return table.Offset + field.Offset
}
