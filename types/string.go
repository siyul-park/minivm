package types

import (
	"fmt"
	"unicode/utf8"
)

type String string

type StringIterator struct {
	value   String
	current Value
	ref     Ref
	offset  int
	done    bool
}

type stringType struct{}

var TypeString = stringType{}

var _ Value = String("")
var _ Traceable = (*StringIterator)(nil)
var _ Iterator = (*StringIterator)(nil)
var _ Type = stringType{}

func NewStringIterator(ref Ref, val String) *StringIterator {
	return &StringIterator{value: val, current: BoxedNull, ref: ref, done: true}
}

func (s String) Kind() Kind {
	return KindRef
}

func (s String) Type() Type {
	return TypeString
}

func (s String) String() string {
	return fmt.Sprintf("%q", string(s))
}

func (it *StringIterator) Kind() Kind { return KindRef }

func (it *StringIterator) Type() Type { return NewIteratorType(TypeI32) }

func (it *StringIterator) String() string { return "string.iterator" }

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

func (it *StringIterator) Current() Value { return it.current }

func (it *StringIterator) Done() bool { return it.done }

func (it *StringIterator) Refs() []Ref { return []Ref{it.ref} }

func (stringType) Kind() Kind {
	return KindRef
}

func (stringType) String() string {
	return "string"
}

func (stringType) Cast(other Type) bool {
	return other == TypeString
}

func (stringType) Equals(other Type) bool {
	return other == TypeString
}
