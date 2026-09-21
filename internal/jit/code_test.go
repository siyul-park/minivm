package jit_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/internal/jit"
)

// ret is a 4-byte ARM64 RET instruction: valid to link on darwin or linux
// but never run, so tests never branch to it.
func ret() []byte {
	return []byte{0xc0, 0x03, 0x5f, 0xd6}
}

func TestNewCode(t *testing.T) {
	t.Run("links code and keeps its identity", func(t *testing.T) {
		exits := []jit.Exit{{Kind: jit.ExitDeopt}}
		c, err := jit.NewCode(3, jit.Baseline, ret(), exits)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, c.Free()) })

		require.NotZero(t, c.Entry())
		require.Equal(t, 3, c.Address)
		require.Equal(t, jit.Baseline, c.Tier)
		require.Equal(t, exits, c.Exits)
	})

	t.Run("rejects empty code", func(t *testing.T) {
		_, err := jit.NewCode(0, jit.Baseline, nil, nil)
		require.Error(t, err)
	})
}

func TestCode_Free(t *testing.T) {
	c, err := jit.NewCode(0, jit.Baseline, ret(), nil)
	require.NoError(t, err)

	require.NoError(t, c.Free())
}

func TestTier_String(t *testing.T) {
	tests := []struct {
		tier jit.Tier
		want string
	}{
		{jit.Baseline, "baseline"},
		{jit.Optimized, "optimized"},
		{jit.Tier(0), "none"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			require.Equal(t, tt.want, tt.tier.String())
		})
	}
}
