package asm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewRegMask(t *testing.T) {
	m := NewRegMask([]uint8{0, 2, 4})
	require.True(t, m.Contains(0))
	require.False(t, m.Contains(1))
	require.True(t, m.Contains(2))
	require.True(t, m.Contains(4))
}

func TestRegMask_Set(t *testing.T) {
	var m RegMask
	m = m.Set(3)
	require.True(t, m.Contains(3))
}

func TestRegMask_Clear(t *testing.T) {
	m := NewRegMask([]uint8{1, 3})
	m = m.Clear(1)
	require.False(t, m.Contains(1))
	require.True(t, m.Contains(3))
}

func TestRegMask_Contains(t *testing.T) {
	m := NewRegMask([]uint8{5})
	require.True(t, m.Contains(5))
	require.False(t, m.Contains(6))
	require.False(t, m.Contains(64))
}

func TestRegMask_Empty(t *testing.T) {
	var m RegMask
	require.True(t, m.Empty())
	m = m.Set(0)
	require.False(t, m.Empty())
}

func TestRegMask_First(t *testing.T) {
	var m RegMask
	require.Equal(t, uint8(0xFF), m.First())
	m = NewRegMask([]uint8{2, 5})
	require.Equal(t, uint8(2), m.First())
}

func TestRegMask_PopFirst(t *testing.T) {
	m := NewRegMask([]uint8{1, 3, 5})
	id, m2 := m.PopFirst()
	require.Equal(t, uint8(1), id)
	require.False(t, m2.Contains(1))
	require.True(t, m2.Contains(3))

	var empty RegMask
	id, _ = empty.PopFirst()
	require.Equal(t, uint8(0xFF), id)
}

func TestRegMask_Count(t *testing.T) {
	var m RegMask
	require.Equal(t, 0, m.Count())
	m = NewRegMask([]uint8{0, 1, 2})
	require.Equal(t, 3, m.Count())
}

func TestRegMask_List(t *testing.T) {
	m := NewRegMask([]uint8{0, 2, 4})
	list := m.List()
	require.Equal(t, []uint8{0, 2, 4}, list)
}

func TestRegMask_SetBoundary(t *testing.T) {
	var m RegMask
	m = m.Set(63)
	require.True(t, m.Contains(63))
	// id >= 64 is a no-op
	m2 := m.Set(64)
	require.Equal(t, m, m2)
}

func TestRegMask_And(t *testing.T) {
	a := NewRegMask([]uint8{0, 1, 2})
	b := NewRegMask([]uint8{1, 2, 3})
	c := a.And(b)
	require.True(t, c.Contains(1))
	require.True(t, c.Contains(2))
	require.False(t, c.Contains(0))
	require.False(t, c.Contains(3))
}

func TestRegMask_Or(t *testing.T) {
	a := NewRegMask([]uint8{0, 1})
	b := NewRegMask([]uint8{2, 3})
	c := a.Or(b)
	for _, id := range []uint8{0, 1, 2, 3} {
		require.True(t, c.Contains(id))
	}
}

func TestRegMask_Not(t *testing.T) {
	m := NewRegMask([]uint8{0})
	n := m.Not()
	require.False(t, n.Contains(0))
	require.True(t, n.Contains(1))
}

func TestRegMask_Sub(t *testing.T) {
	a := NewRegMask([]uint8{0, 1, 2})
	b := NewRegMask([]uint8{1})
	c := a.Sub(b)
	require.True(t, c.Contains(0))
	require.False(t, c.Contains(1))
	require.True(t, c.Contains(2))
}
