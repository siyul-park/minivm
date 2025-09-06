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
			value:    BoxI32(42),
			expected: int32(42),
		},
		{
			value:    BoxI64(42),
			expected: int64(42),
		},
		{
			value:    BoxF32(3.14),
			expected: float32(3.14),
		},
		{
			value:    BoxF64(3.14),
			expected: 3.14,
		},
		{
			value:    BoxBool(true),
			expected: true,
		},
		{
			value:    BoxBool(false),
			expected: false,
		},
		{
			value:    BoxNull(),
			expected: nil,
		},
		{
			value:    BoxRef(0x12345678),
			expected: 0x12345678,
		},
	}

	for _, tt := range tests {
		t.Run(tt.value.Kind().String(), func(t *testing.T) {
			require.Equal(t, tt.expected, tt.value.Interface())
		})
	}
}
