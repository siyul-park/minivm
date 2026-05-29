package types

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

type Map[K comparable] struct {
	Typ     *MapType
	Zero    Boxed
	entries map[K]Boxed
}

type BoxedMap struct {
	Typ     *MapType
	Zero    Boxed
	entries map[BoxedMapKey]BoxedMapEntry
}

type BoxedMapKey struct {
	Kind Kind
	Bits uint64
}

type BoxedMapEntry struct {
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
	_ Traceable = (*BoxedMap)(nil)
	_ Traceable = (*Map[int32])(nil)
	_ Traceable = (*Map[int64])(nil)
	_ Traceable = (*Map[float32])(nil)
	_ Traceable = (*Map[float64])(nil)
	_ Type      = (*MapType)(nil)
)

func NewMap[K comparable](typ *MapType, capacity int) *Map[K] {
	return &Map[K]{Typ: typ, Zero: Zero(typ.ElemKind), entries: make(map[K]Boxed, capacity)}
}

func NewBoxedMap(typ *MapType) *BoxedMap {
	return NewBoxedMapWithCapacity(typ, 0)
}

func NewBoxedMapWithCapacity(typ *MapType, capacity int) *BoxedMap {
	return &BoxedMap{
		Typ:     typ,
		Zero:    Zero(typ.ElemKind),
		entries: make(map[BoxedMapKey]BoxedMapEntry, capacity),
	}
}

func NewMapForType(typ *MapType, capacity int) Value {
	switch typ.KeyKind {
	case KindI32:
		return NewMap[int32](typ, capacity)
	case KindI64:
		return NewMap[int64](typ, capacity)
	case KindF32:
		return NewMap[float32](typ, capacity)
	case KindF64:
		return NewMap[float64](typ, capacity)
	default:
		return NewBoxedMapWithCapacity(typ, capacity)
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

func (m *Map[K]) Kind() Kind { return KindRef }

func (m *Map[K]) Type() Type { return m.Typ }

func (m *Map[K]) Len() int { return len(m.entries) }

func (m *Map[K]) Get(key K) (Boxed, bool) {
	value, ok := m.entries[key]
	return value, ok
}

func (m *Map[K]) Set(key K, value Boxed) (Boxed, bool) {
	old, ok := m.entries[key]
	m.entries[key] = value
	return old, ok
}

func (m *Map[K]) Delete(key K) (Boxed, bool) {
	old, ok := m.entries[key]
	if ok {
		delete(m.entries, key)
	}
	return old, ok
}

func (m *Map[K]) Range(fn func(K, Boxed)) {
	for key, value := range m.entries {
		fn(key, value)
	}
}

func (m *Map[K]) Clear(fn func(Boxed)) {
	for key, value := range m.entries {
		fn(value)
		delete(m.entries, key)
	}
}

func (m *Map[K]) String() string {
	parts := make([]string, 0, m.Len())
	m.Range(func(key K, value Boxed) {
		parts = append(parts, fmt.Sprintf("%s: %s", boxKey(any(key)), value.String()))
	})
	sort.Strings(parts)
	return fmt.Sprintf("%s{%s}", m.Typ, strings.Join(parts, ", "))
}

func (m *Map[K]) Refs() []Ref {
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

func (m *BoxedMap) Kind() Kind { return KindRef }

func (m *BoxedMap) Type() Type { return m.Typ }

func (m *BoxedMap) Len() int { return len(m.entries) }

func (m *BoxedMap) Get(key BoxedMapKey) (BoxedMapEntry, bool) {
	entry, ok := m.entries[key]
	return entry, ok
}

func (m *BoxedMap) Set(key BoxedMapKey, entry BoxedMapEntry) (BoxedMapEntry, bool) {
	old, ok := m.entries[key]
	m.entries[key] = entry
	return old, ok
}

func (m *BoxedMap) Delete(key BoxedMapKey) (BoxedMapEntry, bool) {
	old, ok := m.entries[key]
	if ok {
		delete(m.entries, key)
	}
	return old, ok
}

func (m *BoxedMap) Range(fn func(BoxedMapKey, BoxedMapEntry)) {
	for key, entry := range m.entries {
		fn(key, entry)
	}
}

func (m *BoxedMap) Clear(fn func(BoxedMapEntry)) {
	for key, entry := range m.entries {
		fn(entry)
		delete(m.entries, key)
	}
}

func (m *BoxedMap) String() string {
	parts := make([]string, 0, m.Len())
	m.Range(func(key BoxedMapKey, entry BoxedMapEntry) {
		parts = append(parts, fmt.Sprintf("%s: %s", key.String(), entry.Value.String()))
	})
	sort.Strings(parts)
	return fmt.Sprintf("%s{%s}", m.Typ, strings.Join(parts, ", "))
}

func (m *BoxedMap) Refs() []Ref {
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

func (k BoxedMapKey) String() string {
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
