package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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

func TestStruct_Data(t *testing.T) {
	t.Run("small struct uses data slice", func(t *testing.T) {
		s := NewStruct(NewStructType(
			NewStructField(TypeI32),
			NewStructField(TypeI32),
			NewStructField(TypeI32),
			NewStructField(TypeI32),
		))
		s.SetField(3, BoxI32(4))
		require.Len(t, s.Data, 4)
		require.Equal(t, int32(4), s.Field(3).I32())
	})

	t.Run("large struct uses data slice", func(t *testing.T) {
		s := NewStruct(NewStructType(
			NewStructField(TypeI32),
			NewStructField(TypeI32),
			NewStructField(TypeI32),
			NewStructField(TypeI32),
			NewStructField(TypeI32),
		))
		s.SetField(4, BoxI32(5))
		require.Len(t, s.Data, 5)
		require.Equal(t, int32(5), s.Field(4).I32())
	})
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

		require.Empty(t, s.Refs())
		var refs []Ref
		allocs := testing.AllocsPerRun(100, func() {
			refs = s.Refs()
		})
		require.Empty(t, refs)
		require.Zero(t, allocs)
	})

	t.Run("reference fields", func(t *testing.T) {
		s := NewStruct(
			NewStructType(NewStructField(TypeRef), NewStructField(TypeI32), NewStructField(TypeRef)),
			BoxRef(1), BoxI32(2), BoxRef(3),
		)

		require.Equal(t, []Ref{1, 3}, s.Refs())
	})
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

func BenchmarkStruct_Refs(b *testing.B) {
	b.Run("no refs", func(b *testing.B) {
		s := NewStruct(NewStructType(NewStructField(TypeI32)), BoxI32(1))

		var refs []Ref
		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			refs = s.Refs()
		}
		b.StopTimer()
		require.Empty(b, refs)
	})

	b.Run("child refs", func(b *testing.B) {
		s := NewStruct(NewStructType(NewStructField(TypeRef)), BoxRef(1))

		var refs []Ref
		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			refs = s.Refs()
		}
		b.StopTimer()
		require.Len(b, refs, 1)
	})
}
