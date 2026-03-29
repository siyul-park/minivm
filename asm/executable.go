package asm

import "unsafe"

type Executable interface {
	Func() unsafe.Pointer
}
