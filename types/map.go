package types

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
)

type TypedMap[K comparable] struct {
	Typ     *MapType
	Zero    Boxed
	entries map[K]Boxed
}

type Map struct {
	Typ     *MapType
	Zero    Boxed
	entries map[MapKey]MapEntry
}

type MapKey struct {
	Kind Kind
	Bits uint64
}

type MapEntry struct {
	Key   Boxed
	Value Boxed
}

type MapType struct {
	Key         Type
	Elem        Type
	KeyKind     Kind
	ElemKind    Kind
	TraceKeys   bool
	TraceValues bool
}

// MapIterator walks the live map via reflect.MapIter rather than a
// construction-time snapshot: a snapshot of ref-typed keys/values would not be
// refcount-protected, so a MAP_DELETE on an entry still queued in the snapshot
// could free it before the iterator reached it. Ranging over the live map has
// no such hazard - Go guarantees a key deleted mid-range is simply never
// produced.
type MapIterator struct {
	iter    *reflect.MapIter
	current Value
	typ     Type
	ref     Ref
	kind    mapIteratorKind
	done    bool
}

type mapIteratorKind byte

const (
	mapIteratorInvalid mapIteratorKind = iota
	mapIteratorI8
	mapIteratorI1
	mapIteratorI32
	mapIteratorI64
	mapIteratorF32
	mapIteratorF64
	mapIteratorGeneric
)

var (
	_ Traceable = (*Map)(nil)
	_ Traceable = (*TypedMap[int8])(nil)
	_ Traceable = (*TypedMap[bool])(nil)
	_ Traceable = (*TypedMap[int32])(nil)
	_ Traceable = (*TypedMap[int64])(nil)
	_ Traceable = (*TypedMap[float32])(nil)
	_ Traceable = (*TypedMap[float64])(nil)
	_ Traceable = (*MapIterator)(nil)
	_ Iterator  = (*MapIterator)(nil)
	_ Type      = (*MapType)(nil)
)

func NewTypedMap[K comparable](typ *MapType, capacity int) *TypedMap[K] {
	return &TypedMap[K]{Typ: typ, Zero: Zero(typ.ElemKind), entries: make(map[K]Boxed, capacity)}
}

func NewMap(typ *MapType) *Map {
	return NewMapWithCapacity(typ, 0)
}

func NewMapForType(typ *MapType, capacity int) Value {
	switch typ.KeyKind {
	case KindI8:
		return NewTypedMap[int8](typ, capacity)
	case KindI1:
		return NewTypedMap[bool](typ, capacity)
	case KindI32:
		return NewTypedMap[int32](typ, capacity)
	case KindI64:
		return NewTypedMap[int64](typ, capacity)
	case KindF32:
		return NewTypedMap[float32](typ, capacity)
	case KindF64:
		return NewTypedMap[float64](typ, capacity)
	default:
		return NewMapWithCapacity(typ, capacity)
	}
}

func NewMapWithCapacity(typ *MapType, capacity int) *Map {
	return &Map{
		Typ:     typ,
		Zero:    Zero(typ.ElemKind),
		entries: make(map[MapKey]MapEntry, capacity),
	}
}

func NewMapIterator(ref Ref, val Value) *MapIterator {
	it := &MapIterator{typ: NewIteratorType(TypeRef), ref: ref, done: true, current: BoxedNull}
	switch m := val.(type) {
	case *TypedMap[int8]:
		it.typ = NewIteratorType(m.Typ.Key)
		it.kind = mapIteratorI8
		it.iter = reflect.ValueOf(m.entries).MapRange()
	case *TypedMap[bool]:
		it.typ = NewIteratorType(m.Typ.Key)
		it.kind = mapIteratorI1
		it.iter = reflect.ValueOf(m.entries).MapRange()
	case *TypedMap[int32]:
		it.typ = NewIteratorType(m.Typ.Key)
		it.kind = mapIteratorI32
		it.iter = reflect.ValueOf(m.entries).MapRange()
	case *TypedMap[int64]:
		it.typ = NewIteratorType(m.Typ.Key)
		it.kind = mapIteratorI64
		it.iter = reflect.ValueOf(m.entries).MapRange()
	case *TypedMap[float32]:
		it.typ = NewIteratorType(m.Typ.Key)
		it.kind = mapIteratorF32
		it.iter = reflect.ValueOf(m.entries).MapRange()
	case *TypedMap[float64]:
		it.typ = NewIteratorType(m.Typ.Key)
		it.kind = mapIteratorF64
		it.iter = reflect.ValueOf(m.entries).MapRange()
	case *Map:
		it.typ = NewIteratorType(m.Typ.Key)
		it.kind = mapIteratorGeneric
		it.iter = reflect.ValueOf(m.entries).MapRange()
	}
	return it
}

func NewMapType(key Type, elem Type) *MapType {
	return &MapType{
		Key:         key,
		Elem:        elem,
		KeyKind:     key.Kind(),
		ElemKind:    elem.Kind(),
		TraceKeys:   key.Kind() == KindRef,
		TraceValues: elem.Kind() == KindRef || elem.Kind() == KindI64,
	}
}

func (m *TypedMap[K]) Kind() Kind { return KindRef }

func (m *TypedMap[K]) Type() Type { return m.Typ }

func (m *TypedMap[K]) Get(key K) (Boxed, bool) {
	value, ok := m.entries[key]
	return value, ok
}

func (m *TypedMap[K]) Set(key K, value Boxed) (Boxed, bool) {
	old, ok := m.entries[key]
	m.entries[key] = value
	return old, ok
}

func (m *TypedMap[K]) Delete(key K) (Boxed, bool) {
	old, ok := m.entries[key]
	if ok {
		delete(m.entries, key)
	}
	return old, ok
}

func (m *TypedMap[K]) Clear(fn func(Boxed)) {
	for key, value := range m.entries {
		fn(value)
		delete(m.entries, key)
	}
}

func (m *TypedMap[K]) String() string {
	parts := make([]string, 0, m.Len())
	m.Range(func(key K, value Boxed) {
		parts = append(parts, fmt.Sprintf("%s: %s", formatKey(any(key)), value.String()))
	})
	sort.Strings(parts)
	return fmt.Sprintf("%s{%s}", m.Typ, strings.Join(parts, ", "))
}

func (m *TypedMap[K]) Len() int { return len(m.entries) }

func (m *TypedMap[K]) Range(fn func(K, Boxed)) {
	for key, value := range m.entries {
		fn(key, value)
	}
}

func (m *TypedMap[K]) Refs(dst []Ref) []Ref {
	if !m.Typ.TraceValues {
		return dst
	}
	for _, value := range m.entries {
		if value.Kind() == KindRef {
			dst = append(dst, Ref(value.Ref()))
		}
	}
	return dst
}

func (m *Map) Kind() Kind { return KindRef }

func (m *Map) Type() Type { return m.Typ }

func (m *Map) Get(key MapKey) (MapEntry, bool) {
	entry, ok := m.entries[key]
	return entry, ok
}

func (m *Map) Set(key MapKey, entry MapEntry) (MapEntry, bool) {
	old, ok := m.entries[key]
	m.entries[key] = entry
	return old, ok
}

func (m *Map) Delete(key MapKey) (MapEntry, bool) {
	old, ok := m.entries[key]
	if ok {
		delete(m.entries, key)
	}
	return old, ok
}

func (m *Map) Clear(fn func(MapEntry)) {
	for key, entry := range m.entries {
		fn(entry)
		delete(m.entries, key)
	}
}

func (m *Map) String() string {
	parts := make([]string, 0, m.Len())
	m.Range(func(key MapKey, entry MapEntry) {
		parts = append(parts, fmt.Sprintf("%s: %s", key.String(), entry.Value.String()))
	})
	sort.Strings(parts)
	return fmt.Sprintf("%s{%s}", m.Typ, strings.Join(parts, ", "))
}

func (m *Map) Len() int { return len(m.entries) }

func (m *Map) Range(fn func(MapKey, MapEntry)) {
	for key, entry := range m.entries {
		fn(key, entry)
	}
}

func (m *Map) Refs(dst []Ref) []Ref {
	traceKeys := m.Typ.TraceKeys
	traceValues := m.Typ.TraceValues
	if !traceKeys && !traceValues {
		return dst
	}
	for _, entry := range m.entries {
		if traceKeys && entry.Key.Kind() == KindRef {
			dst = append(dst, Ref(entry.Key.Ref()))
		}
		if traceValues && entry.Value.Kind() == KindRef {
			dst = append(dst, Ref(entry.Value.Ref()))
		}
	}
	return dst
}

func (it *MapIterator) Kind() Kind { return KindRef }

func (it *MapIterator) Type() Type { return it.typ }

func (it *MapIterator) String() string { return "map.iterator" }

func (it *MapIterator) Next() bool {
	if it.iter == nil || !it.iter.Next() {
		it.current = BoxedNull
		it.done = true
		return false
	}
	it.done = false
	switch it.kind {
	case mapIteratorI8:
		it.current = I8(int8(it.iter.Key().Int()))
	case mapIteratorI1:
		it.current = I1(it.iter.Key().Bool())
	case mapIteratorI32:
		it.current = I32(int32(it.iter.Key().Int()))
	case mapIteratorI64:
		it.current = I64(it.iter.Key().Int())
	case mapIteratorF32:
		it.current = F32(float32(it.iter.Key().Float()))
	case mapIteratorF64:
		it.current = F64(it.iter.Key().Float())
	case mapIteratorGeneric:
		key := it.iter.Key().Interface().(MapKey)
		entry := it.iter.Value().Interface().(MapEntry)
		it.current = key.value(entry)
	default:
		it.current = BoxedNull
		it.done = true
		return false
	}
	return true
}

func (it *MapIterator) Current() Value { return it.current }

func (it *MapIterator) Done() bool { return it.done }

func (it *MapIterator) Refs(dst []Ref) []Ref {
	dst = append(dst, it.ref)
	if !it.done {
		switch current := it.current.(type) {
		case Boxed:
			if current.Kind() == KindRef {
				dst = append(dst, Ref(current.Ref()))
			}
		case Ref:
			dst = append(dst, current)
		}
	}
	return dst
}

func (k MapKey) String() string {
	switch k.Kind {
	case KindI32:
		return BoxI32(int32(k.Bits)).String()
	case KindI64:
		return I64(int64(k.Bits)).String()
	case KindF32:
		return F32(math.Float32frombits(uint32(k.Bits))).String()
	case KindF64:
		return F64(math.Float64frombits(k.Bits)).String()
	case KindRef:
		return BoxRef(int(k.Bits)).String()
	default:
		return "<invalid>"
	}
}

func (t *MapType) Kind() Kind { return KindRef }

func (t *MapType) String() string {
	return "map[" + t.Key.String() + "]" + t.Elem.String()
}

func (t *MapType) Cast(other Type) bool {
	return t.Equals(other)
}

func (t *MapType) Equals(other Type) bool {
	if t == other {
		return true
	}
	o, ok := other.(*MapType)
	if !ok {
		return false
	}
	return t.Key.Equals(o.Key) && t.Elem.Equals(o.Elem)
}

func (k MapKey) value(entry MapEntry) Value {
	if entry.Key != 0 {
		return entry.Key
	}
	switch k.Kind {
	case KindI32:
		return I32(int32(k.Bits))
	case KindI64:
		return I64(int64(k.Bits))
	case KindF32:
		return F32(math.Float32frombits(uint32(k.Bits)))
	case KindF64:
		return F64(math.Float64frombits(k.Bits))
	case KindRef:
		return Ref(int32(k.Bits))
	default:
		return BoxedNull
	}
}

// formatKey renders a native map key through its boxed value's String form.
func formatKey(k any) string {
	switch v := k.(type) {
	case int8:
		return I8(v).String()
	case bool:
		return I1(v).String()
	case int32:
		return I32(v).String()
	case int64:
		return I64(v).String()
	case float32:
		return F32(v).String()
	case float64:
		return F64(v).String()
	default:
		return fmt.Sprint(v)
	}
}
