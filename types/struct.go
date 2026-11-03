package types

import (
	"math"
	"strings"
)

// Struct is a heap record whose fields are laid out by its StructType.
type Struct struct {
	Typ    *StructType
	Data   []uint64
	inline [4]uint64
}

// StructLiteral is an unmaterialized struct constant.
type StructLiteral struct {
	Typ    *StructType
	Fields []Value
}

// StructType is the type of structs with the given fields.
type StructType struct {
	Fields []StructField
}

// StructField is one named, typed field of a StructType.
type StructField struct {
	Name string
	Type Type
	Kind Kind
}

var _ Value = (*StructLiteral)(nil)

var _ Traceable = (*Struct)(nil)

var _ Type = (*StructType)(nil)

// FieldWithName returns a field option that sets its name.
func FieldWithName(name string) func(*StructField) {
	return func(f *StructField) {
		f.Name = name
	}
}

// NewStruct returns a struct of type typ with fields assigned in order.
func NewStruct(typ *StructType, fields ...Boxed) *Struct {
	s := &Struct{}
	s.Reset(typ)
	for i, field := range fields {
		s.SetField(i, field)
	}
	return s
}

// NewStructType returns the struct type with fields.
func NewStructType(fields ...StructField) *StructType {
	return &StructType{Fields: fields}
}

// NewStructField returns a field of type typ configured by opts.
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

// Reset prepares s for reuse by an interpreter-owned struct pool. Small structs
// keep their data inline, so reinitialization does not allocate.
func (s *Struct) Reset(typ *StructType) {
	s.Typ = typ
	if len(typ.Fields) <= len(s.inline) {
		s.Data = s.inline[:len(typ.Fields)]
		clear(s.Data)
		return
	}
	s.Data = make([]uint64, len(typ.Fields))
}

// FieldByName returns the named field, or 0 when it does not exist.
func (s *Struct) FieldByName(name string) Boxed {
	return s.Field(s.Typ.FieldIndex(name))
}

// Field returns field i, or 0 when out of range.
func (s *Struct) Field(i int) Boxed {
	if i < 0 || i >= len(s.Typ.Fields) {
		return 0
	}
	return s.field(i, s.Typ.Fields[i])
}

// SetField stores val into field i, ignoring an index out of range.
func (s *Struct) SetField(i int, val Boxed) {
	if i < 0 || i >= len(s.Typ.Fields) {
		return
	}
	switch s.Typ.Fields[i].Kind {
	case KindI32:
		s.Data[i] = uint64(uint32(val.I32()))
	case KindI8:
		s.Data[i] = uint64(uint32(int32(val.I8())))
	case KindI1:
		if val.Bool() {
			s.Data[i] = 1
		} else {
			s.Data[i] = 0
		}
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

// FieldByName returns the field with the given name.
func (t *StructType) FieldByName(name string) (StructField, bool) {
	idx := t.FieldIndex(name)
	if idx < 0 {
		return StructField{}, false
	}
	return t.Fields[idx], true
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

func (s *Struct) Kind() Kind {
	return KindRef
}

func (s *Struct) Type() Type {
	return s.Typ
}

func (s *Struct) Refs(dst []Ref) []Ref {
	for i, f := range s.Typ.Fields {
		if f.Kind != KindRef {
			continue
		}
		val := Boxed(s.Data[i])
		if val.Kind() == KindRef {
			dst = append(dst, Ref(val.Ref()))
		}
	}
	return dst
}

// Clone returns a copy of s with its own field storage. The fields are boxed
// words, so the references they hold are shared, not copied.
func (s *Struct) Clone() *Struct {
	c := NewStruct(s.Typ)
	copy(c.Data, s.Data)
	return c
}

func (s *Struct) String() string {
	return formatSlice(s.Typ, len(s.Typ.Fields), func(i int) string {
		return s.field(i, s.Typ.Fields[i]).String()
	})
}

// Kind reports the reference kind.
func (s *StructLiteral) Kind() Kind {
	return KindRef
}

// Type returns the struct literal type.
func (s *StructLiteral) Type() Type {
	return s.Typ
}

// String returns the canonical struct literal.
func (s *StructLiteral) String() string {
	return formatSlice(s.Type(), len(s.Fields), func(i int) string { return s.Fields[i].String() })
}

func (t *StructType) Kind() Kind {
	return KindRef
}

func (t *StructType) Cast(other Type) bool {
	if t == other {
		return true
	}
	o, ok := other.(*StructType)
	if !ok || len(t.Fields) > len(o.Fields) {
		return false
	}
	for i, f := range t.Fields {
		if !f.Type.Equals(o.Fields[i].Type) {
			return false
		}
	}
	return true
}

// Equals compares field types only. Two structs whose fields differ just in
// name are equal here but render differently from String, which does show
// names, and FieldIndex does treat a name as meaningful. Structural
// compatibility is the contract; do not use Equals to decide whether two types
// print alike.
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

func (t *StructType) String() string {
	var sb strings.Builder
	sb.WriteString("struct {")
	for i, f := range t.Fields {
		if i > 0 {
			sb.WriteString("; ")
		}
		if f.Name != "" {
			sb.WriteString(f.Name)
			sb.WriteString(": ")
		}
		sb.WriteString(f.Type.String())
	}
	sb.WriteString("}")
	return sb.String()
}

func (s *Struct) field(i int, f StructField) Boxed {
	return BoxWord(f.Kind, s.Data[i])
}

func (s *Struct) cloneValue() Value {
	return s.Clone()
}
