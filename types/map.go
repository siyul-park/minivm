package types

import (
	"fmt"
	"math"
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

var (
	_ Traceable = (*Map)(nil)
	_ Traceable = (*TypedMap[int32])(nil)
	_ Traceable = (*TypedMap[int64])(nil)
	_ Traceable = (*TypedMap[float32])(nil)
	_ Traceable = (*TypedMap[float64])(nil)
	_ Type      = (*MapType)(nil)
)

func NewTypedMap[K comparable](typ *MapType, capacity int) *TypedMap[K] {
	return &TypedMap[K]{Typ: typ, Zero: Zero(typ.ElemKind), entries: make(map[K]Boxed, capacity)}
}

func NewMap(typ *MapType) *Map {
	return NewMapWithCapacity(typ, 0)
}

func NewMapWithCapacity(typ *MapType, capacity int) *Map {
	return &Map{
		Typ:     typ,
		Zero:    Zero(typ.ElemKind),
		entries: make(map[MapKey]MapEntry, capacity),
	}
}

func NewMapForType(typ *MapType, capacity int) Value {
	switch typ.KeyKind {
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

func (m *TypedMap[K]) Len() int { return len(m.entries) }

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

func (m *TypedMap[K]) Range(fn func(K, Boxed)) {
	for key, value := range m.entries {
		fn(key, value)
	}
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
		parts = append(parts, fmt.Sprintf("%s: %s", boxKey(any(key)), value.String()))
	})
	sort.Strings(parts)
	return fmt.Sprintf("%s{%s}", m.Typ, strings.Join(parts, ", "))
}

func (m *TypedMap[K]) Refs() []Ref {
	if !m.Typ.TraceValues {
		return nil
	}
	var refs []Ref
	for _, value := range m.entries {
		if value.Kind() == KindRef {
			if refs == nil {
				refs = make([]Ref, 0, m.Len())
			}
			refs = append(refs, Ref(value.Ref()))
		}
	}
	return refs
}

func (m *Map) Kind() Kind { return KindRef }

func (m *Map) Type() Type { return m.Typ }

func (m *Map) Len() int { return len(m.entries) }

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

func (m *Map) Range(fn func(MapKey, MapEntry)) {
	for key, entry := range m.entries {
		fn(key, entry)
	}
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

func (m *Map) Refs() []Ref {
	traceKeys := m.Typ.TraceKeys
	traceValues := m.Typ.TraceValues
	if !traceKeys && !traceValues {
		return nil
	}
	var refs []Ref
	for _, entry := range m.entries {
		if traceKeys && entry.Key.Kind() == KindRef {
			if refs == nil {
				refs = make([]Ref, 0, m.Len()*2)
			}
			refs = append(refs, Ref(entry.Key.Ref()))
		}
		if traceValues && entry.Value.Kind() == KindRef {
			if refs == nil {
				refs = make([]Ref, 0, m.Len()*2)
			}
			refs = append(refs, Ref(entry.Value.Ref()))
		}
	}
	return refs
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

// boxKey renders a native map key through its boxed value's String form.
func boxKey(k any) string {
	switch v := k.(type) {
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
