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
	// (a TypedArray[T]'s data word points at one) or inline (an Array's
	// elements).
	OffsetSliceLen = unsafe.Sizeof(uintptr(0))
	// OffsetStringLen is a Go string header's length word: a types.String
	// slot's data word points at a heap-boxed header.
	OffsetStringLen = unsafe.Sizeof(uintptr(0))

	OffsetStructTyp        = unsafe.Offsetof(types.Struct{}.Typ)
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

// Offsets of unexported storage, taken from reflection; TestHeap_Layout pins
// every value.
var (
	// OffsetArrayElems is an Array's element slice header.
	OffsetArrayElems = offset[types.Array]("elems")
	// OffsetStructData is a Struct's pointer to its first field word; the
	// fields of its type fix how many follow.
	OffsetStructData = offset[types.Struct]("data")

	// The words of a Map's pointer-free table (types/map.go owns the layout),
	// which native code reads and writes. Every key kind shares one layout.
	//
	// OffsetMapCtrl, OffsetMapSlots, OffsetMapOrder, and OffsetMapPos are the
	// data words of the control bytes, of the key and value words, of the
	// insertion order, and of its back-pointers: slot i has its state at
	// ctrl[i], its key word at slots[2i], and its value word at slots[2i+1];
	// order[p] is the slot of the p-th insert, and pos[i] the position of slot
	// i's latest insert.
	OffsetMapCtrl  = offset[types.Map]("table", "ctrl")
	OffsetMapSlots = offset[types.Map]("table", "slots")
	OffsetMapOrder = offset[types.Map]("table", "order")
	OffsetMapPos   = offset[types.Map]("table", "pos")
	// OffsetMapMask, OffsetMapShift, and OffsetMapSeed are the hash words: the
	// hash is (key ^ seed) * MapHash, the home slot is hash >> shift & mask,
	// and the tag is hash >> (shift-7) & 0x7F.
	OffsetMapMask  = offset[types.Map]("table", "mask")
	OffsetMapShift = offset[types.Map]("table", "shift")
	OffsetMapSeed  = offset[types.Map]("table", "seed")
	// OffsetMapCount is the live entry count, OffsetMapUsed the order
	// positions in use, which bounds the slots that are not empty.
	OffsetMapCount = offset[types.Map]("table", "count")
	OffsetMapUsed  = offset[types.Map]("table", "used")
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

// offset is the offset in T of the field path names, each name a field of the
// one before it. A renamed field fails at start, not as a wrong read.
func offset[T any](path ...string) uintptr {
	typ, at := reflect.TypeFor[T](), uintptr(0)
	for _, name := range path {
		field, ok := typ.FieldByName(name)
		if !ok {
			panic("jit: " + typ.String() + " has no " + name + " field")
		}
		typ, at = field.Type, at+field.Offset
	}
	return at
}
