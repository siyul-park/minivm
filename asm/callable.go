package asm

import "errors"

// Callable is a fully linked, directly invokable entry into the executable
// buffer. Implementations are produced by ABI.NewCallable and returned from
// Link.
type Callable interface {
	Call(argv []uint64) error
}

var ErrInvalidArgs = errors.New("invalid arguments")
