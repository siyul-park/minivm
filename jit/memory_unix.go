//go:build unix || darwin || linux

package jit

import (
	"syscall"
)

func allocExecutable(size int) ([]byte, error) {
	data, err := syscall.Mmap(
		-1,
		0,
		size,
		syscall.PROT_READ|syscall.PROT_WRITE|syscall.PROT_EXEC,
		syscall.MAP_PRIVATE|syscall.MAP_ANON,
	)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func freeExecutable(data []byte) error {
	return syscall.Munmap(data)
}
