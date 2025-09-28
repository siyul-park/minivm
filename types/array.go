package types

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

type Array struct {
	Typ   *ArrayType
	kind  Kind
	width int
	bytes []byte
}

type ArrayType struct {
	Elem  Type
	kind  Kind
	width int
}

var ErrIndexOutOfRange = errors.New("index out of range")

var _ Traceable = (*Array)(nil)
var _ Type = (*ArrayType)(nil)

func NewArray(typ *ArrayType, len int) *Array {
	return &Array{
		Typ:   typ,
		kind:  typ.kind,
		width: typ.width,
		bytes: make([]byte, len*typ.width),
	}
}

func (a *Array) Get(idx int) (Boxed, error) {
	if idx < 0 || idx >= a.Len() {
		return 0, ErrIndexOutOfRange
	}
	offset := idx * a.width
	switch a.width {
	case 1:
		return Box(uint64(a.bytes[offset]), a.kind), nil
	case 2:
		return Box(uint64(binary.BigEndian.Uint16(a.bytes[offset:])), a.kind), nil
	case 4:
		return Box(uint64(binary.BigEndian.Uint32(a.bytes[offset:])), a.kind), nil
	case 8:
		return Boxed(binary.BigEndian.Uint64(a.bytes[offset:])), nil
	default:
		return 0, nil
	}
}

func (a *Array) Set(idx int, val Boxed) error {
	if idx < 0 || idx >= a.Len() {
		return ErrIndexOutOfRange
	}
	offset := idx * a.width
	switch a.width {
	case 1:
		a.bytes[offset] = byte(val)
	case 2:
		binary.BigEndian.PutUint16(a.bytes[offset:], uint16(val))
	case 4:
		binary.BigEndian.PutUint32(a.bytes[offset:], uint32(val))
	case 8:
		binary.BigEndian.PutUint64(a.bytes[offset:], uint64(val))
	default:
	}
	return nil
}

func (a *Array) Len() int {
	return len(a.bytes) / a.width
}

func (a *Array) Kind() Kind {
	return KindRef
}

func (a *Array) Type() Type {
	return a.Typ
}

func (a *Array) Interface() any {
	length := a.Len()
	value := make([]any, length)
	for i := 0; i < length; i++ {
		if v, err := a.Get(i); err == nil {
			value[i] = v.Interface()
		}
	}
	return value
}

func (a *Array) String() string {
	length := a.Len()
	var sb strings.Builder
	sb.WriteString(a.Typ.String())
	sb.WriteString("{")
	for i := 0; i < length; i++ {
		if i > 0 {
			sb.WriteString(", ")
		}
		val, err := a.Get(i)
		if err != nil {
			sb.WriteString("<err>")
			continue
		}
		sb.WriteString(fmt.Sprint(val.Interface()))
	}
	sb.WriteString("}")
	return sb.String()
}

func (a *Array) Refs() []Ref {
	if a.kind != KindRef {
		return nil
	}
	length := a.Len()
	refs := make([]Ref, 0, length)
	for i := 0; i < length; i++ {
		v := Boxed(binary.BigEndian.Uint64(a.bytes[i*a.width:]))
		if v.Kind() != KindRef {
			continue
		}
		if ref := v.Ref(); ref > 0 {
			refs = append(refs, Ref(v.Ref()))
		}
	}
	return refs
}

func NewArrayType(elem Type) *ArrayType {
	kind := elem.Kind()
	width := 0
	switch kind {
	case KindI32, KindF32:
		width = 4
	case KindI64, KindF64, KindRef:
		width = 8
	}
	return &ArrayType{Elem: elem, kind: kind, width: width}
}

func (t *ArrayType) Kind() Kind {
	return KindRef
}

func (t *ArrayType) String() string {
	return "[]" + t.Elem.String()
}

func (t *ArrayType) Cast(other Type) bool {
	return t.Equals(other)
}

func (t *ArrayType) Equals(other Type) bool {
	if o, ok := other.(*ArrayType); ok {
		return t.Elem.Equals(o.Elem)
	}
	return false
}
