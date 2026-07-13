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

	i := New(program.New([]instr.Instruction{
		instr.New(instr.NOP),
	}), WithCache(cache))
	defer i.Close()

	require.Same(t, cache, i.cache)
}

func TestCache_Close(t *testing.T) {
	cache := NewCache(program.New(nil))
	i := New(program.New(nil), WithCache(cache))

	require.NoError(t, cache.Close())
	require.NoError(t, cache.Close())
	require.NoError(t, i.Close())
}
