package asm

import "math/bits"

type RegMask uint64

func NewRegMask(ids []uint8) RegMask {
	var m RegMask
	for _, id := range ids {
		m |= 1 << id
	}
	return m
}

func (m RegMask) Set(id uint8) RegMask {
	if id < 64 {
		m |= 1 << id
	}
	return m
}

func (m RegMask) Clear(id uint8) RegMask {
	if id < 64 {
		m &^= 1 << id
	}
	return m
}

func (m RegMask) Contains(id uint8) bool {
	return id < 64 && (m&(1<<id)) != 0
}

func (m RegMask) First() uint8 {
	if m == 0 {
		return 0xFF
	}
	return uint8(bits.TrailingZeros64(uint64(m)))
}

func (m RegMask) PopFirst() (uint8, RegMask) {
	if m == 0 {
		return 0xFF, m
	}
	i := uint8(bits.TrailingZeros64(uint64(m)))
	m &^= 1 << i
	return i, m
}

func (m RegMask) Count() int {
	return bits.OnesCount64(uint64(m))
}
