package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFieldWithName(t *testing.T) {
	field := NewStructField(TypeI32, FieldWithName("value"))
	require.Equal(t, "value", field.Name)
}

func TestNewStruct(t *testing.T) {
	t.Run("initial fields", func(t *testing.T) {
		typ := NewStructType(NewStructField(TypeI32), NewStructField(TypeRef))
		s := NewStruct(typ, BoxI32(1), BoxRef(2))
		require.Same(t, typ, s.Typ)
		require.Equal(t, BoxI32(1), s.Field(0))
		require.Equal(t, BoxRef(2), s.Field(1))
	})

	t.Run("small storage", func(t *testing.T) {
		typ := NewStructType(NewStructField(TypeI32), NewStructField(TypeI32))
		s := NewStruct(typ)
		require.Len(t, s.Data, 2)
	})

	t.Run("large storage", func(t *testing.T) {
		typ := NewStructType(
			NewStructField(TypeI32),
			NewStructField(TypeI32),
			NewStructField(TypeI32),
			NewStructField(TypeI32),
			NewStructField(TypeI32),
		)
		s := NewStruct(typ)
		require.Len(t, s.Data, 5)
	})
}

func TestStruct_FieldByName(t *testing.T) {
	s := NewStruct(NewStructType(NewStructField(TypeI32, FieldWithName("foo"))))

	require.Equal(t, int32(0), s.FieldByName("foo").I32())
	require.Zero(t, s.FieldByName("missing"))
}

func TestStruct_Field(t *testing.T) {
	s := NewStruct(NewStructType(NewStructField(TypeI32)))

	require.Equal(t, int32(0), s.Field(0).I32())
	require.Zero(t, s.Field(1))
}

func TestStruct_SetField(t *testing.T) {
	s := NewStruct(NewStructType(
		NewStructField(TypeI32),
		NewStructField(TypeI64),
		NewStructField(TypeF32),
		NewStructField(TypeF64),
		NewStructField(TypeRef),
	))

	s.SetField(0, BoxI32(1))
	s.SetField(1, BoxI64(2))
	s.SetField(2, BoxF32(3))
	s.SetField(3, BoxF64(4))
	s.SetField(4, BoxRef(5))
	s.SetField(5, BoxRef(6))

	require.Equal(t, int32(1), s.Field(0).I32())
	require.Equal(t, int64(2), s.Field(1).I64())
	require.Equal(t, float32(3), s.Field(2).F32())
	require.Equal(t, float64(4), s.Field(3).F64())
	require.Equal(t, 5, s.Field(4).Ref())
}

func TestStruct_Raw(t *testing.T) {
	s := NewStruct(NewStructType(NewStructField(TypeI64)))
	s.SetRaw(0, 42)

	require.Equal(t, uint64(42), s.Raw(0))
	require.Zero(t, s.Raw(1))
}

func TestStruct_SetRaw(t *testing.T) {
	s := NewStruct(NewStructType(NewStructField(TypeI64)))
	s.SetRaw(0, 42)
	s.SetRaw(1, 99)

	require.Equal(t, uint64(42), s.Raw(0))
}

func TestStruct_Kind(t *testing.T) {
	s := NewStruct(NewStructType())
	require.Equal(t, KindRef, s.Kind())
}

func TestStruct_Type(t *testing.T) {
	s := NewStruct(NewStructType())
	require.Equal(t, NewStructType(), s.Type())
}

func TestStruct_String(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		s := NewStruct(NewStructType())
		require.Equal(t, "struct {}{}", s.String())
	})

	t.Run("fields", func(t *testing.T) {
		s := NewStruct(NewStructType(NewStructField(TypeI32), NewStructField(TypeRef)), BoxI32(1), BoxRef(2))
		require.Equal(t, "struct {i32; ref}{1, 2}", s.String())
	})
}

func TestStruct_Refs(t *testing.T) {
	t.Run("primitive fields", func(t *testing.T) {
		s := NewStruct(NewStructType(NewStructField(TypeI32)), BoxI32(1))

		require.Equal(t, []Ref{9}, s.Refs([]Ref{9}))
		var refs []Ref
		allocs := testing.AllocsPerRun(100, func() {
			refs = s.Refs(nil)
		})
		require.Empty(t, refs)
		require.Zero(t, allocs)
	})

	t.Run("reference fields", func(t *testing.T) {
		s := NewStruct(
			NewStructType(NewStructField(TypeRef), NewStructField(TypeI32), NewStructField(TypeRef)),
			BoxRef(1), BoxI32(2), BoxRef(3),
		)

		require.Equal(t, []Ref{9, 1, 3}, s.Refs([]Ref{9}))
	})
}

func TestNewStructType(t *testing.T) {
	fields := []StructField{NewStructField(TypeI32), NewStructField(TypeRef)}
	typ := NewStructType(fields...)
	require.Equal(t, fields, typ.Fields)
}

func TestStructType_FieldByName(t *testing.T) {
	typ := NewStructType(NewStructField(TypeI32, FieldWithName("foo")))

	field, ok := typ.FieldByName("foo")
	require.True(t, ok)
	require.Equal(t, TypeI32, field.Type)

	_, ok = typ.FieldByName("missing")
	require.False(t, ok)
}

func TestStructType_FieldIndex(t *testing.T) {
	typ := NewStructType(NewStructField(TypeI32, FieldWithName("foo")))

	require.Equal(t, 0, typ.FieldIndex("foo"))
	require.Equal(t, -1, typ.FieldIndex("missing"))
}

func TestStructType_Kind(t *testing.T) {
	require.Equal(t, KindRef, NewStructType().Kind())
}

func TestStructType_String(t *testing.T) {
	require.Equal(t, "struct {i32; ref}", NewStructType(NewStructField(TypeI32), NewStructField(TypeRef)).String())
}

func TestStructType_Cast(t *testing.T) {
	typ := NewStructType(NewStructField(TypeI32))

	require.True(t, typ.Cast(typ))
	require.True(t, typ.Cast(NewStructType(NewStructField(TypeI32), NewStructField(TypeI64))))
	require.True(t, typ.Cast(NewStructType(NewStructField(TypeI64))))
	require.False(t, typ.Cast(TypeI32))
}

func TestStructType_Equals(t *testing.T) {
	typ := NewStructType(NewStructField(TypeI32), NewStructField(TypeRef))

	require.True(t, typ.Equals(typ))
	require.True(t, typ.Equals(NewStructType(NewStructField(TypeI32), NewStructField(TypeRef))))
	require.False(t, typ.Equals(NewStructType(NewStructField(TypeI32))))
	require.False(t, typ.Equals(NewStructType(NewStructField(TypeI32), NewStructField(TypeI64))))
	require.False(t, typ.Equals(TypeI32))
}

func TestNewStructField(t *testing.T) {
	field := NewStructField(TypeI8, FieldWithName("small"))
	require.Equal(t, StructField{Name: "small", Type: TypeI8, Kind: KindI8}, field)
}

func BenchmarkStruct_Refs(b *testing.B) {
	b.Run("no refs", func(b *testing.B) {
		s := NewStruct(NewStructType(NewStructField(TypeI32)), BoxI32(1))
		require.Empty(b, s.Refs(nil))

		var refs []Ref
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			refs = s.Refs(nil)
		}
		b.StopTimer()
		require.Empty(b, refs)
	})

	b.Run("child refs", func(b *testing.B) {
		s := NewStruct(NewStructType(NewStructField(TypeRef)), BoxRef(1))
		require.Equal(b, []Ref{1}, s.Refs(nil))

		refs := make([]Ref, 0, 1)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			refs = s.Refs(refs[:0])
		}
		b.StopTimer()
		require.Equal(b, []Ref{1}, refs)
	})
}
