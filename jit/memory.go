package jit

import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"
)

var (
	ErrMemoryAllocation = errors.New("failed to allocate executable memory")
	ErrMemoryProtection = errors.New("failed to set memory protection")
)

type ExecutableMemory struct {
	Data []byte
	ptr  uintptr
	size int
}

func AllocateExecutable(size int) (*ExecutableMemory, error) {
	if size <= 0 {
		return nil, fmt.Errorf("invalid size: %d", size)
	}

	ptr, err := allocExecutable(size)
	if err != nil {
		return nil, err
	}

	data := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), size)

	return &ExecutableMemory{
		Data: data,
		ptr:  ptr,
		size: size,
	}, nil
}

func (m *ExecutableMemory) Execute() (uint64, error) {
	return callMachineCode(m.ptr)
}

func (m *ExecutableMemory) Free() error {
	if m.ptr == 0 {
		return nil
	}
	err := freeExecutable(m.ptr, m.size)
	m.ptr = 0
	m.Data = nil
	return err
}

func init() {
	if runtime.GOARCH != "amd64" {
		panic("JIT compilation is only supported on amd64 architecture")
	}
}
