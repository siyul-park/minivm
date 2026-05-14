package asm

import "errors"

type Caller interface {
	Params(idx int) []PReg
	Returns(idx int) []PReg
	Call(params []Value, reserved *[]uint64) ([]Value, error)
}

var ErrInvalidArgs = errors.New("invalid arguments")
