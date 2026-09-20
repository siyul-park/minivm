//go:build !darwin || !arm64 || !cgo

package asm

func flushICache(m memory) {}
