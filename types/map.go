package types

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

type Map struct {
	Typ     *MapType
	Zero    Boxed
	entries map[MapKey]MapEntry
}

type MapI32 struct {
	Typ     *MapType
	Zero    Boxed
	entries map[int32]Boxed
}

type MapI64 struct {
	Typ     *MapType
	Zero    Boxed
	entries map[int64]Boxed
}

type MapF32 struct {
	Typ     *MapType
	Zero    Boxed
	entries map[uint32]Boxed
}

type MapF64 struct {
	Typ     *MapType
	Zero    Boxed
	entries map[uint64]Boxed
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
	_ Traceable = (*MapI32)(nil)
	_ Traceable = (*MapI64)(nil)
	_ Traceable = (*MapF32)(nil)
	_ Traceable = (*MapF64)(nil)
	_ Type      = (*MapType)(nil)
)

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
		return NewMapI32(typ, capacity)
	case KindI64:
		return NewMapI64(typ, capacity)
	case KindF32:
		return NewMapF32(typ, capacity)
	case KindF64:
		return NewMapF64(typ, capacity)
	default:
		return NewMapWithCapacity(typ, capacity)
	}
}

func NewMapI32(typ *MapType, capacity int) *MapI32 {
	return &MapI32{Typ: typ, Zero: Zero(typ.ElemKind), entries: make(map[int32]Boxed, capacity)}
}

func NewMapI64(typ *MapType, capacity int) *MapI64 {
	return &MapI64{Typ: typ, Zero: Zero(typ.ElemKind), entries: make(map[int64]Boxed, capacity)}
}

func NewMapF32(typ *MapType, capacity int) *MapF32 {
	return &MapF32{Typ: typ, Zero: Zero(typ.ElemKind), entries: make(map[uint32]Boxed, capacity)}
}

func NewMapF64(typ *MapType, capacity int) *MapF64 {
	return &MapF64{Typ: typ, Zero: Zero(typ.ElemKind), entries: make(map[uint64]Boxed, capacity)}
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
		parts = append(parts, fmt.Sprintf("%s: %s", key.String(m.Typ.Key), entry.Value.String()))
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

func (m *MapI32) Kind() Kind { return KindRef }

func (m *MapI32) Type() Type { return m.Typ }

func (m *MapI32) Len() int { return len(m.entries) }

func (m *MapI32) Get(key int32) (Boxed, bool) {
	value, ok := m.entries[key]
	return value, ok
}

func (m *MapI32) Set(key int32, value Boxed) (Boxed, bool) {
	old, ok := m.entries[key]
	m.entries[key] = value
	return old, ok
}

func (m *MapI32) Delete(key int32) (Boxed, bool) {
	old, ok := m.entries[key]
	if ok {
		delete(m.entries, key)
	}
	return old, ok
}

func (m *MapI32) Range(fn func(int32, Boxed)) {
	for key, value := range m.entries {
		fn(key, value)
	}
}

func (m *MapI32) Clear(fn func(Boxed)) {
	for key, value := range m.entries {
		fn(value)
		delete(m.entries, key)
	}
}

func (m *MapI32) String() string {
	parts := make([]string, 0, m.Len())
	m.Range(func(key int32, value Boxed) {
		parts = append(parts, fmt.Sprintf("%s: %s", I32(key), value.String()))
	})
	sort.Strings(parts)
	return fmt.Sprintf("%s{%s}", m.Typ, strings.Join(parts, ", "))
}

func (m *MapI32) Refs() []Ref {
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

func (m *MapI64) Kind() Kind { return KindRef }

func (m *MapI64) Type() Type { return m.Typ }

func (m *MapI64) Len() int { return len(m.entries) }

func (m *MapI64) Get(key int64) (Boxed, bool) {
	value, ok := m.entries[key]
	return value, ok
}

func (m *MapI64) Set(key int64, value Boxed) (Boxed, bool) {
	old, ok := m.entries[key]
	m.entries[key] = value
	return old, ok
}

func (m *MapI64) Delete(key int64) (Boxed, bool) {
	old, ok := m.entries[key]
	if ok {
		delete(m.entries, key)
	}
	return old, ok
}

func (m *MapI64) Range(fn func(int64, Boxed)) {
	for key, value := range m.entries {
		fn(key, value)
	}
}

func (m *MapI64) Clear(fn func(Boxed)) {
	for key, value := range m.entries {
		fn(value)
		delete(m.entries, key)
	}
}

func (m *MapI64) String() string {
	parts := make([]string, 0, m.Len())
	m.Range(func(key int64, value Boxed) {
		parts = append(parts, fmt.Sprintf("%s: %s", I64(key), value.String()))
	})
	sort.Strings(parts)
	return fmt.Sprintf("%s{%s}", m.Typ, strings.Join(parts, ", "))
}

func (m *MapI64) Refs() []Ref {
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

func (m *MapF32) Kind() Kind { return KindRef }

func (m *MapF32) Type() Type { return m.Typ }

func (m *MapF32) Len() int { return len(m.entries) }

func (m *MapF32) Get(key float32) (Boxed, bool) {
	bits := math.Float32bits(key)
	if bits == 1<<31 {
		bits = 0
	}
	value, ok := m.entries[bits]
	return value, ok
}

func (m *MapF32) Set(key float32, value Boxed) (Boxed, bool) {
	bits := math.Float32bits(key)
	if bits == 1<<31 {
		bits = 0
	}
	old, ok := m.entries[bits]
	m.entries[bits] = value
	return old, ok
}

func (m *MapF32) Delete(key float32) (Boxed, bool) {
	bits := math.Float32bits(key)
	if bits == 1<<31 {
		bits = 0
	}
	old, ok := m.entries[bits]
	if ok {
		delete(m.entries, bits)
	}
	return old, ok
}

func (m *MapF32) Range(fn func(float32, Boxed)) {
	for key, value := range m.entries {
		fn(math.Float32frombits(key), value)
	}
}

func (m *MapF32) Clear(fn func(Boxed)) {
	for key, value := range m.entries {
		fn(value)
		delete(m.entries, key)
	}
}

func (m *MapF32) String() string {
	parts := make([]string, 0, m.Len())
	m.Range(func(key float32, value Boxed) {
		parts = append(parts, fmt.Sprintf("%s: %s", F32(key), value.String()))
	})
	sort.Strings(parts)
	return fmt.Sprintf("%s{%s}", m.Typ, strings.Join(parts, ", "))
}

func (m *MapF32) Refs() []Ref {
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

func (m *MapF64) Kind() Kind { return KindRef }

func (m *MapF64) Type() Type { return m.Typ }

func (m *MapF64) Len() int { return len(m.entries) }

func (m *MapF64) Get(key float64) (Boxed, bool) {
	bits := math.Float64bits(key)
	if bits == 1<<63 {
		bits = 0
	}
	value, ok := m.entries[bits]
	return value, ok
}

func (m *MapF64) Set(key float64, value Boxed) (Boxed, bool) {
	bits := math.Float64bits(key)
	if bits == 1<<63 {
		bits = 0
	}
	old, ok := m.entries[bits]
	m.entries[bits] = value
	return old, ok
}

func (m *MapF64) Delete(key float64) (Boxed, bool) {
	bits := math.Float64bits(key)
	if bits == 1<<63 {
		bits = 0
	}
	old, ok := m.entries[bits]
	if ok {
		delete(m.entries, bits)
	}
	return old, ok
}

func (m *MapF64) Range(fn func(float64, Boxed)) {
	for key, value := range m.entries {
		fn(math.Float64frombits(key), value)
	}
}

func (m *MapF64) Clear(fn func(Boxed)) {
	for key, value := range m.entries {
		fn(value)
		delete(m.entries, key)
	}
}

func (m *MapF64) String() string {
	parts := make([]string, 0, m.Len())
	m.Range(func(key float64, value Boxed) {
		parts = append(parts, fmt.Sprintf("%s: %s", F64(key), value.String()))
	})
	sort.Strings(parts)
	return fmt.Sprintf("%s{%s}", m.Typ, strings.Join(parts, ", "))
}

func (m *MapF64) Refs() []Ref {
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

func (k MapKey) String(typ Type) string {
	if typ.Equals(TypeString) && k.Kind == KindRef {
		return BoxRef(int(k.Bits)).String()
	}
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
