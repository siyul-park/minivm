package types

import (
	"fmt"
	"maps"
	"math"
	"math/bits"
	"reflect"
	"sort"
	"strings"
	"unsafe"
)

// TypedMap is a map from a primitive or string key to boxed values. A numeric
// key lives in the pointer-free open-addressing table so native code can reach
// it from this struct's address; a string key lives in a Go map. Exactly one of
// the two is in use, chosen by K at construction. Iteration follows slot order
// for numeric keys and is unspecified for string keys.
type TypedMap[K comparable] struct {
	Typ     *MapType
	Zero    Boxed
	entries map[K]Boxed
	table   table
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
// or generic map relies on Go's range guarantee; a numeric table walks slots in
// order, and after a rehash it keeps walking the slots it started on and
// produces a key only while the live table still holds it. Clear ends the walk.
type MapIterator struct {
	iter    *reflect.MapIter
	table   *table
	slots   []uint64
	ctrl    []byte
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
// value word. It holds no Go pointers besides its two slices. Slot i keeps its
// key at slots[2i] and its value at slots[2i+1], and its state at ctrl[i]. The
// capacity is a power of two, mask is capacity-1, and a key's home slot is its
// multiply-shift hash >> shift. Probing is linear; a deleted slot becomes a
// tombstone, and a rehash into fresh arrays drops tombstones. Floats follow Go
// map keys: -0 and +0 are one key (the latest stored bits are reported), and a
// NaN never matches, so every Set of NaN adds an entry that only Clear removes.
type table struct {
	ctrl  []byte
	slots []uint64
	mask  uint64
	shift uint64
	count int
	used  int
	gen   uint64
	neg   uint64
	abs   uint64
	inf   uint64
}

type mapIteratorKind byte

// KindText marks a MapKey indexed by string content rather than by heap
// reference. It discriminates map keys only and is never an operand kind, so
// an empty string key stays distinct from a null reference key.
const KindText Kind = 0xFE

const (
	slotEmpty byte = iota
	slotFull
	slotDead
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

// Native code reads a TypedMap's table at these byte offsets from the map
// base; a layout change must fail to compile here until native code is updated.
const tableOffset = unsafe.Offsetof(TypedMap[int64]{}.table)

var (
	_ = [1]byte{}[tableOffset+unsafe.Offsetof(table{}.ctrl)-24]
	_ = [1]byte{}[tableOffset+unsafe.Offsetof(table{}.slots)-48]
	_ = [1]byte{}[tableOffset+unsafe.Offsetof(table{}.mask)-72]
	_ = [1]byte{}[tableOffset+unsafe.Offsetof(table{}.shift)-80]
	_ = [1]byte{}[tableOffset+unsafe.Offsetof(table{}.count)-88]
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
	_ Traceable = (*TypedMap[string])(nil)
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
			return NewTypedMap[string](typ, capacity)
		}
	}
	return NewMapWithCapacity(typ, capacity)
}

// NewTypedMap creates a typed map.
func NewTypedMap[K comparable](typ *MapType, capacity int) *TypedMap[K] {
	m := &TypedMap[K]{Typ: typ, Zero: Zero(typ.ElemKind)}
	switch any(*new(K)).(type) {
	case bool, int8, int32, int64:
		m.table.init(capacity, 0, 0, 0)
	case float32:
		m.table.init(capacity, 1<<31, 1<<31-1, 0x7F800000)
	case float64:
		m.table.init(capacity, 1<<63, 1<<63-1, 0x7FF0000000000000)
	default:
		m.entries = make(map[K]Boxed, capacity)
	}
	return m
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
		it.walk(&m.table)
	case *TypedMap[bool]:
		it.typ = NewIteratorType(m.Typ.Key)
		it.kind = mapIteratorI1
		it.walk(&m.table)
	case *TypedMap[int32]:
		it.typ = NewIteratorType(m.Typ.Key)
		it.kind = mapIteratorI32
		it.walk(&m.table)
	case *TypedMap[int64]:
		it.typ = NewIteratorType(m.Typ.Key)
		it.kind = mapIteratorI64
		it.walk(&m.table)
	case *TypedMap[float32]:
		it.typ = NewIteratorType(m.Typ.Key)
		it.kind = mapIteratorF32
		it.walk(&m.table)
	case *TypedMap[float64]:
		it.typ = NewIteratorType(m.Typ.Key)
		it.kind = mapIteratorF64
		it.walk(&m.table)
	case *TypedMap[string]:
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
	if m.entries != nil {
		value, ok := m.entries[key]
		return value, ok
	}
	value, ok := m.table.get(keyWord(key))
	return Boxed(value), ok
}

// Set stores value under key and returns the replaced value and whether one existed.
func (m *TypedMap[K]) Set(key K, value Boxed) (Boxed, bool) {
	if m.entries != nil {
		old, ok := m.entries[key]
		m.entries[key] = value
		return old, ok
	}
	old, ok := m.table.set(keyWord(key), uint64(value))
	return Boxed(old), ok
}

// Delete removes key and returns the removed value and whether one existed.
func (m *TypedMap[K]) Delete(key K) (Boxed, bool) {
	if m.entries != nil {
		old, ok := m.entries[key]
		if ok {
			delete(m.entries, key)
		}
		return old, ok
	}
	old, ok := m.table.delete(keyWord(key))
	return Boxed(old), ok
}

// Clear calls fn on every value and removes all entries.
func (m *TypedMap[K]) Clear(fn func(Boxed)) {
	if m.entries != nil {
		for _, value := range m.entries {
			fn(value)
		}
		m.entries = make(map[K]Boxed)
		return
	}
	m.table.clear(func(value uint64) { fn(Boxed(value)) })
}

// Len returns the number of entries.
func (m *TypedMap[K]) Len() int {
	if m.entries != nil {
		return len(m.entries)
	}
	return m.table.count
}

// Range calls fn on every entry, in slot order for numeric keys and in
// unspecified order otherwise. fn must not modify m.
func (m *TypedMap[K]) Range(fn func(K, Boxed)) {
	if m.entries != nil {
		for key, value := range m.entries {
			fn(key, value)
		}
		return
	}
	m.table.each(func(key, value uint64) { fn(wordKey[K](key), Boxed(value)) })
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

// Clear calls fn on every entry and removes all entries.
func (m *Map) Clear(fn func(MapEntry)) {
	for _, entry := range m.entries {
		fn(entry)
	}
	m.entries = make(map[MapKey]MapEntry)
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
	for _, value := range m.entries {
		if value.Kind() == KindRef {
			dst = append(dst, Ref(value.Ref()))
		}
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
	if m.entries != nil {
		c.entries = maps.Clone(m.entries)
	}
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

// String returns the canonical map literal.
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
	case string:
		return String(v).String()
	default:
		return fmt.Sprint(v)
	}
}

func formatMap(typ Type, parts []string) string {
	sort.Strings(parts)
	return fmt.Sprintf("%s{%s}", typ, strings.Join(parts, ", "))
}

func (m *TypedMap[K]) cloneValue() Value {
	return m.Clone()
}

func (m *Map) cloneValue() Value {
	return m.Clone()
}

// nextSlot advances to the next full slot. A rehash leaves the iterator on the
// slots it started on, so a key found there is produced only while the live
// table still holds it; a NaN key cannot be deleted and needs no check.
func (it *MapIterator) nextSlot() bool {
	if it.gen != it.table.gen {
		it.current = BoxedNull
		it.done = true
		return false
	}
	rehashed := unsafe.SliceData(it.slots) != unsafe.SliceData(it.table.slots)
	for it.pos < len(it.ctrl) {
		i := it.pos
		it.pos++
		if it.ctrl[i] != slotFull {
			continue
		}
		key := it.slots[2*i]
		if rehashed && !it.table.isNaN(key) {
			if _, ok := it.table.get(key); !ok {
				continue
			}
		}
		it.done = false
		it.current = it.decode(key)
		return true
	}
	it.current = BoxedNull
	it.done = true
	return false
}

func (it *MapIterator) walk(t *table) {
	it.table, it.slots, it.ctrl, it.gen = t, t.slots, t.ctrl, t.gen
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

func (t *table) init(capacity int, neg, abs, inf uint64) {
	t.neg, t.abs, t.inf = neg, abs, inf
	n := tableMinCapacity
	for n*3 < capacity*4 {
		n *= 2
	}
	t.alloc(n)
}

func (t *table) get(key uint64) (uint64, bool) {
	if t.isNaN(key) {
		return 0, false
	}
	hit, _ := t.probe(key, true)
	if hit < 0 {
		return 0, false
	}
	return t.slots[2*hit+1], true
}

func (t *table) set(key, value uint64) (uint64, bool) {
	nan := t.isNaN(key)
	hit, free := t.probe(key, !nan)
	if hit >= 0 && !nan {
		old := t.slots[2*hit+1]
		t.slots[2*hit], t.slots[2*hit+1] = key, value
		return old, true
	}
	if t.ctrl[free] == slotEmpty {
		if (t.used+1)*4 > len(t.ctrl)*3 {
			t.rehash()
			_, free = t.probe(key, false)
		}
		t.used++
	}
	t.ctrl[free] = slotFull
	t.slots[2*free], t.slots[2*free+1] = key, value
	t.count++
	return 0, false
}

// rehash moves every entry into fresh arrays, doubling them when more than
// three eighths of the slots hold live entries and keeping the size otherwise
// so tombstones alone never force growth.
func (t *table) rehash() {
	ctrl, slots := t.ctrl, t.slots
	n := len(ctrl)
	if t.count*8 > n*3 {
		n *= 2
	}
	t.alloc(n)
	for i, state := range ctrl {
		if state != slotFull {
			continue
		}
		_, free := t.probe(slots[2*i], false)
		t.ctrl[free] = slotFull
		t.slots[2*free], t.slots[2*free+1] = slots[2*i], slots[2*i+1]
		t.count++
		t.used++
	}
}

func (t *table) delete(key uint64) (uint64, bool) {
	if t.isNaN(key) {
		return 0, false
	}
	hit, _ := t.probe(key, true)
	if hit < 0 {
		return 0, false
	}
	old := t.slots[2*hit+1]
	t.ctrl[hit] = slotDead
	t.count--
	return old, true
}

func (t *table) clear(fn func(uint64)) {
	t.each(func(_, value uint64) { fn(value) })
	t.alloc(tableMinCapacity)
	t.gen++
}

func (t *table) alloc(n int) {
	t.ctrl = make([]byte, n)
	t.slots = make([]uint64, 2*n)
	t.mask = uint64(n - 1)
	t.shift = uint64(64 - bits.TrailingZeros(uint(n)))
	t.count, t.used = 0, 0
}

func (t *table) each(fn func(key, value uint64)) {
	slots, ctrl := t.slots, t.ctrl
	for i, state := range ctrl {
		if state == slotFull {
			fn(slots[2*i], slots[2*i+1])
		}
	}
}

// probe returns the slot holding key, or -1, and the slot a new key would
// take: the first tombstone on the path, else the empty slot that ended it.
// A key probed with match false never hits, which is how a NaN key is added.
func (t *table) probe(key uint64, match bool) (hit, free int) {
	canon := key
	if canon == t.neg {
		canon = 0
	}
	i := (canon * tableHash >> t.shift) & t.mask
	free = -1
	for {
		switch t.ctrl[i] {
		case slotEmpty:
			if free < 0 {
				free = int(i)
			}
			return -1, free
		case slotDead:
			if free < 0 {
				free = int(i)
			}
		default:
			if stored := t.slots[2*i]; match && (stored == canon || (canon == 0 && stored == t.neg)) {
				return int(i), free
			}
		}
		i = (i + 1) & t.mask
	}
}

func (t *table) isNaN(key uint64) bool {
	return key&t.abs > t.inf
}

// keyWord returns the numeric key in the low bits of a word; wordKey inverts it.
func keyWord[K comparable](key K) uint64 {
	switch unsafe.Sizeof(key) {
	case 1:
		return uint64(*(*uint8)(unsafe.Pointer(&key)))
	case 4:
		return uint64(*(*uint32)(unsafe.Pointer(&key)))
	default:
		return *(*uint64)(unsafe.Pointer(&key))
	}
}

func wordKey[K comparable](word uint64) K {
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
