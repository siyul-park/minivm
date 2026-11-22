package types

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"math/rand/v2"
	"unicode/utf8"
	"unsafe"
)

// StringIterator walks a string by rune.
type StringIterator struct {
	value   String
	current Value
	ref     Ref
	offset  int
	done    bool
}

// String is an immutable string value.
type String string

type stringType struct{}

// HashMix0 and HashMix1 are the multipliers String.Hash mixes with; native
// code emits the same hash with them.
const (
	HashMix0 = 0xa0761d6478bd642f
	HashMix1 = 0xe7037ed1a0b428db
)

var (
	typeString Type = stringType{}
	_          Type = stringType{}
)

// hashkey seeds String.Hash for the process. It never affects order.
var hashkey = rand.Uint64()

var _ Value = String("")

var (
	_ Traceable = (*StringIterator)(nil)
	_ Iterator  = (*StringIterator)(nil)
)

// TypeString returns the VM string type.
func TypeString() Type {
	return typeString
}

// Hashkey returns the process seed String.Hash mixes into every hash.
func Hashkey() uint64 {
	return hashkey
}

// NewStringIterator returns an iterator over val, which is at heap ref.
func NewStringIterator(ref Ref, val String) *StringIterator {
	return &StringIterator{value: val, current: BoxedNull, ref: ref, done: true}
}

func (it *StringIterator) Next() bool {
	if it.offset >= len(it.value) {
		it.current = BoxedNull
		it.done = true
		return false
	}
	r, size := utf8.DecodeRuneInString(string(it.value[it.offset:]))
	if size == 0 {
		it.current = BoxedNull
		it.done = true
		return false
	}
	it.offset += size
	it.current = I32(r)
	it.done = false
	return true
}

func (it *StringIterator) Current() Value {
	return it.current
}

func (it *StringIterator) Done() bool {
	return it.done
}

// Hash returns the content hash of s, reading only inside it, as native code
// does.
func (s String) Hash() uint64 {
	n := len(s)
	h := hashkey ^ uint64(n)
	var a, b uint64
	switch {
	case n >= 8:
		for i := 0; i+8 < n; i += 8 {
			h = mix(s.le64(i)^HashMix1, h^HashMix0)
		}
		a = s.le64(n - 8)
	case n >= 4:
		a, b = s.le32(0), s.le32(n-4)
	case n > 0:
		a = uint64(s[0])<<16 | uint64(s[n>>1])<<8 | uint64(s[n-1])
	}
	return mix(a^HashMix1, b^h^HashMix0)
}

// Kind returns the value kind.
func (it *StringIterator) Kind() Kind {
	return KindRef
}

// Type returns the value type.
func (it *StringIterator) Type() Type {
	return NewIteratorType(TypeI32())
}

func (it *StringIterator) Refs(dst []Ref) []Ref {
	return append(dst, it.ref)
}

// String returns the textual representation.
func (it *StringIterator) String() string {
	return "string.iterator"
}

// Kind returns the value kind.
func (s String) Kind() Kind {
	return KindRef
}

// Type returns the value type.
func (s String) Type() Type {
	return TypeString()
}

// String returns the textual representation.
func (s String) String() string {
	return fmt.Sprintf("%q", string(s))
}

func (stringType) Kind() Kind {
	return KindRef
}

func (stringType) Cast(other Type) bool {
	return other == TypeString()
}

func (stringType) Equals(other Type) bool {
	return other == TypeString()
}

func (stringType) String() string {
	return "string"
}

func mix(a, b uint64) uint64 {
	hi, lo := bits.Mul64(a, b)
	return hi ^ lo
}

func (s String) le64(i int) uint64 {
	return binary.LittleEndian.Uint64(unsafe.Slice(unsafe.StringData(string(s)), len(s))[i:])
}

func (s String) le32(i int) uint64 {
	return uint64(binary.LittleEndian.Uint32(unsafe.Slice(unsafe.StringData(string(s)), len(s))[i:]))
}
