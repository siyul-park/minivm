//go:build darwin && arm64 && cgo

package asm

/*
#include <stddef.h>
#include <libkern/OSCacheControl.h>
*/
import "C"

import "unsafe"

func (m Memory) flushICache() {
	if len(m) == 0 {
		return
	}
	C.sys_icache_invalidate(unsafe.Pointer(&m[0]), C.size_t(len(m)))
}
