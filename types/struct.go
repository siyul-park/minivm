package types

import (
	"math"
	"strings"
	"unsafe"
)

// Struct is a heap record whose fields are laid out by its StructType.
type Struct struct {
	Typ *StructType
	// data is the first of the words of the fields of Typ, one per field: inline
	// for up to four fields. A pointer instead of a slice keeps the header in the
	// 64-byte size class.
	data   *uint64
	inline [4]uint64
	binding
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

// NewStruct returns a struct of type typ with fields assigned in order. A
// non-nil heap binds it and supplies a struct of up to four fields it kept for
// reuse.
func NewStruct(heap *Heap, typ *StructType, fields ...Boxed) *Struct {
	s, ok := (*Struct)(nil), false
	if heap != nil && len(typ.Fields) <= len(Struct{}.inline) {
		s, ok = heap.structs.get()
	}
	if !ok {
		s = &Struct{}
	}
	s.reset(typ)
	s.heap = heap
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

// FieldByName returns the named field, or 0 when it does not exist.
func (s *Struct) FieldByName(name string) Boxed {
	i := s.Typ.FieldIndex(name)
	if i < 0 {
		return 0
	}
	return s.Field(i)
}

// Field returns field i, which s borrows. A wide i64 truncates. It indexes like
// a slice.
func (s *Struct) Field(i int) Boxed {
	return s.field(i, s.Typ.Fields[i])
}

// SetField takes val as field i, encoded by the field's kind. A reference field
// releases the word it replaces after the store, and panics with ErrDetached
// when it must and s is unbound. It indexes like a slice.
func (s *Struct) SetField(i int, val Boxed) {
	kind := s.Typ.Fields[i].Kind
	words := s.words()
	if kind != KindRef {
		words[i] = encode(kind, val, words[i])
		return
	}
	old := Boxed(words[i])
	if !owns(old) {
		words[i] = uint64(val)
		return
	}
	h := s.bound()
	words[i] = uint64(val)
	h.Release(old)
}

// SwapField takes val as reference field i and returns the word it replaces, which
// the caller now owns.
//
//gcassert:inline
func (s *Struct) SwapField(i int, val Boxed) Boxed {
	words := s.words()
	old := Boxed(words[i])
	words[i] = uint64(val)
	return old
}

// Raw returns the stored word of field i without per-kind decoding. It
// indexes like a slice.
//
//gcassert:inline
func (s *Struct) Raw(i int) uint64 {
	return s.words()[i]
}

// SetRaw stores w as field i without per-kind encoding or counting. It MUST
// NOT name a reference field.
//
//gcassert:inline
func (s *Struct) SetRaw(i int, w uint64) {
	s.words()[i] = w
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
	words := s.words()
	for i, f := range s.Typ.Fields {
		if f.Kind != KindRef {
			continue
		}
		val := Boxed(words[i])
		if val.Kind() == KindRef {
			dst = append(dst, Ref(val.Ref()))
		}
	}
	return dst
}

// Clone returns a copy of s with its own field storage. A bound copy retains
// every reference field, null included, so both structs own theirs. An unbound
// copy shares its words.
func (s *Struct) Clone() *Struct {
	c := NewStruct(s.heap, s.Typ)
	words := s.words()
	copy(c.words(), words)
	if s.heap != nil {
		for i, f := range s.Typ.Fields {
			if f.Kind == KindRef {
				s.heap.Retain(Boxed(words[i]))
			}
		}
	}
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

// reset gives s the type typ with every field word zero. Up to four fields
// keep their words inline, so reuse does not allocate.
func (s *Struct) reset(typ *StructType) {
	s.Typ = typ
	if len(typ.Fields) <= len(s.inline) {
		s.inline = [4]uint64{}
		s.data = &s.inline[0]
		return
	}
	s.data = unsafe.SliceData(make([]uint64, len(typ.Fields)))
}

func (s *Struct) field(i int, f StructField) Boxed {
	return BoxWord(f.Kind, s.words()[i])
}

// words returns the field words of s, one per field of its type.
func (s *Struct) words() []uint64 {
	return unsafe.Slice(s.data, len(s.Typ.Fields))
}

func (s *Struct) clone() Value {
	return s.Clone()
}

// encode returns the stored word of val for a field of the given kind, or old
// for a kind with no encoding.
func encode(kind Kind, val Boxed, old uint64) uint64 {
	switch kind {
	case KindI32:
		return uint64(uint32(val.I32()))
	case KindI8:
		return uint64(uint32(int32(val.I8())))
	case KindI1:
		if val.Bool() {
			return 1
		}
		return 0
	case KindI64:
		return uint64(val.I64())
	case KindF32:
		return uint64(math.Float32bits(val.F32()))
	case KindF64:
		return math.Float64bits(val.F64())
	case KindRef:
		return uint64(val)
	}
	return old
}
