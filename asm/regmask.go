package asm

type RegMask uint64

func NewRegMask(ids []uint8) RegMask {
	var m RegMask
	for _, id := range ids {
		m.Set(id)
	}
	return m
}

func (m *RegMask) Set(id uint8) {
	if id < 64 {
		*m |= 1 << id
	}
}

func (m *RegMask) Clear(id uint8) {
	if id < 64 {
		*m &^= 1 << id
	}
}

func (m *RegMask) Contains(id uint8) bool {
	return id < 64 && (*m&(1<<id)) != 0
}

func (m *RegMask) First() uint8 {
	if *m == 0 {
		return InvalidRegID
	}
	for i := uint8(0); i < 64; i++ {
		if (*m & (1 << i)) != 0 {
			return i
		}
	}
	return InvalidRegID
}

func (m *RegMask) Count() int {
	count := 0
	mask := uint64(*m)
	for mask != 0 {
		count++
		mask &= mask - 1
	}
	return count
}

func (m *RegMask) List() []uint8 {
	var ids []uint8
	for i := uint8(0); i < 64; i++ {
		if m.Contains(i) {
			ids = append(ids, i)
		}
	}
	return ids
}
