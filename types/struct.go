package types

import (
	"strings"
	"unsafe"
)

type Struct struct {
	Typ  *StructType
	Data []byte
}

type StructType struct {
	Fields []StructField
	Size   int
}

type StructField struct {
	Name   string
	Type   Type
	Kind   Kind
	Size   int
	Offset int
}

var _ Traceable = (*Struct)(nil)
var _ Type = (*StructType)(nil)

func FieldWithName(name string) func(*StructField) {
	return func(f *StructField) {
		f.Name = name
	}
}

func NewStruct(typ *StructType, fields ...Boxed) *Struct {
	s := &Struct{
		Typ:  typ,
		Data: make([]byte, typ.Size),
	}
	for i, field := range fields {
		s.SetField(i, field)
	}
	return s
}

func (s *Struct) FieldByName(name string) Boxed {
	f, ok := s.Typ.FieldByName(name)
	if !ok {
		return 0
	}
	return s.field(f)
}

func (s *Struct) Field(i int) Boxed {
	typ := s.Typ
	if i < 0 || i >= len(typ.Fields) {
		return 0
	}
	return s.field(typ.Fields[i])
}

func (s *Struct) SetField(i int, val Boxed) {
	typ := s.Typ
	if i < 0 || i >= len(typ.Fields) {
		return
	}
	f := typ.Fields[i]
	offset := f.Offset
	switch f.Kind {
	case KindI32:
		*(*int32)(unsafe.Pointer(&s.Data[offset])) = val.I32()
	case KindI64:
		*(*int64)(unsafe.Pointer(&s.Data[offset])) = val.I64()
	case KindF32:
		*(*float32)(unsafe.Pointer(&s.Data[offset])) = val.F32()
	case KindF64:
		*(*float64)(unsafe.Pointer(&s.Data[offset])) = val.F64()
	case KindRef:
		*(*uint64)(unsafe.Pointer(&s.Data[offset])) = uint64(val)
	}
}

func (s *Struct) Kind() Kind {
	return KindRef
}

func (s *Struct) Type() Type {
	return s.Typ
}

func (s *Struct) String() string {
	var sb strings.Builder
	sb.WriteString(s.Typ.String())
	sb.WriteString("{")
	for i, f := range s.Typ.Fields {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(s.field(f).String())
	}
	sb.WriteString("}")
	return sb.String()
}

func (s *Struct) Refs() []Ref {
	refs := make([]Ref, 0, len(s.Typ.Fields))
	for _, f := range s.Typ.Fields {
		if f.Kind == KindRef {
			val := Boxed(*(*uint64)(unsafe.Pointer(&s.Data[f.Offset])))
			if val.Kind() == KindRef {
				refs = append(refs, Ref(val.Ref()))
			}
		}
	}
	return refs
}

func (s *Struct) field(f StructField) Boxed {
	offset := f.Offset
	switch f.Kind {
	case KindI32:
		return BoxI32(*(*int32)(unsafe.Pointer(&s.Data[offset])))
	case KindI64:
		return BoxI64(*(*int64)(unsafe.Pointer(&s.Data[offset])))
	case KindF32:
		return BoxF32(*(*float32)(unsafe.Pointer(&s.Data[offset])))
	case KindF64:
		return BoxF64(*(*float64)(unsafe.Pointer(&s.Data[offset])))
	case KindRef:
		return Boxed(*(*uint64)(unsafe.Pointer(&s.Data[offset])))
	default:
		return 0
	}
}

func NewStructType(fields ...StructField) *StructType {
	offset := 0
	for i := 0; i < len(fields); i++ {
		fields[i].Offset = offset
		align := fields[i].Size
		if align > 0 && offset%align != 0 {
			offset += align - (offset % align)
		}
		offset += fields[i].Size
	}
	align := 1
	for _, f := range fields {
		if f.Size > align {
			align = f.Size
		}
	}
	if offset%align != 0 {
		offset += align - (offset % align)
	}
	return &StructType{Fields: fields, Size: offset}
}

func (t *StructType) FieldByName(name string) (StructField, bool) {
	for _, field := range t.Fields {
		if field.Name == name {
			return field, true
		}
	}
	return StructField{}, false
}

func (t *StructType) Kind() Kind {
	return KindRef
}

func (t *StructType) String() string {
	var sb strings.Builder
	sb.WriteString("struct {")
	for i, f := range t.Fields {
		if i > 0 {
			sb.WriteString("; ")
		}
		sb.WriteString(f.Type.String())
	}
	sb.WriteString("}")
	return sb.String()
}

func (t *StructType) Cast(other Type) bool {
	if o, ok := other.(*StructType); ok {
		if o == other {
			return true
		}
		if len(t.Fields) >= len(o.Fields) {
			return false
		}
		for i, f := range o.Fields {
			if !f.Type.Equals(t.Fields[i].Type) {
				return false
			}
		}
	}
	return false
}

func (t *StructType) Equals(other Type) bool {
	if t == other {
		return true
	}
	o, ok := other.(*StructType)
	if !ok {
		return false
	}
	if len(t.Fields) != len(o.Fields) {
		return false
	}
	for i, f := range o.Fields {
		if !f.Type.Equals(t.Fields[i].Type) {
			return false
		}
	}
	return true
}

func NewStructField(typ Type, opts ...func(field *StructField)) StructField {
	kind := typ.Kind()
	size := kind.Size()
	if kind == KindRef {
		size = 8
	}
	s := StructField{
		Type: typ,
		Kind: kind,
		Size: size,
	}
	for _, opt := range opts {
		opt(&s)
	}
	return s
}
