package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStruct_FieldByName(t *testing.T) {
	s := NewStruct(NewStructType(NewStructField(TypeI32, FieldWithName("foo"))))
	val := s.FieldByName("foo")
	require.Equal(t, int32(0), val.I32())
}

func TestStruct_Field(t *testing.T) {
	s := NewStruct(NewStructType(NewStructField(TypeI32)))
	val := s.Field(0)
	require.Equal(t, int32(0), val.I32())
}

func TestStruct_SetField(t *testing.T) {
	s := NewStruct(NewStructType(NewStructField(TypeI32)))
	s.SetField(0, BoxI32(1))
	require.Equal(t, int32(1), s.Field(0).I32())
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
	s := NewStruct(NewStructType())
	require.Equal(t, "struct {}{}", s.String())
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

func BenchmarkStruct_Refs(b *testing.B) {
	b.Run("no refs", func(b *testing.B) {
		s := NewStruct(NewStructType(NewStructField(TypeI32)), BoxI32(1))

		var refs []Ref
		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			refs = s.Refs()
		}
		if len(refs) != 0 {
			b.Fatal("unexpected refs")
		}
	})

	b.Run("child refs", func(b *testing.B) {
		s := NewStruct(NewStructType(NewStructField(TypeRef)), BoxRef(1))

		var refs []Ref
		b.ReportAllocs()
		b.ResetTimer()
		for n := 0; n < b.N; n++ {
			refs = s.Refs()
		}
		if len(refs) != 1 {
			b.Fatal("missing refs")
		}
	})
}
