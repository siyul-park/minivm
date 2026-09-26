package jit

import (
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

	OffsetArrayElems       = unsafe.Offsetof(types.Array{}.Elems)
	OffsetStructTyp        = unsafe.Offsetof(types.Struct{}.Typ)
	OffsetStructData       = unsafe.Offsetof(types.Struct{}.Data)
	OffsetStructTypeFields = unsafe.Offsetof(types.StructType{}.Fields)
	SizeofStructField      = unsafe.Sizeof(types.StructField{})
	OffsetStructFieldKind  = unsafe.Offsetof(types.StructField{}.Kind)
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
