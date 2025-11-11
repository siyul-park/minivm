//go:build windows

package jit

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	flushInstructionCache = kernel32.NewProc("FlushInstructionCache")
)

func allocExecutable(size int) ([]byte, error) {
	addr, err := windows.VirtualAlloc(
		0,
		uintptr(size),
		windows.MEM_COMMIT|windows.MEM_RESERVE,
		windows.PAGE_EXECUTE_READWRITE,
	)
	if err != nil {
		return nil, fmt.Errorf("VirtualAlloc failed: %w", err)
	}

	currentProcess := ^uintptr(0)
	flushInstructionCache.Call(currentProcess, addr, uintptr(size))

	data := unsafe.Slice((*byte)(unsafe.Pointer(addr)), size)
	return data, nil
}

func freeExecutable(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	addr := uintptr(unsafe.Pointer(&data[0]))
	err := windows.VirtualFree(addr, 0, windows.MEM_RELEASE)
	if err != nil {
		return fmt.Errorf("VirtualFree failed: %w", err)
	}
	return nil
}
