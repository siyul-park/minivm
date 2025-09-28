package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArray_Set(t *testing.T) {
	tests := []struct {
		typ   *ArrayType
		value Boxed
	}{
		{NewArrayType(TypeI32), BoxI32(42)},
		{NewArrayType(TypeI64), BoxI64(42)},
		{NewArrayType(TypeF32), BoxF32(42)},
		{NewArrayType(TypeF64), BoxF64(42)},
		{NewArrayType(TypeRef), BoxRef(42)},
	}

	for _, tt := range tests {
		t.Run(tt.typ.String(), func(t *testing.T) {
			a := NewArray(tt.typ, 3)

			err := a.Set(0, tt.value)
			require.NoError(t, err)

			actual, err := a.Get(0)
			require.NoError(t, err)
			require.Equal(t, tt.value, actual)
		})
	}
}
