package types

import (
	"fmt"
	"unicode/utf8"
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

var (
	typeString Type = stringType{}
	_          Type = stringType{}
)

var _ Value = String("")

var (
	_ Traceable = (*StringIterator)(nil)
	_ Iterator  = (*StringIterator)(nil)
)

// TypeString returns the VM string type.
func TypeString() Type {
	return typeString
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
