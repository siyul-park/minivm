package types

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrimitive_Kind(t *testing.T) {
	tests := []struct {
		val  Value
		kind Kind
	}{
		{
			val:  I32(0),
			kind: KindI32,
		},
		{
			val:  I64(0),
			kind: KindI64,
		},
		{
			val:  F32(0),
			kind: KindF32,
		},
		{
			val:  F64(0),
			kind: KindF64,
		},
		{
			val:  Ref(0),
			kind: KindRef,
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.kind, tt.val.Kind())
		})
	}
}

func TestPrimitive_Type(t *testing.T) {
	tests := []struct {
		val Value
		typ Type
	}{
		{
			val: I32(0),
			typ: TypeI32,
		},
		{
			val: I64(0),
			typ: TypeI64,
		},
		{
			val: F32(0),
			typ: TypeF32,
		},
		{
			val: F64(0),
			typ: TypeF64,
		},
		{
			val: Ref(0),
			typ: TypeRef,
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.typ, tt.val.Type())
		})
	}
}

func TestPrimitive_String(t *testing.T) {
	tests := []struct {
		val Value
		str string
	}{
		{
			val: I32(0),
			str: "0",
		},
		{
			val: I64(0),
			str: "0",
		},
		{
			val: F32(0),
			str: "0.000000",
		},
		{
			val: F64(0),
			str: "0.000000",
		},
		{
			val: Ref(0),
			str: "0",
		},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.str, tt.val.String())
		})
	}
}
