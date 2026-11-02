//go:build !arm64

package asm

import "runtime"

// Native reports whether native code can run on this architecture: Enter and
// Resume execute it where Native is true and panic here.
const Native = false

// Enter cannot run on this architecture: no native code can be published
// for it, so reaching here is a programmer error.
func Enter(uintptr, *State) bool {
	panic("asm: native execution is unsupported on " + runtime.GOARCH)
}

// Resume cannot run on this architecture; see Enter.
func Resume(*State) bool {
	panic("asm: native execution is unsupported on " + runtime.GOARCH)
}

// exitPC is no stub: nothing native runs here to call one.
func exitPC() uintptr {
	return 0
}
