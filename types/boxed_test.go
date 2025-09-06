package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBoxed_Interface(t *testing.T) {
	tests := []struct {
		value    Boxed
		expected any
	}{
		{
			value:    NewI32(42),
			expected: int32(42),
		},
		{
			value:    NewI64(42),
			expected: int64(42),
		},
		{
			value:    NewF32(3.14),
			expected: float32(3.14),
		},
		{
			value:    NewF64(3.14),
			expected: 3.14,
		},
		{
			value:    NewBool(true),
			expected: true,
		},
		{
			value:    NewBool(false),
			expected: false,
		},
		{
			value:    NewNull(),
			expected: nil,
		},
		{
			value:    NewRef(0x12345678),
			expected: uintptr(0x12345678),
		},
	}

	for _, tt := range tests {
		t.Run(tt.value.Kind().String(), func(t *testing.T) {
			require.Equal(t, tt.expected, tt.value.Interface())
		})
	}
}
