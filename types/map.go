package types

import (
	"fmt"
	"maps"
	"math"
	"math/bits"
	"math/rand/v2"
	"reflect"
	"slices"
	"sort"
	"strings"
	"unsafe"
)

// TypedMap is a map from a numeric key to boxed values. Its entries live in a
// pointer-free open-addressing table, so native code can read them without
// touching a Go map. A TypedMap `MUST` come from NewTypedMap; its zero value is
// not usable. Iteration follows insertion order.
type TypedMap[K Scalar] struct {
	Typ   *MapType
	Zero  Boxed
	table table
}

// StringMap is a map from a string key to boxed values.
type StringMap struct {
	Typ     *MapType
	Zero    Boxed
	entries map[string]Boxed
}

// Map is a map with keys of any kind, indexed by MapKey.
type Map struct {
	Typ     *MapType
	Zero    Boxed
	entries map[MapKey]MapEntry
}

// MapLiteral is an unmaterialized map constant.
type MapLiteral struct {
	Typ    *MapType
	Keys   []Value
	Values []Value
}

// MapKey indexes one entry of a generic Map. A scalar key is held in Bits, a
// string key in Text under KindText so equal strings index one entry however
// each was published, and any other reference key in Bits as its heap address.
type MapKey struct {
	Kind Kind
	Bits uint64
	Text string
}

// MapEntry is one key and value of a Map.
type MapEntry struct {
	Key   Boxed
	Value Boxed
}

// MapType is the type of maps from Key to Elem.
type MapType struct {
	Key         Type
	Elem        Type
	KeyKind     Kind
	ElemKind    Kind
	TraceKeys   bool
	TraceValues bool
}

// MapIterator walks the live map rather than a construction-time snapshot: a
// snapshot of ref-typed keys/values would not be refcount-protected, so a
// MAP_DELETE on an entry still queued in the snapshot could free it before the
// iterator reached it. Each entry present for the whole walk is produced once,
// and a key deleted before the iterator reaches it is never produced. A string
// or generic map relies on Go's range guarantee. A numeric table walks its
// live insertion order, so an entry inserted during the walk is produced too. A
// rehash moves the walk to the new order through the epoch it started on, so it
// never reads an entry the table gave up. Clear ends every walk.
type MapIterator struct {
	iter    *reflect.MapIter
	table   *table
	epoch   *epoch
	pos     int
	gen     uint64
	current Value
	typ     Type
	ref     Ref
	kind    mapIteratorKind
	started bool
	done    bool
}

// table is an open-addressing hash table from a numeric key word to a Boxed
// value word that remembers insertion order. Native code reaches only its four
// slices and integer words. Slot i keeps its key at slots[2i], its value at
// slots[2i+1], and its state at ctrl[i]: slotEmpty, slotDead, or slotFull with
// seven hash bits, so a probe passes most occupied slots on the control byte
// alone. order[:used] lists the slots in insertion order, and pos[i] is the
// position of slot i's latest insert: position p holds a live entry while
// slot order[p] is full and pos[order[p]] is p.
//
// The capacity is a power of two. A key's hash is its word xor seed times
// tableHash; its home slot is the hash shifted right by shift, and its tag the
// seven bits below those. seed is random per table, so keys crafted to collide
// for the bare constant spread out; it never affects order. Probing is linear.
// An insert takes the first tombstone on its probe, else the empty slot that
// ended it, and always appends a position, so used bounds the non-empty slots.
// Once order is full, a rehash moves the live entries, in order, into fresh
// arrays and, once a walk has begun, starts a new epoch. Keys compare as their
// Go values, so floats follow Go map keys, except that a zero key is stored as
// +0: -0 and +0 are one key, and a NaN never matches, so every Set of NaN adds
// an entry that only Clear removes.
type table struct {
	ctrl  []byte
	slots []uint64
	order []uint32
	pos   []uint32
	mask  uint64
	shift uint64
	seed  uint64
	count int
	used  int
	gen   uint64
	epoch *epoch
}

// epoch is one rehash generation of a table's order, after V8's obsolete
// OrderedHashTable. A rehash ends the current epoch by recording the next one
// and the positions of its order that held dead entries, so an iterator at
// position p of the old order resumes at p minus the holes before p of the
// new one, and never reads the old arrays. A table has no epoch until its first
// walk, so a map nobody iterates never pays for one.
type epoch struct {
	next  *epoch
	holes []uint32
}

type mapIteratorKind byte

// KindText marks a MapKey indexed by string content rather than by heap
// reference. It discriminates map keys only and is never an operand kind, so
// an empty string key stays distinct from a null reference key.
const KindText Kind = 0xFE

const (
	slotEmpty byte = 0
	slotDead  byte = 1
	slotFull  byte = 0x80
)

const (
	tableMinCapacity = 8
	tableHash        = 0x9E3779B97F4A7C15
)

const (
	mapIteratorInvalid mapIteratorKind = iota
	mapIteratorI8
	mapIteratorI1
	mapIteratorI32
	mapIteratorI64
	mapIteratorF32
	mapIteratorF64
	mapIteratorString
	mapIteratorGeneric
)

var (
	_ Value     = (*MapLiteral)(nil)
	_ Traceable = (*Map)(nil)
	_ Traceable = (*TypedMap[int8])(nil)
	_ Traceable = (*TypedMap[bool])(nil)
	_ Traceable = (*TypedMap[int32])(nil)
	_ Traceable = (*TypedMap[int64])(nil)
	_ Traceable = (*TypedMap[float32])(nil)
	_ Traceable = (*TypedMap[float64])(nil)
	_ Traceable = (*StringMap)(nil)
	_ Traceable = (*MapIterator)(nil)
	_ Iterator  = (*MapIterator)(nil)
	_ Type      = (*MapType)(nil)
)

// NewMapForType returns an empty map specialized to the key kind of typ.
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
	case KindRef:

		if typ.Key.Equals(TypeString()) {
			return NewStringMap(typ, capacity)
		}
	}
	return NewMapWithCapacity(typ, capacity)
}

// NewTypedMap creates a typed map sized for capacity entries.
func NewTypedMap[K Scalar](typ *MapType, capacity int) *TypedMap[K] {
	m := &TypedMap[K]{Typ: typ, Zero: Zero(typ.ElemKind)}
	m.table.init(capacity)
	return m
}

// NewStringMap creates a string-keyed map sized for capacity entries.
func NewStringMap(typ *MapType, capacity int) *StringMap {
	return &StringMap{Typ: typ, Zero: Zero(typ.ElemKind), entries: make(map[string]Boxed, capacity)}
}

// NewMap returns an empty generic map.
func NewMap(typ *MapType) *Map {
	return NewMapWithCapacity(typ, 0)
}

// NewMapWithCapacity returns an empty generic Map of type typ sized for capacity entries.
func NewMapWithCapacity(typ *MapType, capacity int) *Map {
	return &Map{
		Typ:     typ,
		Zero:    Zero(typ.ElemKind),
		entries: make(map[MapKey]MapEntry, capacity),
	}
}

// NewMapType returns the map type from key to elem.
func NewMapType(key Type, elem Type) *MapType {
	return &MapType{
		Key:         key,
		Elem:        elem,
		KeyKind:     key.Kind(),
		ElemKind:    elem.Kind(),
		TraceKeys:   key.Kind() == KindRef && !key.Equals(TypeString()),
		TraceValues: elem.Kind() == KindRef || elem.Kind() == KindI64,
	}
}

// NewMapIterator returns an iterator over the live map val, which is at heap ref.
func NewMapIterator(ref Ref, val Value) *MapIterator {
	it := &MapIterator{typ: NewIteratorType(TypeAny()), ref: ref, done: true, current: BoxedNull}
	switch m := val.(type) {
	case *TypedMap[int8]:
		it.typ = NewIteratorType(m.Typ.Key)
		it.kind = mapIteratorI8
		walk(it, m)
	case *TypedMap[bool]:
		it.typ = NewIteratorType(m.Typ.Key)
		it.kind = mapIteratorI1
		walk(it, m)
	case *TypedMap[int32]:
		it.typ = NewIteratorType(m.Typ.Key)
		it.kind = mapIteratorI32
		walk(it, m)
	case *TypedMap[int64]:
		it.typ = NewIteratorType(m.Typ.Key)
		it.kind = mapIteratorI64
		walk(it, m)
	case *TypedMap[float32]:
		it.typ = NewIteratorType(m.Typ.Key)
		it.kind = mapIteratorF32
		walk(it, m)
	case *TypedMap[float64]:
		it.typ = NewIteratorType(m.Typ.Key)
		it.kind = mapIteratorF64
		walk(it, m)
	case *StringMap:
		it.typ = NewIteratorType(m.Typ.Key)
		it.kind = mapIteratorString
		it.iter = reflect.ValueOf(m.entries).MapRange()
	case *Map:
		it.typ = NewIteratorType(m.Typ.Key)
		it.kind = mapIteratorGeneric
		it.iter = reflect.ValueOf(m.entries).MapRange()
	}
	return it
}

// Get returns the value for key and whether it exists.
func (m *TypedMap[K]) Get(key K) (Boxed, bool) {
	value, ok := get(&m.table, key)
	return Boxed(value), ok
}

// Set stores value under key and returns the replaced value and whether one existed.
func (m *TypedMap[K]) Set(key K, value Boxed) (Boxed, bool) {
	old, ok := set(&m.table, key, uint64(value))
	return Boxed(old), ok
}

// Delete removes key and returns the removed value and whether one existed.
func (m *TypedMap[K]) Delete(key K) (Boxed, bool) {
	old, ok := remove(&m.table, key)
	return Boxed(old), ok
}

// Clear calls fn on every value and removes all entries.
func (m *TypedMap[K]) Clear(fn func(Boxed)) {
	m.table.clear(func(value uint64) { fn(Boxed(value)) })
}

// Len returns the number of entries.
func (m *TypedMap[K]) Len() int {
	return m.table.count
}

// Range calls fn on every entry in insertion order. fn must not modify m.
func (m *TypedMap[K]) Range(fn func(K, Boxed)) {
	m.table.each(func(key, value uint64) { fn(wordKey[K](key), Boxed(value)) })
}

// Get returns the value for key and whether it exists.
func (m *StringMap) Get(key string) (Boxed, bool) {
	value, ok := m.entries[key]
	return value, ok
}

// Set stores value under key and returns the replaced value and whether one existed.
func (m *StringMap) Set(key string, value Boxed) (Boxed, bool) {
	old, ok := m.entries[key]
	m.entries[key] = value
	return old, ok
}

// Delete removes key and returns the removed value and whether one existed.
func (m *StringMap) Delete(key string) (Boxed, bool) {
	old, ok := m.entries[key]
	if ok {
		delete(m.entries, key)
	}
	return old, ok
}

// Clear calls fn on every value and removes all entries in place, so a running
// iterator produces nothing more.
func (m *StringMap) Clear(fn func(Boxed)) {
	for _, value := range m.entries {
		fn(value)
	}
	clear(m.entries)
}

// Len returns the number of entries.
func (m *StringMap) Len() int {
	return len(m.entries)
}

// Range calls fn on every entry in unspecified order.
func (m *StringMap) Range(fn func(string, Boxed)) {
	for key, value := range m.entries {
		fn(key, value)
	}
}

// Get returns the entry for key and whether it exists.
func (m *Map) Get(key MapKey) (MapEntry, bool) {
	entry, ok := m.entries[key]
	return entry, ok
}

// Set stores entry under key and returns the replaced entry and whether one existed.
func (m *Map) Set(key MapKey, entry MapEntry) (MapEntry, bool) {
	old, ok := m.entries[key]
	m.entries[key] = entry
	return old, ok
}

// Delete removes key and returns the removed entry and whether one existed.
func (m *Map) Delete(key MapKey) (MapEntry, bool) {
	old, ok := m.entries[key]
	if ok {
		delete(m.entries, key)
	}
	return old, ok
}

// Clear calls fn on every entry and removes all entries in place, so a running
// iterator produces nothing more.
func (m *Map) Clear(fn func(MapEntry)) {
	for _, entry := range m.entries {
		fn(entry)
	}
	clear(m.entries)
}

// Len returns the number of entries.
func (m *Map) Len() int {
	return len(m.entries)
}

// Range calls fn on every entry in unspecified order.
func (m *Map) Range(fn func(MapKey, MapEntry)) {
	for key, entry := range m.entries {
		fn(key, entry)
	}
}

// Value reports the key this entry is indexed by, from the entry's own key
// when it holds one and from the index otherwise.
func (k MapKey) Value(entry MapEntry) Value {
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
	case KindText:
		return String(k.Text)
	default:
		return BoxedNull
	}
}

func (it *MapIterator) Next() bool {
	if it.started && it.done {
		it.current = BoxedNull
		return false
	}
	it.started = true
	if it.table != nil {
		return it.nextSlot()
	}
	if it.iter == nil || !it.iter.Next() {
		it.current = BoxedNull
		it.done = true
		return false
	}
	it.done = false
	switch it.kind {
	case mapIteratorString:
		it.current = String(it.iter.Key().String())
	case mapIteratorGeneric:
		key := it.iter.Key().Interface().(MapKey)
		entry := it.iter.Value().Interface().(MapEntry)
		it.current = key.Value(entry)
	default:
		it.current = BoxedNull
		it.done = true
		return false
	}
	return true
}

func (it *MapIterator) Current() Value {
	return it.current
}

func (it *MapIterator) Done() bool {
	return it.done
}

// Kind returns the value kind.
func (m *TypedMap[K]) Kind() Kind {
	return KindRef
}

// Type returns the value type.
func (m *TypedMap[K]) Type() Type {
	return m.Typ
}

// Refs returns the referenced values.
func (m *TypedMap[K]) Refs(dst []Ref) []Ref {
	if !m.Typ.TraceValues {
		return dst
	}
	m.table.each(func(_, value uint64) {
		if Boxed(value).Kind() == KindRef {
			dst = append(dst, Ref(Boxed(value).Ref()))
		}
	})
	return dst
}

// Clone returns a copy of m with its own entries and the same Typ and Zero. The
// values are boxed words, so the references they hold are shared, not copied.
func (m *TypedMap[K]) Clone() *TypedMap[K] {
	c := *m
	c.table.ctrl = append([]byte(nil), m.table.ctrl...)
	c.table.slots = append([]uint64(nil), m.table.slots...)
	c.table.pos, c.table.order = split(slices.Concat(m.table.pos, m.table.order), len(m.table.pos))
	c.table.epoch = nil
	return &c
}

// String returns the textual representation.
func (m *TypedMap[K]) String() string {
	parts := make([]string, 0, m.Len())
	m.Range(func(key K, value Boxed) {
		parts = append(parts, fmt.Sprintf("%s: %s", formatKey(any(key)), value.String()))
	})
	return formatMap(m.Typ, parts)
}

// Kind returns the value kind.
func (m *StringMap) Kind() Kind {
	return KindRef
}

// Type returns the value type.
func (m *StringMap) Type() Type {
	return m.Typ
}

// Refs returns the referenced values.
func (m *StringMap) Refs(dst []Ref) []Ref {
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

// Clone returns a copy of m with its own entries and the same Typ and Zero. The
// values are boxed words, so the references they hold are shared, not copied.
func (m *StringMap) Clone() *StringMap {
	c := NewStringMap(m.Typ, len(m.entries))
	c.Zero = m.Zero
	maps.Copy(c.entries, m.entries)
	return c
}

// String returns the textual representation.
func (m *StringMap) String() string {
	parts := make([]string, 0, m.Len())
	m.Range(func(key string, value Boxed) {
		parts = append(parts, fmt.Sprintf("%s: %s", String(key).String(), value.String()))
	})
	sort.Strings(parts)
	return formatMap(m.Typ, parts)
}

// Kind returns the value kind.
func (m *Map) Kind() Kind {
	return KindRef
}

// Type returns the value type.
func (m *Map) Type() Type {
	return m.Typ
}

// Refs returns the referenced values.
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

// Clone returns a copy of m with its own entries and the same Typ and Zero. The
// keys and values are boxed words, so the references they hold are shared, not
// copied.
func (m *Map) Clone() *Map {
	c := NewMapWithCapacity(m.Typ, len(m.entries))
	c.Zero = m.Zero
	maps.Copy(c.entries, m.entries)
	return c
}

// String returns the textual representation.
func (m *Map) String() string {
	parts := make([]string, 0, m.Len())
	m.Range(func(key MapKey, entry MapEntry) {
		parts = append(parts, fmt.Sprintf("%s: %s", key.String(), entry.Value.String()))
	})
	sort.Strings(parts)
	return formatMap(m.Typ, parts)
}

// Kind reports the reference kind.
func (m *MapLiteral) Kind() Kind {
	return KindRef
}

// Type returns the map literal type.
func (m *MapLiteral) Type() Type {
	return m.Typ
}

// String returns the canonical map literal, its entries in literal order.
func (m *MapLiteral) String() string {
	parts := make([]string, len(m.Keys))
	for i := range m.Keys {
		parts[i] = fmt.Sprintf("%s: %s", m.Keys[i].String(), m.Values[i].String())
	}
	return formatMap(m.Type(), parts)
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
	case KindText:
		return String(k.Text).String()
	default:
		return "<invalid>"
	}
}

func (t *MapType) Kind() Kind {
	return KindRef
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

func (t *MapType) String() string {
	return "map[" + t.Key.String() + "]" + t.Elem.String()
}

// Kind returns the value kind.
func (it *MapIterator) Kind() Kind {
	return KindRef
}

// Type returns the value type.
func (it *MapIterator) Type() Type {
	return it.typ
}

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

// String returns the textual representation.
func (it *MapIterator) String() string {
	return "map.iterator"
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

func formatMap(typ Type, parts []string) string {
	return fmt.Sprintf("%s{%s}", typ, strings.Join(parts, ", "))
}

func (m *TypedMap[K]) clone() Value {
	return m.Clone()
}

func (m *StringMap) clone() Value {
	return m.Clone()
}

func (m *Map) clone() Value {
	return m.Clone()
}

// nextSlot advances to the next live entry in insertion order. It first
// follows every rehash since its last step to the table's current epoch, then
// walks the live order, which grows with every insert.
func (it *MapIterator) nextSlot() bool {
	t := it.table
	if it.gen != t.gen {
		it.current = BoxedNull
		it.done = true
		return false
	}
	for e := it.epoch; e.next != nil; e = e.next {
		holes, _ := slices.BinarySearch(e.holes, uint32(it.pos))
		it.pos -= holes
		it.epoch = e.next
	}
	for it.pos < t.used {
		slot, ok := t.live(it.pos)
		it.pos++
		if !ok {
			continue
		}
		it.done = false
		it.current = it.decode(t.slots[2*slot])
		return true
	}
	it.current = BoxedNull
	it.done = true
	return false
}

func (it *MapIterator) decode(key uint64) Value {
	switch it.kind {
	case mapIteratorI8:
		return I8(int8(key))
	case mapIteratorI1:
		return I1(key != 0)
	case mapIteratorI32:
		return I32(int32(key))
	case mapIteratorI64:
		return I64(int64(key))
	case mapIteratorF32:
		return F32(math.Float32frombits(uint32(key)))
	default:
		return F64(math.Float64frombits(key))
	}
}

// walk starts it on m's current arrays.
func walk[K Scalar](it *MapIterator, m *TypedMap[K]) {
	t := &m.table
	if t.epoch == nil {
		t.epoch = &epoch{}
	}
	it.table, it.epoch, it.gen = t, t.epoch, t.gen
}

func get[K Scalar](t *table, key K) (uint64, bool) {
	hit, _, _ := probe(t, key)
	if hit < 0 {
		return 0, false
	}
	return t.slots[2*hit+1], true
}

func set[K Scalar](t *table, key K, value uint64) (uint64, bool) {
	hit, free, tag := probe(t, key)
	if hit >= 0 {
		old := t.slots[2*hit+1]
		t.slots[2*hit+1] = value
		return old, true
	}
	if (t.used+1)*4 > len(t.ctrl)*3 {
		rehash[K](t)
		_, free, tag = probe(t, key)
	}
	t.ctrl[free] = tag
	t.slots[2*free], t.slots[2*free+1] = fold(key), value
	t.order[t.used], t.pos[free] = uint32(free), uint32(t.used)
	t.used++
	t.count++
	return 0, false
}

// rehash moves every live entry, in insertion order, into fresh arrays,
// doubling them when more than three eighths of the slots hold live entries
// and keeping the size otherwise, so tombstones alone never force growth. If a
// walk has begun, it ends the current epoch with the order positions it
// dropped.
func rehash[K Scalar](t *table) {
	src := *t
	n := len(src.ctrl)
	if t.count*8 > n*3 {
		n *= 2
	}
	t.alloc(n)
	old := t.epoch
	var holes []uint32
	for p := range src.used {
		slot, ok := src.live(p)
		if !ok {
			if old != nil {
				holes = append(holes, uint32(p))
			}
			continue
		}
		_, free, tag := probe(t, wordKey[K](src.slots[2*slot]))
		t.ctrl[free] = tag
		t.slots[2*free], t.slots[2*free+1] = src.slots[2*slot], src.slots[2*slot+1]
		t.order[t.used], t.pos[free] = uint32(free), uint32(t.used)
		t.used++
		t.count++
	}
	if old != nil {
		t.epoch = &epoch{}
		old.next, old.holes = t.epoch, holes
	}
}

func remove[K Scalar](t *table, key K) (uint64, bool) {
	hit, _, _ := probe(t, key)
	if hit < 0 {
		return 0, false
	}
	t.ctrl[hit] = slotDead
	t.count--
	return t.slots[2*hit+1], true
}

// probe returns the slot holding key, or -1; the slot a new key goes in, the
// first tombstone on the probe or else the empty slot that ended it; and the
// control byte of key. Slots compare as Go values, so a NaN key never hits and
// -0 hits +0.
func probe[K Scalar](t *table, key K) (hit, free int, tag byte) {
	h := (fold(key) ^ t.seed) * tableHash
	i := (h >> t.shift) & t.mask
	tag = slotFull | byte(h>>(t.shift-7))&^slotFull
	free = -1
	for {
		switch t.ctrl[i] {
		case slotEmpty:
			if free < 0 {
				free = int(i)
			}
			return -1, free, tag
		case slotDead:
			if free < 0 {
				free = int(i)
			}
		case tag:
			if wordKey[K](t.slots[2*i]) == key {
				return int(i), -1, tag
			}
		}
		i = (i + 1) & t.mask
	}
}

func (t *table) init(capacity int) {
	n := tableMinCapacity
	for n*3 < capacity*4 {
		n *= 2
	}
	t.seed = rand.Uint64()
	t.alloc(n)
}

func (t *table) clear(fn func(uint64)) {
	t.each(func(_, value uint64) { fn(value) })
	t.alloc(tableMinCapacity)
	t.gen++
}

// alloc gives t fresh empty arrays of n slots: order holds the most inserts the
// table admits before it rehashes, three quarters of them, and shares one
// allocation with pos.
func (t *table) alloc(n int) {
	t.ctrl = make([]byte, n)
	t.slots = make([]uint64, 2*n)
	t.pos, t.order = split(make([]uint32, n+n*3/4), n)
	t.mask = uint64(n - 1)
	t.shift = uint64(64 - bits.TrailingZeros(uint(n)))
	t.count, t.used = 0, 0
}

// each calls fn on every live entry in insertion order.
func (t *table) each(fn func(key, value uint64)) {
	for p := range t.used {
		if slot, ok := t.live(p); ok {
			fn(t.slots[2*slot], t.slots[2*slot+1])
		}
	}
}

// split cuts buf into pos, its first n words, and order, the rest.
func split(buf []uint32, n int) (pos, order []uint32) {
	return buf[:n:n], buf[n:]
}

// live returns the slot at order position p and whether p holds its live entry.
func (t *table) live(p int) (uint32, bool) {
	slot := t.order[p]
	return slot, t.ctrl[slot]&slotFull != 0 && t.pos[slot] == uint32(p)
}

// fold returns the word of key a table stores and hashes: a zero key, -0
// included, is the zero word.
func fold[K Scalar](key K) uint64 {
	var zero K
	if key == zero {
		return 0
	}
	return keyWord(key)
}

// keyWord returns the key in the low bits of a word; wordKey inverts it.
func keyWord[K Scalar](key K) uint64 {
	switch unsafe.Sizeof(key) {
	case 1:
		return uint64(*(*uint8)(unsafe.Pointer(&key)))
	case 4:
		return uint64(*(*uint32)(unsafe.Pointer(&key)))
	default:
		return *(*uint64)(unsafe.Pointer(&key))
	}
}

func wordKey[K Scalar](word uint64) K {
	var key K
	switch unsafe.Sizeof(key) {
	case 1:
		*(*uint8)(unsafe.Pointer(&key)) = uint8(word)
	case 4:
		*(*uint32)(unsafe.Pointer(&key)) = uint32(word)
	default:
		*(*uint64)(unsafe.Pointer(&key)) = word
	}
	return key
}
