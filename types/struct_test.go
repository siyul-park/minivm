package types_test

import (
	"testing"

	types "github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestFieldWithName(t *testing.T) {
	field := types.NewStructField(types.TypeI32, types.FieldWithName("value"))
	require.Equal(t, "value", field.Name)
}

func TestNewStruct(t *testing.T) {
	t.Run("initial fields", func(t *testing.T) {
		typ := types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeRef))
		s := types.NewStruct(typ, types.BoxI32(1), types.BoxRef(2))
		require.Same(t, typ, s.Typ)
		require.Equal(t, types.BoxI32(1), s.Field(0))
		require.Equal(t, types.BoxRef(2), s.Field(1))
	})

}

func TestStruct_Reset(t *testing.T) {
	t.Run("small", func(t *testing.T) {
		typ := types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeI32))
		s := types.NewStruct(typ, types.BoxI32(1), types.BoxI32(2))

		s.Reset(typ)
		require.Same(t, typ, s.Typ)
		require.Equal(t, types.BoxI32(0), s.Field(0))
		require.Equal(t, types.BoxI32(0), s.Field(1))
	})

	t.Run("large", func(t *testing.T) {
		typ := types.NewStructType(
			types.NewStructField(types.TypeI32), types.NewStructField(types.TypeI32),
			types.NewStructField(types.TypeI32), types.NewStructField(types.TypeI32),
			types.NewStructField(types.TypeI32),
		)
		s := types.NewStruct(typ, types.BoxI32(1), types.BoxI32(2), types.BoxI32(3), types.BoxI32(4), types.BoxI32(5))

		s.Reset(typ)
		for i := range typ.Fields {
			require.Equal(t, types.BoxI32(0), s.Field(i))
		}
	})

	t.Run("reference", func(t *testing.T) {
		typ := types.NewStructType(types.NewStructField(types.TypeRef))
		s := types.NewStruct(typ, types.BoxRef(42))

		s.Reset(typ)
		require.Zero(t, s.Raw(0))
	})
}

func TestStruct_FieldByName(t *testing.T) {
	s := types.NewStruct(types.NewStructType(types.NewStructField(types.TypeI32, types.FieldWithName("foo"))))

	require.Equal(t, int32(0), s.FieldByName("foo").I32())
	require.Zero(t, s.FieldByName("missing"))
}

func TestStruct_Field(t *testing.T) {
	s := types.NewStruct(types.NewStructType(types.NewStructField(types.TypeI32)))

	require.Equal(t, int32(0), s.Field(0).I32())
	require.Zero(t, s.Field(1))
}

func TestStruct_SetField(t *testing.T) {
	s := types.NewStruct(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeI64), types.NewStructField(types.TypeF32), types.NewStructField(types.TypeF64), types.NewStructField(types.TypeRef)))

	s.SetField(0, types.BoxI32(1))
	s.SetField(1, types.BoxI64(2))
	s.SetField(2, types.BoxF32(3))
	s.SetField(3, types.BoxF64(4))
	s.SetField(4, types.BoxRef(5))
	s.SetField(5, types.BoxRef(6))

	require.Equal(t, int32(1), s.Field(0).I32())
	require.Equal(t, int64(2), s.Field(1).I64())
	require.Equal(t, float32(3), s.Field(2).F32())
	require.Equal(t, float64(4), s.Field(3).F64())
	require.Equal(t, 5, s.Field(4).Ref())
}

func TestStruct_Raw(t *testing.T) {
	s := types.NewStruct(types.NewStructType(types.NewStructField(types.TypeI64)))
	s.SetRaw(0, 42)

	require.Equal(t, uint64(42), s.Raw(0))
	require.Zero(t, s.Raw(1))
}

func TestStruct_SetRaw(t *testing.T) {
	s := types.NewStruct(types.NewStructType(types.NewStructField(types.TypeI64)))
	s.SetRaw(0, 42)
	s.SetRaw(1, 99)

	require.Equal(t, uint64(42), s.Raw(0))
}

func TestStruct_Kind(t *testing.T) {
	s := types.NewStruct(types.NewStructType())
	require.Equal(t, types.KindRef, s.Kind())
}

func TestStruct_Type(t *testing.T) {
	s := types.NewStruct(types.NewStructType())
	require.Equal(t, types.NewStructType(), s.Type())
}

func TestStruct_String(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		s := types.NewStruct(types.NewStructType())
		require.Equal(t, "struct {}{}", s.String())
	})

	t.Run("fields", func(t *testing.T) {
		s := types.NewStruct(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeRef)), types.BoxI32(1), types.BoxRef(2))
		require.Equal(t, "struct {i32; ref}{1, 2}", s.String())
	})
}

func TestStruct_Refs(t *testing.T) {
	t.Run("primitive fields", func(t *testing.T) {
		s := types.NewStruct(types.NewStructType(types.NewStructField(types.TypeI32)), types.BoxI32(1))

		require.Equal(t, []types.Ref{9}, s.Refs([]types.Ref{9}))
		var refs []types.Ref
		allocs := testing.AllocsPerRun(100, func() {
			refs = s.Refs(nil)
		})
		require.Empty(t, refs)
		require.Zero(t, allocs)
	})

	t.Run("reference fields", func(t *testing.T) {
		s := types.NewStruct(types.NewStructType(types.NewStructField(types.TypeRef), types.NewStructField(types.TypeI32), types.NewStructField(types.TypeRef)), types.BoxRef(1), types.BoxI32(2), types.BoxRef(3))

		require.Equal(t, []types.Ref{9, 1, 3}, s.Refs([]types.Ref{9}))
	})
}

func TestNewStructType(t *testing.T) {
	fields := []types.StructField{types.NewStructField(types.TypeI32), types.NewStructField(types.TypeRef)}
	typ := types.NewStructType(fields...)
	require.Equal(t, fields, typ.Fields)
}

func TestStructType_FieldByName(t *testing.T) {
	typ := types.NewStructType(types.NewStructField(types.TypeI32, types.FieldWithName("foo")))

	field, ok := typ.FieldByName("foo")
	require.True(t, ok)
	require.Equal(t, types.TypeI32, field.Type)

	_, ok = typ.FieldByName("missing")
	require.False(t, ok)
}

func TestStructType_FieldIndex(t *testing.T) {
	typ := types.NewStructType(types.NewStructField(types.TypeI32, types.FieldWithName("foo")))

	require.Equal(t, 0, typ.FieldIndex("foo"))
	require.Equal(t, -1, typ.FieldIndex("missing"))
}

func TestStructType_Kind(t *testing.T) {
	require.Equal(t, types.KindRef, types.NewStructType().Kind())
}

func TestStructType_String(t *testing.T) {
	require.Equal(t, "struct {i32; ref}", types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeRef)).String())
}

func TestStructType_Cast(t *testing.T) {
	typ := types.NewStructType(types.NewStructField(types.TypeI32))

	require.True(t, typ.Cast(typ))
	require.True(t, typ.Cast(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeI64))))
	require.False(t, typ.Cast(types.NewStructType(types.NewStructField(types.TypeI64))))
	require.False(t, typ.Cast(types.TypeI32))
}

func TestStructType_Equals(t *testing.T) {
	typ := types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeRef))

	require.True(t, typ.Equals(typ))
	require.True(t, typ.Equals(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeRef))))
	require.False(t, typ.Equals(types.NewStructType(types.NewStructField(types.TypeI32))))
	require.False(t, typ.Equals(types.NewStructType(types.NewStructField(types.TypeI32), types.NewStructField(types.TypeI64))))
	require.False(t, typ.Equals(types.TypeI32))
}

func TestNewStructField(t *testing.T) {
	field := types.NewStructField(types.TypeI8, types.FieldWithName("small"))
	require.Equal(t, types.StructField{Name: "small", Type: types.TypeI8, Kind: types.KindI8}, field)
}

func BenchmarkStruct_Refs(b *testing.B) {
	b.Run("no refs", func(b *testing.B) {
		s := types.NewStruct(types.NewStructType(types.NewStructField(types.TypeI32)), types.BoxI32(1))
		require.Empty(b, s.Refs(nil))

		var refs []types.Ref
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			refs = s.Refs(nil)
		}
		b.StopTimer()
		require.Empty(b, refs)
	})

	b.Run("child refs", func(b *testing.B) {
		s := types.NewStruct(types.NewStructType(types.NewStructField(types.TypeRef)), types.BoxRef(1))
		require.Equal(b, []types.Ref{1}, s.Refs(nil))

		refs := make([]types.Ref, 0, 1)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			refs = s.Refs(refs[:0])
		}
		b.StopTimer()
		require.Equal(b, []types.Ref{1}, refs)
	})
}
