package types

import (
	"fmt"
	"math"
	"math/bits"
	"math/rand/v2"
	"slices"
	"strings"
)

// Map is a map from a key of any kind to boxed values, in insertion order.
// Its entries live in a pointer-free open-addressing table, so native code can
// read them without touching a Go map. A Map owns the references its keys and
// values hold, and releases them through its heap. A Map MUST come from
// NewMap; its zero value is not usable.
type Map struct {
	Typ   *MapType
	Zero  Boxed
	table table
	heap  *Heap
}

// MapLiteral is an unmaterialized map constant.
type MapLiteral struct {
	Typ    *MapType
	Keys   []Value
	Values []Value
}

// MapType is the type of maps from Key to Elem. KeyKind and ElemKind are
// consecutive bytes, so native code reads them with one load. KeyKind is
// KindAny for an any key, which tells an any map from a string map.
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
// and a key deleted before the iterator reaches it is never produced. It walks
// the live insertion order, so an entry inserted during the walk is produced
// too. A rehash moves the walk to the new order through the epoch it started
// on, so it never reads an entry the table gave up. Clear ends every walk.
type MapIterator struct {
	m       *Map
	epoch   *epoch
	pos     int
	gen     uint64
	current Value
	typ     Type
	ref     Ref
	started bool
	done    bool
}

// table is an open-addressing hash table from a key word to a Boxed value word
// that remembers insertion order. Native code reaches only its four slices and
// integer words. Slot i keeps its key at slots[2i], its value at slots[2i+1],
// and its state at ctrl[i]: slotEmpty, slotDead, or slotFull with seven hash
// bits, so a probe passes most occupied slots on the control byte alone.
// order[:used] lists the slots in insertion order, and pos[i] is the position
// of slot i's latest insert: position p holds a live entry while slot order[p]
// is full and pos[order[p]] is p.
//
// The capacity is a power of two. A key's hash is its base hash xor seed times
// tableHash; its home slot is the hash shifted right by shift, and its tag the
// seven bits below those. seed is random per table, so keys crafted to collide
// for the bare constant spread out; it never affects order. Probing is linear.
// An insert takes the first tombstone on its probe, else the empty slot that
// ended it, and always appends a position, so used bounds the non-empty slots.
// Once order is full, a rehash moves the live entries, in order, into fresh
// arrays and, once a walk has begun, starts a new epoch.
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

const (
	slotEmpty byte = 0
	slotDead  byte = 1
	slotFull  byte = 0x80
)

const (
	tableMinCapacity = 8
	tableHash        = 0x9E3779B97F4A7C15
)

// How a probe decides a key hit a slot whose word differs.
const (
	matchWord    byte = iota // words only
	matchContent             // the same word, or a String or I64 cell of equal value
	matchNone                // never: a NaN
)

var (
	_ Value     = (*MapLiteral)(nil)
	_ Traceable = (*Map)(nil)
	_ Traceable = (*MapIterator)(nil)
	_ Iterator  = (*MapIterator)(nil)
	_ Type      = (*MapType)(nil)
)

// NewMap returns an empty map of type typ sized for capacity entries. A nil
// heap makes an unbound map: the first reference it must read, retain, or
// release panics with ErrDetached.
func NewMap(heap *Heap, typ *MapType, capacity int) *Map {
	m := &Map{Typ: typ, Zero: Zero(typ.ElemKind), heap: heap}
	m.table.init(capacity)
	return m
}

// NewMapType returns the map type from key to elem.
func NewMapType(key Type, elem Type) *MapType {
	keyKind := key.Kind()
	if key == TypeAny() {
		keyKind = KindAny
	}
	return &MapType{
		Key:         key,
		Elem:        elem,
		KeyKind:     keyKind,
		ElemKind:    elem.Kind(),
		TraceKeys:   key.Kind() == KindRef || key.Kind() == KindI64,
		TraceValues: elem.Kind() == KindRef || elem.Kind() == KindI64,
	}
}

// NewMapIterator returns an iterator over the live map m, which is at heap ref.
func NewMapIterator(ref Ref, m *Map) *MapIterator {
	t := &m.table
	if t.epoch == nil {
		t.epoch = &epoch{}
	}
	return &MapIterator{m: m, epoch: t.epoch, gen: t.gen, typ: NewIteratorType(m.Typ.Key), ref: ref, current: BoxedNull, done: true}
}

// Accepts reports whether key is a valid key of m. A scalar is valid unless m
// is keyed by a reference type; the verifier checks its kind. For a map keyed
// by any a scalar or any reference is valid, by string a String, by another
// reference type any reference, and by i64 an I64.
func (m *Map) Accepts(key Boxed) bool {
	if uint64(key)>>VBits != refTag && m.Typ.KeyKind != KindRef {
		return true
	}
	return m.accepts(key)
}

// Load returns the value for key, which it borrows, and whether it exists.
func (m *Map) Load(key Boxed) (Boxed, bool) {
	hit, _, _, _ := m.find(key)
	if hit < 0 {
		return 0, false
	}
	return Boxed(m.table.slots[2*hit+1]), true
}

// Store takes key and value. A hit replaces the value, keeps the entry's first
// key and place, and releases the incoming key and the replaced value.
func (m *Map) Store(key, value Boxed) {
	if previous, loaded := m.Swap(key, value); loaded {
		m.drop(0, previous)
	}
}

// Swap takes key and value as Store does, but returns the replaced value to
// the caller unreleased.
func (m *Map) Swap(key, value Boxed) (Boxed, bool) {
	if uint64(key)>>VBits == refTag {
		key = m.own(key)
	}
	hit, free, tag, word := m.find(key)
	if hit < 0 {
		m.insert(free, tag, word, value)
		return 0, false
	}
	slots := m.table.slots
	previous := Boxed(slots[2*hit+1])
	slots[2*hit+1] = uint64(value)
	m.drop(key, 0)
	return previous, true
}

// LoadAndDelete removes key, which it borrows, and returns its value, which
// moves to the caller. It releases the stored key.
func (m *Map) LoadAndDelete(key Boxed) (Boxed, bool) {
	hit, _, _, _ := m.find(key)
	if hit < 0 {
		return 0, false
	}
	t := &m.table
	stored, value := Boxed(t.slots[2*hit]), Boxed(t.slots[2*hit+1])
	t.ctrl[hit] = slotDead
	t.count--
	m.drop(stored, 0)
	return value, true
}

// Delete removes key, which it borrows, and releases the stored key and value.
func (m *Map) Delete(key Boxed) {
	hit, _, _, _ := m.find(key)
	if hit < 0 {
		return
	}
	t := &m.table
	stored, value := Boxed(t.slots[2*hit]), Boxed(t.slots[2*hit+1])
	t.ctrl[hit] = slotDead
	t.count--
	m.drop(stored, value)
}

// Clear removes every entry, ends every walk, and then releases each key and
// value once.
func (m *Map) Clear() {
	t := &m.table
	old := *t
	t.alloc(tableMinCapacity)
	t.gen++
	if !m.Typ.TraceKeys && !m.Typ.TraceValues {
		return
	}
	old.each(func(key, value uint64) { m.drop(Boxed(key), Boxed(value)) })
}

// Len returns the number of entries.
func (m *Map) Len() int {
	return m.table.count
}

// Range calls f on every entry in insertion order until f returns false. It
// lends every key, so it never allocates: an i64 key outside the inline range
// is the map's own I64 cell. f must not modify m.
func (m *Map) Range(f func(key, value Boxed) bool) {
	t := &m.table
	for p := range t.used {
		slot, ok := t.live(p)
		if ok && !f(m.key(t.slots[2*slot]), Boxed(t.slots[2*slot+1])) {
			return
		}
	}
}

func (it *MapIterator) Next() bool {
	if it.started && it.done {
		it.current = BoxedNull
		return false
	}
	it.started = true
	t := &it.m.table
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
		it.current = it.m.cell(t.slots[2*slot])
		return true
	}
	it.current = BoxedNull
	it.done = true
	return false
}

// Current returns the key: a borrowed Boxed, or the I64 an i64 cell holds.
func (it *MapIterator) Current() Value {
	return it.current
}

func (it *MapIterator) Done() bool {
	return it.done
}

// Kind returns the value kind.
func (m *Map) Kind() Kind {
	return KindRef
}

// Type returns the value type.
func (m *Map) Type() Type {
	return m.Typ
}

// Refs returns the referenced values, null included.
func (m *Map) Refs(dst []Ref) []Ref {
	keys, values := m.Typ.TraceKeys, m.Typ.TraceValues
	if !keys && !values {
		return dst
	}
	m.table.each(func(key, value uint64) {
		if keys && Boxed(key).Kind() == KindRef {
			dst = append(dst, Ref(Boxed(key).Ref()))
		}
		if values && Boxed(value).Kind() == KindRef {
			dst = append(dst, Ref(Boxed(value).Ref()))
		}
	})
	return dst
}

// Clone returns a copy of m with its own entries and the same Typ, Zero, and
// heap. A bound copy retains every reference it holds, so both maps own theirs.
func (m *Map) Clone() *Map {
	c := *m
	c.table.ctrl = append([]byte(nil), m.table.ctrl...)
	c.table.slots = append([]uint64(nil), m.table.slots...)
	c.table.pos, c.table.order = split(slices.Concat(m.table.pos, m.table.order), len(m.table.pos))
	c.table.epoch = nil
	if m.heap != nil {
		m.table.each(func(key, value uint64) {
			if m.Typ.TraceKeys {
				m.heap.Retain(Boxed(key))
			}
			if m.Typ.TraceValues {
				m.heap.Retain(Boxed(value))
			}
		})
	}
	return &c
}

// String returns the textual representation, its entries in insertion order.
func (m *Map) String() string {
	parts := make([]string, 0, m.Len())
	m.Range(func(key, value Boxed) bool {
		parts = append(parts, m.format(key)+": "+value.String())
		return true
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

// String returns the canonical map literal, its entries in literal order.
func (m *MapLiteral) String() string {
	parts := make([]string, len(m.Keys))
	for i := range m.Keys {
		parts[i] = fmt.Sprintf("%s: %s", m.Keys[i].String(), m.Values[i].String())
	}
	return formatMap(m.Type(), parts)
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
		if current, ok := it.current.(Boxed); ok && current.Kind() == KindRef {
			dst = append(dst, Ref(current.Ref()))
		}
	}
	return dst
}

// String returns the textual representation.
func (it *MapIterator) String() string {
	return "map.iterator"
}

func (m *Map) clone() Value {
	return m.Clone()
}

func (m *Map) accepts(key Boxed) bool {
	isRef := uint64(key)>>VBits == refTag
	switch m.Typ.KeyKind {
	case KindAny:
		return true
	case KindRef:
		if _, ok := m.Typ.Key.(stringType); ok {
			if isRef {
				_, ok := m.bound().At(key.Ref()).(String)
				return ok
			}
			return false
		}
		return isRef
	case KindI64:
		if isRef {
			_, ok := m.bound().At(key.Ref()).(I64)
			return ok
		}
	}
	return false
}

// own makes the ref key a Swap takes safe to store: an I64 cell that fits the
// inline range becomes its inline key, a shared wide cell becomes a copy the
// map alone holds, and a sole-owned wide cell is adopted as it is. It changes
// no counts but the key's, and does so before the table is touched.
func (m *Map) own(key Boxed) Boxed {
	if m.Typ.KeyKind != KindI64 && m.Typ.KeyKind != KindRef && m.Typ.KeyKind != KindAny {
		return key
	}
	h := m.bound()
	v, ok := h.At(key.Ref()).(I64)
	switch {
	case !ok:
		return key
	case IsBoxable(int64(v)):
		h.Release(key)
		return BoxI64(int64(v))
	case h.Count(key.Ref()) == 1:
		return key
	}
	owned := BoxRef(h.Add(v))
	h.Release(key)
	return owned
}

// drop releases the references an entry's key and value hold.
func (m *Map) drop(key, value Boxed) {
	if m.Typ.TraceKeys || m.Typ.TraceValues {
		m.release(key, value)
	}
}

// release releases key and value, each when it is a traced reference.
func (m *Map) release(key, value Boxed) {
	if m.Typ.TraceKeys && key.Kind() == KindRef {
		m.bound().Release(key)
	}
	if m.Typ.TraceValues && value.Kind() == KindRef {
		m.bound().Release(value)
	}
}

// find returns the slot holding key, or -1; the slot a new key goes in, the
// first tombstone on the probe or else the empty slot that ended it; and the
// control byte and table word of key. A NaN key never hits.
func (m *Map) find(key Boxed) (hit, free int, tag byte, word uint64) {
	match := matchWord
	var base uint64
	switch m.Typ.KeyKind {
	case KindI1:
		word = min(uint64(key)&VMask, 1)
		base = word
	case KindI8:
		word = uint64(uint8(key))
		base = word
	case KindI32:
		word = uint64(uint32(key))
		base = word
	case KindI64:
		if uint64(key)>>VBits == refTag {
			word, base, match = m.resolve(key)
		} else {
			word = uint64(int64(key<<(64-VBits)) >> (64 - VBits))
			base = word
		}
	default:
		word, base, match = m.resolve(key)
	}
	t := &m.table
	h := (base ^ t.seed) * tableHash
	i := (h >> t.shift) & t.mask
	tag = slotFull | byte(h>>(t.shift-7))&^slotFull
	free = -1
	for {
		switch t.ctrl[i] {
		case slotEmpty:
			if free < 0 {
				free = int(i)
			}
			return -1, free, tag, word
		case slotDead:
			if free < 0 {
				free = int(i)
			}
		case tag:
			slot := t.slots[2*i]
			if slot == word {
				if match != matchNone {
					return int(i), -1, tag, word
				}
			} else if match == matchContent && m.equal(slot, word) {
				return int(i), -1, tag, word
			}
		}
		i = (i + 1) & t.mask
	}
}

// resolve returns the table word and the base hash of a borrowed key find does
// not read inline, a float, a key of a reference-keyed map, or a reference in
// an i64 map, and how it matches a slot. The base hash is what hash returns
// for the word. A reference key matches by value when it is a String or an I64
// cell, else by identity.
func (m *Map) resolve(key Boxed) (word, base uint64, match byte) {
	switch m.Typ.KeyKind {
	case KindF32:
		f := key.F32()
		if f == 0 {
			return 0, 0, matchWord
		}
		word = uint64(math.Float32bits(f))
		return word, word, nanMatch(float64(f))
	case KindF64:
		f := key.F64()
		if f == 0 {
			return 0, 0, matchWord
		}
		return uint64(key), uint64(key), nanMatch(f)
	}
	switch key.Kind() {
	case KindI1, KindI8, KindI32:
		word = uint64(BoxI32(key.I32()))
		return word, word, matchWord
	case KindF32:
		if f := key.F32(); f == 0 {
			word = uint64(BoxF32(0))
			return word, word, matchWord
		} else {
			return uint64(key), uint64(key), nanMatch(float64(f))
		}
	case KindF64:
		if f := key.F64(); f == 0 {
			return 0, 0, matchWord
		} else {
			return uint64(key), uint64(key), nanMatch(f)
		}
	case KindRef:
		switch v := m.bound().At(key.Ref()).(type) {
		case String:
			return uint64(key), v.Hash(), matchContent
		case I64:
			switch {
			case !IsBoxable(int64(v)):
				return uint64(key), uint64(v), matchContent
			case m.Typ.KeyKind == KindI64:
				return uint64(v), uint64(v), matchWord
			}
			word = uint64(BoxI64(int64(v)))
			return word, word, matchWord
		}
	}
	return uint64(key), uint64(key), matchWord
}

func nanMatch(f float64) byte {
	if f != f {
		return matchNone
	}
	return matchWord
}

// equal reports whether the ref key word equals the stored word by content.
func (m *Map) equal(slot, key uint64) bool {
	if slot>>VBits != refTag {
		return false
	}
	h := m.bound()
	return h.At(Boxed(slot).Ref()) == h.At(Boxed(key).Ref())
}

// insert appends a new entry at slot free, rehashing first when order is full.
func (m *Map) insert(free int, tag byte, word uint64, value Boxed) {
	t := &m.table
	if (t.used+1)*4 > len(t.ctrl)*3 {
		m.rehash()
		free, tag = t.place(m.hash(word))
	}
	t.ctrl[free] = tag
	t.slots[2*free], t.slots[2*free+1] = word, uint64(value)
	t.order[t.used], t.pos[free] = uint32(free), uint32(t.used)
	t.used++
	t.count++
}

// rehash moves every live entry, in insertion order, into fresh arrays,
// doubling them when more than three eighths of the slots hold live entries
// and keeping the size otherwise, so tombstones alone never force growth. If a
// walk has begun, it ends the current epoch with the order positions it
// dropped.
func (m *Map) rehash() {
	t := &m.table
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
		free, tag := t.place(m.hash(src.slots[2*slot]))
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

// hash returns the base hash of a table word: the word itself, the content
// hash of a String, or the value of an I64 cell.
func (m *Map) hash(word uint64) uint64 {
	if word>>VBits != refTag || m.Typ.KeyKind == KindF64 {
		return word
	}
	return m.digest(word)
}

// digest returns the base hash of a ref word, read through the heap.
func (m *Map) digest(word uint64) uint64 {
	switch v := m.bound().At(Boxed(word).Ref()).(type) {
	case String:
		return v.Hash()
	case I64:
		return uint64(v)
	}
	return word
}

// cell returns the key a guest may hold: the I64 an i64 cell holds, else the
// borrowed key.
func (m *Map) cell(word uint64) Value {
	key := m.key(word)
	if m.Typ.TraceKeys && key.Kind() == KindRef {
		if v, ok := m.bound().At(key.Ref()).(I64); ok {
			return v
		}
	}
	return key
}

// key returns the borrowed key a stored word stands for.
func (m *Map) key(word uint64) Boxed {
	switch m.Typ.KeyKind {
	case KindI1:
		return BoxI1(word != 0)
	case KindI8:
		return BoxI8(int8(word))
	case KindI32:
		return BoxI32(int32(word))
	case KindI64:
		if word>>VBits != refTag {
			return BoxI64(int64(word))
		}
	case KindF32:
		return BoxF32(math.Float32frombits(uint32(word)))
	}
	return Boxed(word)
}

// format renders a key, through the heap when it is a String or an I64 cell.
func (m *Map) format(key Boxed) string {
	if m.Typ.TraceKeys && key.Kind() == KindRef {
		switch v := m.bound().At(key.Ref()).(type) {
		case String:
			return v.String()
		case I64:
			return v.String()
		}
	}
	return key.String()
}

// bound returns the heap m's references live in.
func (m *Map) bound() *Heap {
	if m.heap == nil {
		panic(ErrDetached)
	}
	return m.heap
}

func formatMap(typ Type, parts []string) string {
	return fmt.Sprintf("%s{%s}", typ, strings.Join(parts, ", "))
}

func (t *table) init(capacity int) {
	n := tableMinCapacity
	for n*3 < capacity*4 {
		n *= 2
	}
	t.seed = rand.Uint64()
	t.alloc(n)
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

// place returns the first empty slot on the probe of the base hash and its
// control byte, for a table without tombstones.
func (t *table) place(base uint64) (int, byte) {
	h := (base ^ t.seed) * tableHash
	i := (h >> t.shift) & t.mask
	for t.ctrl[i] != slotEmpty {
		i = (i + 1) & t.mask
	}
	return int(i), slotFull | byte(h>>(t.shift-7))&^slotFull
}

// each calls fn on every live entry in insertion order.
func (t *table) each(fn func(key, value uint64)) {
	for p := range t.used {
		if slot, ok := t.live(p); ok {
			fn(t.slots[2*slot], t.slots[2*slot+1])
		}
	}
}

// live returns the slot at order position p and whether p holds its live entry.
func (t *table) live(p int) (uint32, bool) {
	slot := t.order[p]
	return slot, t.ctrl[slot]&slotFull != 0 && t.pos[slot] == uint32(p)
}

// split cuts buf into pos, its first n words, and order, the rest.
func split(buf []uint32, n int) (pos, order []uint32) {
	return buf[:n:n], buf[n:]
}
