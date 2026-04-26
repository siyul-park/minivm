package asm

import "errors"

type ABI interface {
	MaxParams() int
	MaxReturns() int
	NewCaller(sig *Signature, chunk *Chunk) (Caller, error)
}

var (
	ErrTooManyParams  = errors.New("too many params")
	ErrTooManyReturns = errors.New("too many returns")
)
