//go:build !arm64

package asm

import "runtime"

// Enter cannot run on this architecture: no native code can be published
// for it, so reaching here is a programmer error.
func Enter(uintptr, *Context) Trap {
	panic("asm: native execution is unsupported on " + runtime.GOARCH)
}

// Resume cannot run on this architecture; see Enter.
func Resume(*Context) Trap {
	panic("asm: native execution is unsupported on " + runtime.GOARCH)
}

func exitPC() uintptr { return 0 }
