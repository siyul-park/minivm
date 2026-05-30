package asm

import "errors"

var (
	ErrInvalidArgs          = errors.New("invalid arguments")
	ErrInvalidOperand       = errors.New("invalid operand")
	ErrInvalidSize          = errors.New("invalid size")
	ErrNotImplemented       = errors.New("not implemented")
	ErrUnresolvedLabel      = errors.New("unresolved label")
	ErrConflictingPin       = errors.New("conflicting pin")
	ErrNoRegistersAvailable = errors.New("no registers available")
	ErrTooManyArgs          = errors.New("too many args")
	ErrTooManyReturns       = errors.New("too many returns")
	ErrMmapFailed           = errors.New("mmap failed")
	ErrMprotectFailed       = errors.New("mprotect failed")
	ErrMunmapFailed         = errors.New("munmap failed")
	ErrBufferFull           = errors.New("buffer full")
)
