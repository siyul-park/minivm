package types

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

type Map struct {
	Typ     *MapType
	entries map[uint64]MapEntry
	strings map[string]MapEntry
}

type MapKey struct {
	Kind Kind
	Bits uint64
	Text string
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
	StringKeys  bool
}

var _ Traceable = (*Map)(nil)
var _ Type = (*MapType)(nil)

func NewMap(typ *MapType) *Map {
	return NewMapWithCapacity(typ, 0)
}

func NewMapWithCapacity(typ *MapType, capacity int) *Map {
	m := &Map{Typ: typ}
	if typ.StringKeys {
		m.strings = make(map[string]MapEntry, capacity)
	} else {
		m.entries = make(map[uint64]MapEntry, capacity)
	}
	return m
}

func (m *Map) Kind() Kind { return KindRef }

func (m *Map) Type() Type { return m.Typ }

func (m *Map) Len() int {
	if m.Typ.StringKeys {
		return len(m.strings)
	}
	return len(m.entries)
}

func (m *Map) Get(key MapKey) (MapEntry, bool) {
	if m.Typ.StringKeys {
		entry, ok := m.strings[key.Text]
		return entry, ok
	}
	entry, ok := m.entries[key.Bits]
	return entry, ok
}

func (m *Map) Set(key MapKey, entry MapEntry) (MapEntry, bool) {
	if m.Typ.StringKeys {
		old, ok := m.strings[key.Text]
		m.strings[key.Text] = entry
		return old, ok
	}
	old, ok := m.entries[key.Bits]
	m.entries[key.Bits] = entry
	return old, ok
}

func (m *Map) Delete(key MapKey) (MapEntry, bool) {
	if m.Typ.StringKeys {
		old, ok := m.strings[key.Text]
		if ok {
			delete(m.strings, key.Text)
		}
		return old, ok
	}
	old, ok := m.entries[key.Bits]
	if ok {
		delete(m.entries, key.Bits)
	}
	return old, ok
}

func (m *Map) Range(fn func(MapKey, MapEntry)) {
	if m.Typ.StringKeys {
		for key, entry := range m.strings {
			fn(MapKey{Kind: KindRef, Text: key}, entry)
		}
		return
	}
	for bits, entry := range m.entries {
		fn(MapKey{Kind: m.Typ.KeyKind, Bits: bits}, entry)
	}
}

func (m *Map) Clear(fn func(MapEntry)) {
	if m.Typ.StringKeys {
		for key, entry := range m.strings {
			fn(entry)
			delete(m.strings, key)
		}
		return
	}
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
	refs := make([]Ref, 0, m.Len()*2)
	m.Range(func(_ MapKey, entry MapEntry) {
		if traceKeys && entry.Key.Kind() == KindRef {
			refs = append(refs, Ref(entry.Key.Ref()))
		}
		if traceValues && entry.Value.Kind() == KindRef {
			refs = append(refs, Ref(entry.Value.Ref()))
		}
	})
	return refs
}

func (k MapKey) String(typ Type) string {
	if typ.Equals(TypeString) {
		return fmt.Sprintf("%q", k.Text)
	}
	switch k.Kind {
	case KindI32:
		return BoxI32(int32(k.Bits)).String()
	case KindI64:
		return I64(int64(k.Bits)).String()
	case KindF32:
		return F32(math.Float32frombits(uint32(k.Bits))).String()
	case KindF64:
		return Boxed(k.Bits).String()
	case KindRef:
		return BoxRef(int(k.Bits)).String()
	default:
		return "<invalid>"
	}
}

func NewMapType(key Type, elem Type) *MapType {
	return &MapType{
		Key:         key,
		Elem:        elem,
		KeyKind:     key.Kind(),
		ElemKind:    elem.Kind(),
		TraceKeys:   key.Equals(TypeRef),
		TraceValues: elem.Kind() == KindRef || elem.Kind() == KindI64,
		StringKeys:  key.Equals(TypeString),
	}
}

func (t *MapType) Kind() Kind { return KindRef }

func (t *MapType) HasRefs() bool { return t.TraceKeys || t.TraceValues }

func (t *MapType) KeyRef(val Boxed) (int, bool) {
	if val.Kind() != KindRef || (!t.StringKeys && !t.TraceKeys) {
		return 0, false
	}
	return val.Ref(), true
}

func (t *MapType) BoxKey(val Boxed) (MapKey, Boxed, bool) {
	switch t.KeyKind {
	case KindI32:
		bits := uint64(uint32(val.I32()))
		return MapKey{Kind: KindI32, Bits: bits}, BoxI32(int32(bits)), true
	case KindF32:
		bits := math.Float32bits(val.F32())
		if bits == 1<<31 {
			bits = 0
		}
		return MapKey{Kind: KindF32, Bits: uint64(bits)}, BoxF32(math.Float32frombits(bits)), true
	case KindF64:
		bits := math.Float64bits(val.F64())
		if bits == 1<<63 {
			bits = 0
		}
		return MapKey{Kind: KindF64, Bits: bits}, BoxF64(math.Float64frombits(bits)), true
	case KindRef:
		if val.Kind() != KindRef {
			return MapKey{}, 0, false
		}
		return MapKey{Kind: KindRef, Bits: uint64(val.Ref())}, val, true
	default:
		return MapKey{}, 0, false
	}
}

func (t *MapType) I64Key(bits int64) (MapKey, Boxed) {
	return MapKey{Kind: KindI64, Bits: uint64(bits)}, 0
}

func (t *MapType) StringKey(text string) (MapKey, Boxed) {
	return MapKey{Kind: KindRef, Text: text}, BoxedNull
}

func (t *MapType) Zero() Boxed {
	switch t.ElemKind {
	case KindI32:
		return BoxI32(0)
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

func IsComparableMapKeyType(typ Type) bool {
	return typ.Equals(TypeI32) ||
		typ.Equals(TypeI64) ||
		typ.Equals(TypeF32) ||
		typ.Equals(TypeF64) ||
		typ.Equals(TypeString) ||
		typ.Equals(TypeRef)
}
