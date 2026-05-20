package types

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

type Map struct {
	Typ     *MapType
	Entries map[MapKey]MapEntry
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
	Key  Type
	Elem Type
}

var _ Traceable = (*Map)(nil)
var _ Type = (*MapType)(nil)

func NewMap(typ *MapType) *Map {
	return NewMapWithCapacity(typ, 0)
}

func NewMapWithCapacity(typ *MapType, capacity int) *Map {
	return &Map{
		Typ:     typ,
		Entries: make(map[MapKey]MapEntry, capacity),
	}
}

func (m *Map) Kind() Kind { return KindRef }

func (m *Map) Type() Type { return m.Typ }

func (m *Map) String() string {
	parts := make([]string, 0, len(m.Entries))
	for key, entry := range m.Entries {
		parts = append(parts, fmt.Sprintf("%s: %s", key.String(m.Typ.Key), entry.Value.String()))
	}
	sort.Strings(parts)
	return fmt.Sprintf("%s{%s}", m.Typ, strings.Join(parts, ", "))
}

func (m *Map) Refs() []Ref {
	traceKeys := m.Typ.Key.Equals(TypeRef)
	traceValues := m.Typ.Elem.Kind() == KindRef
	if !traceKeys && !traceValues {
		return nil
	}
	refs := make([]Ref, 0, len(m.Entries)*2)
	for _, entry := range m.Entries {
		if traceKeys && entry.Key.Kind() == KindRef {
			refs = append(refs, Ref(entry.Key.Ref()))
		}
		if traceValues && entry.Value.Kind() == KindRef {
			refs = append(refs, Ref(entry.Value.Ref()))
		}
	}
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
	return &MapType{Key: key, Elem: elem}
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

func IsComparableMapKeyType(typ Type) bool {
	return typ.Equals(TypeI32) ||
		typ.Equals(TypeI64) ||
		typ.Equals(TypeF32) ||
		typ.Equals(TypeF64) ||
		typ.Equals(TypeString) ||
		typ.Equals(TypeRef)
}
