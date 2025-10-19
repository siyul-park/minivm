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
