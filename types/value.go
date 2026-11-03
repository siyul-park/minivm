package types

import (
	"reflect"
	"unsafe"

	"github.com/siyul-park/minivm/instr"
)

// Value is a runtime value with its kind and type.
type Value interface {
	Kind() Kind
	Type() Type
	String() string
}

// Type describes values: its kind, textual form, and its cast and equality relations to other types.
type Type interface {
	Kind() Kind
	String() string
	Cast(other Type) bool
	Equals(other Type) bool
}

// Traceable exposes an object's nested refs for GC mark/sweep and release
// graph walks. Refs appends each nested ref to dst and returns the extended
// slice (the same append contract as the built-in append), so callers can
// reuse one buffer across many calls instead of allocating a fresh slice per
// object.
type Traceable interface {
	Value
	Refs(dst []Ref) []Ref
}

// Iterator walks a sequence: Next advances, Current reads, and Done reports exhaustion.
type Iterator interface {
	Value
	Next() bool
	Current() Value
	Done() bool
}

// Kind is the operand value classification. It is defined in instr (the lowest
// package) and aliased here so instruction metadata and the value model share
// one type; its methods (String, IsNumeric, Size) live on instr.Kind.
type Kind = instr.Kind

// sliceIdentity names a slice-backed value by its type and backing array.
type sliceIdentity struct {
	typ reflect.Type
	ptr unsafe.Pointer
	len int
}

// cloner is implemented by the mutable value types, each returning a shallow
// copy of itself.
type cloner interface {
	cloneValue() Value
}

const (
	// KindF64 is the f64 value kind.
	KindF64 = instr.KindF64
	// KindF32 is the f32 value kind.
	KindF32 = instr.KindF32
	// KindI64 is the i64 value kind.
	KindI64 = instr.KindI64
	// KindI32 is the i32 value kind.
	KindI32 = instr.KindI32
	// KindI8 is the i8 value kind.
	KindI8 = instr.KindI8
	// KindI1 is the i1 value kind.
	KindI1 = instr.KindI1
	// KindRef is the ref value kind.
	KindRef = instr.KindRef
)

// IsNull reports whether v is the null reference.
func IsNull(v Value) bool {
	switch v := v.(type) {
	case Ref:
		return v == Null
	case Boxed:
		return v == BoxedNull
	default:
		return false
	}
}

// Clone returns a shallow copy of a mutable value, so mutating the copy leaves v
// alone; boxed elements are shared. An immutable value is returned as is.
func Clone(v Value) Value {
	if c, ok := v.(cloner); ok {
		return c.cloneValue()
	}
	return v
}

// Identity returns what makes two values the same object: the pointer for a
// pointer value, the backing array for a non-empty slice. Values with equal
// identities share their mutable state. ok is false for any other value.
func Identity(v Value) (id any, ok bool) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer:
		return v, true
	case reflect.Slice:
		if rv.Len() > 0 {
			return sliceIdentity{typ: rv.Type(), ptr: rv.UnsafePointer(), len: rv.Len()}, true
		}
	}
	return nil, false
}

// Kinds projects each Type in ts to its Kind. Returns nil for an empty
// input so callers can use the result as an optional table.
func Kinds(ts []Type) []Kind {
	if len(ts) == 0 {
		return nil
	}
	out := make([]Kind, len(ts))
	for i, t := range ts {
		out[i] = t.Kind()
	}
	return out
}

// Zeros returns the Zero of each declared type's kind, the word a slot of
// that type starts as. Returns nil for an empty input.
func Zeros(ts []Type) []Boxed {
	if len(ts) == 0 {
		return nil
	}
	out := make([]Boxed, len(ts))
	for i, t := range ts {
		out[i] = Zero(t.Kind())
	}
	return out
}

// Zero returns the zero value.
func Zero(kind Kind) Boxed {
	switch kind {
	case KindI32:
		return BoxI32(0)
	case KindI8:
		return BoxI8(0)
	case KindI1:
		return BoxI1(false)
	case KindI64:
		return BoxI64(0)
	case KindF32:
		return BoxF32(0)
	case KindF64:
		return BoxF64(0)
	case KindRef:
		return BoxedNull
	default:
		return 0
	}
}
