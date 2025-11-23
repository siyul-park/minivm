package asm

import (
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

type Code struct {
	data []byte
	once sync.Once
}

func NewCode(b []byte) (*Code, error) {
	data, err := syscall.Mmap(
		-1, 0, len(b),
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_PRIVATE|syscall.MAP_ANON,
	)
	if err != nil {
		return nil, err
	}
	copy(data, b)

	if err := syscall.Mprotect(data, syscall.PROT_READ|syscall.PROT_EXEC); err != nil {
		_ = syscall.Munmap(data)
		return nil, err
	}

	c := &Code{data: data}
	runtime.SetFinalizer(c, func(c *Code) {
		_ = c.Close()
	})
	return c, nil
}

func (c *Code) Ptr() unsafe.Pointer {
	return unsafe.Pointer(&c.data[0])
}

func (c *Code) Close() error {
	var err error
	c.once.Do(func() {
		if c.data != nil {
			err = syscall.Munmap(c.data)
			c.data = nil
		}
	})
	return err
}
