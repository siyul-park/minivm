package asm

import "errors"

type Caller interface {
	Params() []RegType
	Returns() []RegType
	Call(params []uint64, reserved *[]uint64) ([]uint64, error)
}

var ErrInvalidArgs = errors.New("invalid arguments")
