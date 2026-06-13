package types

import (
	"math"
	"strings"
)

type Struct struct {
	Typ    *StructType
	Data   []uint64
	inline [4]uint64
}

type StructType struct {
	Fields []StructField
}

type StructField struct {
	Name string
	Type Type
	Kind Kind
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
		Typ: typ,
	}
	if len(typ.Fields) <= len(s.inline) {
		s.Data = s.inline[:len(typ.Fields)]
	} else {
		s.Data = make([]uint64, len(typ.Fields))
	}
	for i, field := range fields {
		s.SetField(i, field)
	}
	return s
}

func (s *Struct) FieldByName(name string) Boxed {
	for i, f := range s.Typ.Fields {
		if f.Name == name {
			return s.field(i, f)
		}
	}
	return 0
}

func (s *Struct) Field(i int) Boxed {
	if i < 0 || i >= len(s.Typ.Fields) {
		return 0
	}
	return s.field(i, s.Typ.Fields[i])
}

func (s *Struct) SetField(i int, val Boxed) {
	if i < 0 || i >= len(s.Typ.Fields) {
		return
	}
	switch s.Typ.Fields[i].Kind {
	case KindI32:
		s.Data[i] = uint64(uint32(val.I32()))
	case KindI64:
		s.Data[i] = uint64(val.I64())
	case KindF32:
		s.Data[i] = uint64(math.Float32bits(val.F32()))
	case KindF64:
		s.Data[i] = math.Float64bits(val.F64())
	case KindRef:
		s.Data[i] = uint64(val)
	}
}

// Raw returns the raw 64-bit slot for field i without per-kind decoding.
// Used by the interpreter when it needs the underlying bits (e.g. to box
// an i64 through its sidetable, or to inspect a ref slot for retain/release).
func (s *Struct) Raw(i int) uint64 {
	if i < 0 || i >= len(s.Data) {
		return 0
	}
	return s.Data[i]
}

// SetRaw writes raw 64-bit bits into slot i without per-kind encoding.
func (s *Struct) SetRaw(i int, bits uint64) {
	if i < 0 || i >= len(s.Data) {
		return
	}
	s.Data[i] = bits
}

func (s *Struct) Kind() Kind {
	return KindRef
}

func (s *Struct) Type() Type {
	return s.Typ
}

func (s *Struct) String() string {
	return formatSlice(s.Typ, len(s.Typ.Fields), func(i int) string {
		return s.field(i, s.Typ.Fields[i]).String()
	})
}

func (s *Struct) Refs() []Ref {
	var refs []Ref
	for i, f := range s.Typ.Fields {
		if f.Kind != KindRef {
			continue
		}
		val := Boxed(s.Data[i])
		if val.Kind() == KindRef {
			if refs == nil {
				refs = make([]Ref, 0, len(s.Typ.Fields))
			}
			refs = append(refs, Ref(val.Ref()))
		}
	}
	return refs
}

func (s *Struct) field(i int, f StructField) Boxed {
	bits := s.Data[i]
	switch f.Kind {
	case KindI32:
		return BoxI32(int32(uint32(bits)))
	case KindI64:
		return BoxI64(int64(bits))
	case KindF32:
		return BoxF32(math.Float32frombits(uint32(bits)))
	case KindF64:
		return BoxF64(math.Float64frombits(bits))
	case KindRef:
		return Boxed(bits)
	default:
		return 0
	}
}

func NewStructType(fields ...StructField) *StructType {
	return &StructType{Fields: fields}
}

func (t *StructType) FieldByName(name string) (StructField, bool) {
	for _, field := range t.Fields {
		if field.Name == name {
			return field, true
		}
	}
	return StructField{}, false
}

// FieldIndex returns the index of the field named name, or -1 if no such
// field exists.
func (t *StructType) FieldIndex(name string) int {
	for i, field := range t.Fields {
		if field.Name == name {
			return i
		}
	}
	return -1
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
	s := StructField{
		Type: typ,
		Kind: typ.Kind(),
	}
	for _, opt := range opts {
		opt(&s)
	}
	return s
}
