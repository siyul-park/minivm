//go:build !darwin || !arm64 || !cgo

package asm

func (m memory) flushICache() {}
