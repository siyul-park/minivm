package jit_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/types"
)

// ret is a 4-byte ARM64 RET instruction: valid to link on darwin or linux
// but never run, so tests never branch to it.
func ret() []byte {
	return []byte{0xc0, 0x03, 0x5f, 0xd6}
}

func TestNewCode(t *testing.T) {
	t.Run("links code and keeps its metadata", func(t *testing.T) {
		registers := []types.Kind{types.KindI32}
		arguments := []types.Kind{types.KindRef}
		exits := []jit.Exit{{Kind: jit.ExitDeopt}}
		c, err := jit.NewCode(3, 0, false, jit.Baseline, 1, registers, arguments, ret(), exits, 0)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, c.Free()) })

		require.NotZero(t, c.Native())
		require.Equal(t, c.Native(), c.Entry())
		require.Equal(t, 3, c.Address)
		require.Zero(t, c.IP)
		require.False(t, c.OSR)
		require.Equal(t, jit.Baseline, c.Tier)
		require.Equal(t, 1, c.Results)
		require.Equal(t, registers, c.Registers)
		require.Equal(t, arguments, c.Arguments)
		require.Equal(t, exits, c.Exits)

		registers[0] = types.KindF64
		arguments[0] = types.KindI64
		exits[0].Kind = jit.ExitBridge
		require.Equal(t, []types.Kind{types.KindI32}, c.Registers)
		require.Equal(t, []types.Kind{types.KindRef}, c.Arguments)
		require.Equal(t, []jit.Exit{{Kind: jit.ExitDeopt}}, c.Exits)
	})

	t.Run("places the entry stub at a byte offset from the body", func(t *testing.T) {
		code := append(ret(), ret()...)
		c, err := jit.NewCode(3, 0, false, jit.Baseline, 0, nil, nil, code, nil, 4)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, c.Free()) })

		require.Equal(t, c.Native()+4, c.Entry())
	})

	t.Run("keeps an OSR unit's entry IP", func(t *testing.T) {
		c, err := jit.NewCode(3, 12, true, jit.Optimized, 0, nil, nil, ret(), nil, 0)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, c.Free()) })

		require.Equal(t, 12, c.IP)
		require.True(t, c.OSR)
	})

	t.Run("keeps OSR true at IP 0: OSR is its own field, never inferred from IP", func(t *testing.T) {
		c, err := jit.NewCode(3, 0, true, jit.Baseline, 0, nil, nil, ret(), nil, 0)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, c.Free()) })

		require.Zero(t, c.IP)
		require.True(t, c.OSR)
	})

	t.Run("rejects empty code", func(t *testing.T) {
		_, err := jit.NewCode(0, 0, false, jit.Baseline, 0, nil, nil, nil, nil, 0)
		require.Error(t, err)
	})

	t.Run("rejects an entry stub offset outside the code", func(t *testing.T) {
		_, err := jit.NewCode(0, 0, false, jit.Baseline, 0, nil, nil, ret(), nil, len(ret())+1)
		require.Error(t, err)
	})
}

func TestCode_Free(t *testing.T) {
	c, err := jit.NewCode(0, 0, false, jit.Baseline, 0, nil, nil, ret(), nil, 0)
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
