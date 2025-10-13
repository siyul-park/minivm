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

func NewStruct(typ *StructType, fields ...Value) *Struct {
	s := &Struct{
		Typ:  typ,
		Data: make([]byte, typ.Size),
	}
	for i, field := range fields {
		s.SetField(i, field)
	}
	return s
}

func (s *Struct) FieldByName(name string) Value {
	f, ok := s.Typ.FieldByName(name)
	if !ok {
		return nil
	}
	return s.field(f)
}

func (s *Struct) Field(i int) Value {
	typ := s.Typ
	if i < 0 || i >= len(typ.Fields) {
		return nil
	}
	return s.field(typ.Fields[i])
}

func (s *Struct) SetField(i int, val Value) {
	typ := s.Typ
	if i < 0 || i >= len(typ.Fields) {
		return
	}
	f := typ.Fields[i]
	offset := f.Offset
	switch v := val.(type) {
	case I32:
		*(*int32)(unsafe.Pointer(&s.Data[offset])) = int32(v)
	case I64:
		*(*int64)(unsafe.Pointer(&s.Data[offset])) = int64(v)
	case F32:
		*(*float32)(unsafe.Pointer(&s.Data[offset])) = float32(v)
	case F64:
		*(*float64)(unsafe.Pointer(&s.Data[offset])) = float64(v)
	case Ref:
		*(*uint64)(unsafe.Pointer(&s.Data[offset])) = uint64(BoxRef(int(v)))
	case Boxed:
		*(*uint64)(unsafe.Pointer(&s.Data[offset])) = uint64(v)
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
	sb.WriteString("struct {")
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

func (s *Struct) field(f StructField) Value {
	offset := f.Offset
	switch f.Kind {
	case KindI32:
		return I32(*(*int32)(unsafe.Pointer(&s.Data[offset])))
	case KindI64:
		return I64(*(*int64)(unsafe.Pointer(&s.Data[offset])))
	case KindF32:
		return F32(*(*float32)(unsafe.Pointer(&s.Data[offset])))
	case KindF64:
		return F64(*(*float64)(unsafe.Pointer(&s.Data[offset])))
	case KindRef:
		return Boxed(*(*uint64)(unsafe.Pointer(&s.Data[offset])))
	default:
		return nil
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

func NewStructField(typ Type) StructField {
	kind := typ.Kind()
	size := kind.Size()
	if kind == KindRef {
		size = 8
	}
	return StructField{
		Type: typ,
		Kind: kind,
		Size: size,
	}
}
