package asm

import "errors"

type Caller interface {
	Call(params []Value, scratch *[]uint64) ([]Value, error)
}

var ErrInvalidArgs = errors.New("invalid arguments")
