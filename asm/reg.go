package asm

import "fmt"

type Reg interface {
	Type() RegType
	Width() RegWidth
	String() string
}

type PReg struct {
	id    uint8
	typ   RegType
	width RegWidth
}

type VReg struct {
	id    int32
	typ   RegType
	width RegWidth
}

const InvalidRegID = 255

type RegType uint8

const (
	RegTypeInt RegType = iota
	RegTypeFloat
)

type RegWidth uint8

const (
	WidthUndefined RegWidth = 0
	Width32        RegWidth = 32
	Width64        RegWidth = 64
)

func NewPReg(id uint8, typ RegType, w RegWidth) PReg {
	return PReg{id: id, typ: typ, width: w}
}

func NewVReg(id int32, typ RegType, w RegWidth) VReg {
	return VReg{id: id, typ: typ, width: w}
}

func (r PReg) ID() uint8       { return r.id }
func (r PReg) Type() RegType   { return r.typ }
func (r PReg) Width() RegWidth { return r.width }

func (r PReg) String() string {
	if r.typ == RegTypeFloat {
		if r.width == Width32 {
			return fmt.Sprintf("s%d", r.id)
		}
		return fmt.Sprintf("d%d", r.id)
	}
	if r.width == Width32 {
		return fmt.Sprintf("w%d", r.id)
	}
	return fmt.Sprintf("x%d", r.id)
}

func (r VReg) ID() int32       { return r.id }
func (r VReg) Type() RegType   { return r.typ }
func (r VReg) Width() RegWidth { return r.width }

func (r VReg) String() string {
	prefix := "vr"
	if r.typ == RegTypeFloat {
		prefix = "vf"
	}
	return fmt.Sprintf("%s%d", prefix, r.id)
}
