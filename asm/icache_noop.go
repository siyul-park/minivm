//go:build !darwin || !arm64 || !cgo

package asm

func (m Memory) flushICache() {}
