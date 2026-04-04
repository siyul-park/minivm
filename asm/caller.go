package asm

import (
	"errors"
)

type Caller interface {
	Params() []RegType
	Returns() []RegType
	Call(args []uint64) ([]uint64, error)
}

var ErrInvalidArgs = errors.New("asm: invalid arguments")
