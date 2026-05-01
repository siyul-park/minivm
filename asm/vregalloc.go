package asm

type VRegAlloc struct {
	next int32
}

func NewVRegAlloc() *VRegAlloc {
	return &VRegAlloc{}
}

func (a *VRegAlloc) Alloc(typ RegType, w RegWidth) VReg {
	r := NewVReg(a.next, typ, w)
	a.next++
	return r
}

func (a *VRegAlloc) Reset() {
	a.next = 0
}
