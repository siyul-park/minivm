package arm64

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncoder_Encode(t *testing.T) {
	encoder := NewEncoder()

	t.Run("register offset load store scales slot index", func(t *testing.T) {
		got, err := encoder.Encode(LDRR(X3, X4, X5))
		require.NoError(t, err)
		require.Equal(t, []byte{0x83, 0x78, 0x65, 0xF8}, got)

		got, err = encoder.Encode(STRR(X3, X4, X5))
		require.NoError(t, err)
		require.Equal(t, []byte{0x83, 0x78, 0x25, 0xF8}, got)
	})
}
