package asm

type VRegAlloc struct {
	nextInt   uint8
	nextFloat uint8
}

func NewVRegAlloc() *VRegAlloc {
	return &VRegAlloc{}
}

func (a *VRegAlloc) Alloc(typ RegType) Register {
	if typ == RegTypeFloat {
		r := NewVReg(a.nextFloat, RegTypeFloat)
		a.nextFloat++
		return r
	}
	r := NewVReg(a.nextInt, RegTypeInt)
	a.nextInt++
	return r
}

func (a *VRegAlloc) Reset() {
	a.nextInt = 0
	a.nextFloat = 0
}
