package interp

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/stretchr/testify/require"
)

func TestNewCache(t *testing.T) {
	cache := NewCache(program.New([]instr.Instruction{
		instr.New(instr.NOP),
	}))
	defer cache.Close()

	i, _ := New(program.New([]instr.Instruction{
		instr.New(instr.NOP),
	}), WithCache(cache))
	defer i.Close()

	require.Same(t, cache, i.cache)
}

func TestCache_Due(t *testing.T) {
	cache := NewCache(program.New([]instr.Instruction{
		instr.New(instr.NOP),
	}))
	defer cache.Close()

	require.False(t, cache.due(0, 2))
	require.True(t, cache.due(0, 2))
	require.False(t, cache.due(0, 2))
}

func TestCache_Rearm(t *testing.T) {
	cache := NewCache(program.New([]instr.Instruction{
		instr.New(instr.NOP),
	}))
	defer cache.Close()

	require.True(t, cache.due(0, 1))
	cache.ready(0)
	require.True(t, cache.rearm(0))
	require.False(t, cache.due(0, 1))
	cache.ready(0)
	require.True(t, cache.rearm(0))
	require.False(t, cache.rearm(2))
}

func TestCache_Close(t *testing.T) {
	cache := NewCache(program.New(nil))
	i, _ := New(program.New(nil), WithCache(cache))

	require.NoError(t, cache.Close())
	require.NoError(t, cache.Close())
	require.NoError(t, i.Close())
}
