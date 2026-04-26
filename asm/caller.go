package asm

import (
	"errors"
)

type Caller interface {
	Call(args []uint64) ([]uint64, error)
}

var ErrInvalidArgs = errors.New("asm: invalid arguments")
