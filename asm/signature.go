package asm

type Signature struct {
	Params          []RegType
	Returns         []RegType
	ReservedParams  int // number of leading Params slots that carry metadata
	ReservedReturns int // number of leading Returns slots that carry metadata
}
