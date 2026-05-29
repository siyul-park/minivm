package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewRegMask(t *testing.T) {
	m := NewRegMask([]uint8{0, 2, 4})
	require.True(t, m.Contains(0))
	require.True(t, m.Contains(2))
	require.True(t, m.Contains(4))
	require.False(t, m.Contains(1))
}

func TestRegMask_Set(t *testing.T) {
	var m RegMask
	m = m.Set(3)
	require.True(t, m.Contains(3))
}

func TestRegMask_Clear(t *testing.T) {
	m := NewRegMask([]uint8{1, 2, 3})
	m = m.Clear(2)
	require.False(t, m.Contains(2))
	require.True(t, m.Contains(1))
	require.True(t, m.Contains(3))
}

func TestRegMask_Contains(t *testing.T) {
	t.Run("present", func(t *testing.T) {
		m := NewRegMask([]uint8{5})
		require.True(t, m.Contains(5))
	})
	t.Run("absent", func(t *testing.T) {
		m := NewRegMask([]uint8{5})
		require.False(t, m.Contains(6))
	})
	t.Run("out of range", func(t *testing.T) {
		m := NewRegMask([]uint8{5})
		require.False(t, m.Contains(64))
	})
}

func TestRegMask_First(t *testing.T) {
	t.Run("non-empty", func(t *testing.T) {
		m := NewRegMask([]uint8{3, 7, 1})
		require.Equal(t, uint8(1), m.First())
	})
	t.Run("empty returns 0xFF", func(t *testing.T) {
		require.Equal(t, uint8(0xFF), RegMask(0).First())
	})
}

func TestRegMask_PopFirst(t *testing.T) {
	t.Run("non-empty", func(t *testing.T) {
		m := NewRegMask([]uint8{2, 5})
		id, m2 := m.PopFirst()
		require.Equal(t, uint8(2), id)
		require.False(t, m2.Contains(2))
		require.True(t, m2.Contains(5))
	})
	t.Run("empty returns 0xFF", func(t *testing.T) {
		id, _ := RegMask(0).PopFirst()
		require.Equal(t, uint8(0xFF), id)
	})
}

func TestRegMask_Count(t *testing.T) {
	t.Run("zero", func(t *testing.T) {
		require.Equal(t, 0, RegMask(0).Count())
	})
	t.Run("three", func(t *testing.T) {
		require.Equal(t, 3, NewRegMask([]uint8{0, 1, 2}).Count())
	})
}
