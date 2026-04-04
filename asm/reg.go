package asm

type Register struct {
	id      uint8
	typ     RegType
	virtual bool
}

type RegType uint8

const (
	TypeInt RegType = iota
	TypeFloat
)

func NewReg(id uint8, typ RegType) Register {
	return Register{id: id, typ: typ}
}

func NewVReg(id uint8, typ RegType) Register {
	return Register{id: id, typ: typ, virtual: true}
}

func (r Register) ID() uint8     { return r.id }
func (r Register) Type() RegType { return r.typ }
func (r Register) Virtual() bool { return r.virtual }
