//go:build arm64

package arm64

import "unsafe"

// invoke calls the compiled native block at addr with ctx passed in X0.
//
//go:noescape
func invoke(addr uintptr, ctx unsafe.Pointer)
