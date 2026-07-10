//go:build !arm64

package arm64

import "unsafe"

func invoke(addr uintptr, ctx unsafe.Pointer) {
	panic("not implemented")
}
