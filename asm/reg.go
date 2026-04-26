package asm

import "fmt"

type RegType uint8

const (
	RegTypeInt RegType = iota
	RegTypeFloat
)

const InvalidRegID uint8 = 0xFF

type Register struct {
	id      uint8
	typ     RegType
	virtual bool
}

func NewReg(id uint8, typ RegType) Register {
	return Register{id: id, typ: typ}
}

func NewVReg(id uint8, typ RegType) Register {
	return Register{id: id, typ: typ, virtual: true}
}

func (r Register) ID() uint8       { return r.id }
func (r Register) Type() RegType   { return r.typ }
func (r Register) IsVirtual() bool { return r.virtual }
func (r Register) Valid() bool     { return r.id != InvalidRegID }

func (r Register) String() string {
	if !r.Valid() {
		return "invalid"
	}
	prefix := "r"
	if r.typ == RegTypeFloat {
		prefix = "f"
	}
	if r.virtual {
		prefix = "v" + prefix
	}
	return fmt.Sprintf("%s%d", prefix, r.id)
}
