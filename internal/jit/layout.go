package jit

import (
	"reflect"
	"unsafe"

	"github.com/siyul-park/minivm/types"
)

// Backing identifies where a ref value derives its reference count.
type Backing uint8

// Layout is the set of runtime struct offsets and heap type identities a
// backend needs to reach into interp's private types (HostStruct, field,
// conversion, coroutine) without naming them. Input computes it once, in
// architecture-neutral code where those types are visible, and a backend
// carries the copy from Input to every lowering site — the same offsets a
// future package boundary would hand across unchanged once the backend that
// consumes them moves out of interp.
type Layout struct {
	HostFields      int
	HostPtr         int
	HostFieldOffset int
	HostFieldConv   int
	HostFieldSize   int
	HostConvKind    int
	CoroutineValue  int
	CoroutineDone   int
	// HostStructItab and CoroutineItab are the concrete heap itabs of
	// interp's private *HostStruct and *coroutine types. Neither type is
	// visible from this package, so interp computes their itabs with Itab
	// and hands them across here instead — the same treatment Layout already
	// gives struct field offsets.
	HostStructItab uintptr
	CoroutineItab  uintptr
}

const (
	BackingStack  Backing = iota // retain lives on the operand stack copy
	BackingConst                 // compile-time constant, never retained
	BackingLocal                 // deferred to a VM stack local slot
	BackingGlobal                // deferred to a global slot
	BackingUpval                 // deferred to a closure upval slot
)

// ElemShape is how one array element kind is stored: the concrete container
// itab, the byte offset its data begins at, the shift from index to byte, and
// whether the element is raw rather than boxed.
type ElemShape struct {
	Itab  uintptr
	Base  int16
	Scale uint8
	Raw   bool
}

// arrayElems is where a ref array's elements begin. It sits here with the
// shape table rather than with the lowering offsets because the portable
// planner reads it through elemShapes.
const arrayElems = int(unsafe.Offsetof(types.Array{}.Elems))

// The heap itabs the backends and the planner compare against. They are
// runtime type identities rather than lowering detail, so they belong with
// the portable JIT core: this file resolves element layout on every
// architecture, including ones with no native backend at all. heapArrayRef
// has no reader outside elemShapes: every array-kind comparison a backend
// makes goes through ElemShapeByKind or ElemShapeByItab instead.
var (
	HeapI32      = Itab(types.I32(0))
	HeapF32      = Itab(types.F32(0))
	HeapF64      = Itab(types.F64(0))
	HeapArrayI1  = Itab(types.TypedArray[bool](nil))
	HeapArrayI8  = Itab(types.TypedArray[int8](nil))
	HeapArrayI32 = Itab(types.TypedArray[int32](nil))
	HeapArrayI64 = Itab(types.TypedArray[int64](nil))
	HeapArrayF32 = Itab(types.TypedArray[float32](nil))
	HeapArrayF64 = Itab(types.TypedArray[float64](nil))
	heapArrayRef = Itab((*types.Array)(nil))
	HeapString   = Itab(types.String(""))
	HeapStruct   = Itab((*types.Struct)(nil))
	HeapError    = Itab((*types.Error)(nil))
)

// elemShapes is the one place the element storage layout is written down.
// arrayGet, arraySet, arrayLen, and the planner's hoist eligibility all
// resolve through it, so a new element kind is one row rather than an edit to
// each.
var elemShapes = []struct {
	kind  types.Kind
	shape ElemShape
}{
	{types.KindI1, ElemShape{Itab: HeapArrayI1, Raw: true}},
	{types.KindI8, ElemShape{Itab: HeapArrayI8, Raw: true}},
	{types.KindI32, ElemShape{Itab: HeapArrayI32, Scale: 2, Raw: true}},
	{types.KindI64, ElemShape{Itab: HeapArrayI64, Scale: 3, Raw: true}},
	{types.KindF32, ElemShape{Itab: HeapArrayF32, Scale: 2, Raw: true}},
	{types.KindF64, ElemShape{Itab: HeapArrayF64, Scale: 3, Raw: true}},
	{types.KindRef, ElemShape{Itab: heapArrayRef, Base: int16(arrayElems)}},
}

// ElemShapeByKind resolves the storage shape of an element kind.
func ElemShapeByKind(kind types.Kind) (ElemShape, bool) {
	for _, row := range elemShapes {
		if row.kind == kind {
			return row.shape, true
		}
	}
	return ElemShape{}, false
}

// ElemShapeByItab resolves the storage shape of a container's concrete itab.
func ElemShapeByItab(want uintptr) (ElemShape, bool) {
	for _, row := range elemShapes {
		if row.shape.Itab == want {
			return row.shape, true
		}
	}
	return ElemShape{}, false
}

// HostShape is how one Go field kind sits in memory. Kind is the VM kind its
// conversion produces, Size is the width of the Go field, and signed is the
// extension a field narrower than its slot widens with; a float row is signed
// because the VM holds a float's bit pattern sign-extended, as it holds an
// i32.
type HostShape struct {
	Kind   types.Kind
	Size   uintptr
	signed bool
}

// hostShapes is the one place the memory layout of a hosted Go field is
// written down, indexed by the reflect.Kind the codec compiled the field
// through. It mirrors the leaves table the codec picks a conversion from, and
// holds a row exactly where that conversion is a plain load or store: a
// string, pointer, or container field publishes a heap reference instead, so
// it has no row and its access stays with the interpreter.
var hostShapes = [...]HostShape{
	reflect.Bool:    {Kind: types.KindI1, Size: unsafe.Sizeof(false)},
	reflect.Int8:    {Kind: types.KindI8, Size: unsafe.Sizeof(int8(0)), signed: true},
	reflect.Int16:   {Kind: types.KindI32, Size: unsafe.Sizeof(int16(0)), signed: true},
	reflect.Int32:   {Kind: types.KindI32, Size: unsafe.Sizeof(int32(0)), signed: true},
	reflect.Int:     {Kind: types.KindI64, Size: unsafe.Sizeof(int(0)), signed: true},
	reflect.Int64:   {Kind: types.KindI64, Size: unsafe.Sizeof(int64(0)), signed: true},
	reflect.Uint8:   {Kind: types.KindI32, Size: unsafe.Sizeof(uint8(0))},
	reflect.Uint16:  {Kind: types.KindI32, Size: unsafe.Sizeof(uint16(0))},
	reflect.Uint32:  {Kind: types.KindI32, Size: unsafe.Sizeof(uint32(0))},
	reflect.Uint:    {Kind: types.KindI64, Size: unsafe.Sizeof(uint(0))},
	reflect.Uint64:  {Kind: types.KindI64, Size: unsafe.Sizeof(uint64(0))},
	reflect.Uintptr: {Kind: types.KindI64, Size: unsafe.Sizeof(uintptr(0))},
	reflect.Float32: {Kind: types.KindF32, Size: unsafe.Sizeof(float32(0)), signed: true},
	reflect.Float64: {Kind: types.KindF64, Size: unsafe.Sizeof(float64(0)), signed: true},
}

// slotShapes is the width a VM slot holds a raw scalar in, and whether it
// holds it sign-extended. A host field as wide as its slot is the slot's
// exact image, so a read reinterprets it and a write stores it whole.
var slotShapes = [...]struct {
	size   uintptr
	signed bool
}{
	types.KindI1:  {size: 1},
	types.KindI8:  {size: 1, signed: true},
	types.KindI32: {size: 4, signed: true},
	types.KindI64: {size: 8, signed: true},
	types.KindF32: {size: 4, signed: true},
	types.KindF64: {size: 8, signed: true},
}

// HostShapeByKind resolves the layout of a Go field kind, and reports false where
// the kind has no row.
func HostShapeByKind(kind reflect.Kind) (HostShape, bool) {
	if int(kind) >= len(hostShapes) {
		return HostShape{}, false
	}
	shape := hostShapes[kind]
	return shape, shape.Size != 0
}

// Exact reports whether the Go field of shape s is as wide as a VM slot of
// s.Kind, which makes the two the same bytes in either direction. A narrower
// field is not: it decodes through the range check setSigned and setUnsigned
// perform, and a check that can fail belongs with the interpreter that
// reports it, so only an exact field lowers a write. Signedness does not
// enter, because at equal width a conversion only reinterprets the bytes a
// store already writes.
func (s HostShape) Exact() bool {
	return s.Size == slotShapes[s.Kind].size
}

// Read is the width and extension a read of the field loads with. An exact
// field is reinterpreted with its slot's own extension, which is how an
// unsigned Go field reaches the guest as the signed VM value its conversion
// casts to; a narrower one widens with its own.
func (s HostShape) Read() (uintptr, bool) {
	if s.Exact() {
		return s.Size, slotShapes[s.Kind].signed
	}
	return s.Size, s.signed
}

// iface mirrors the two-word shape of a non-empty Go interface value: the
// itab pointer identifying its concrete type, and the data pointer Itab does
// not need.
type iface struct {
	itab uintptr
	_    uintptr
}

// Itab returns the itab pointer of v's concrete type: the runtime type
// identity a guarded access compares a heap value's shape against.
func Itab(v types.Value) uintptr {
	return (*iface)(unsafe.Pointer(&v)).itab
}
