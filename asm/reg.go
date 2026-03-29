package asm

type Register struct {
	id   uint8
	typ  RegType
	size RegSize
}

type RegType uint8

const (
	TypeInt RegType = iota
	TypeFloat
)

type RegSize uint8

const (
	Size32 RegSize = 32
	Size64 RegSize = 64
)

func NewRegister(id uint8, typ RegType, size RegSize) Register {
	return Register{id: id, typ: typ, size: size}
}

func (r Register) ID() uint8     { return r.id }
func (r Register) Type() RegType { return r.typ }
func (r Register) Size() RegSize { return r.size }
