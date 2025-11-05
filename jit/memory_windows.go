//go:build windows

package jit

import (
	"syscall"
)

const (
	MEM_COMMIT             = 0x1000
	MEM_RESERVE            = 0x2000
	MEM_RELEASE            = 0x8000
	PAGE_EXECUTE_READWRITE = 0x40
)

var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	virtualAlloc          = kernel32.NewProc("VirtualAlloc")
	virtualFree           = kernel32.NewProc("VirtualFree")
	flushInstructionCache = kernel32.NewProc("FlushInstructionCache")
)

func allocExecutable(size int) (uintptr, error) {
	r1, _, err := virtualAlloc.Call(
		0,
		uintptr(size),
		MEM_COMMIT|MEM_RESERVE,
		PAGE_EXECUTE_READWRITE,
	)
	if r1 == 0 {
		return 0, err
	}

	currentProcess := ^uintptr(0)
	flushInstructionCache.Call(currentProcess, r1, uintptr(size))

	return r1, nil
}

func freeExecutable(ptr uintptr, size int) error {
	r1, _, err := virtualFree.Call(
		ptr,
		0,
		MEM_RELEASE,
	)
	if r1 == 0 {
		return err
	}
	return nil
}
