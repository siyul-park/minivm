package asm

import (
	"errors"
	"unsafe"
)

// Callable is a fully linked, directly invokable entry into the executable
// buffer. Implementations are produced by ABI.NewCallable and returned from
// Link. Addr exposes the raw entry address so callers can emit direct
// branches without going back through Go.
type Callable interface {
	Call(args []Value, scratch []uint64) (returns []Value, err error)
	Addr() unsafe.Pointer
}

var ErrInvalidArgs = errors.New("invalid arguments")
