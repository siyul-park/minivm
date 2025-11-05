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
}

func AllocateExecutable(size int) (*ExecutableMemory, error) {
	if size <= 0 {
		return nil, fmt.Errorf("invalid size: %d", size)
	}

	data, err := allocExecutable(size)
	if err != nil {
		return nil, err
	}

	return &ExecutableMemory{
		Data: data,
	}, nil
}

func (m *ExecutableMemory) Execute() (uint64, error) {
	if len(m.Data) == 0 {
		return 0, errors.New("empty executable memory")
	}
	ptr := uintptr(unsafe.Pointer(&m.Data[0]))
	return callMachineCode(ptr)
}

func (m *ExecutableMemory) Free() error {
	if len(m.Data) == 0 {
		return nil
	}
	err := freeExecutable(m.Data)
	m.Data = nil
	return err
}

func init() {
	if runtime.GOARCH != "amd64" {
		panic("JIT compilation is only supported on amd64 architecture")
	}
}
