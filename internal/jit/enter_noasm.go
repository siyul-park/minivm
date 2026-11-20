//go:build !arm64

package jit

import "runtime"

// enter cannot run on this host: no native code can be published for it, so
// reaching here is a programmer error.
func enter(uintptr, *State) {
	panic("jit: native execution is unsupported on " + runtime.GOARCH)
}

// resume cannot run on this host; see enter.
func resume(*State) {
	panic("jit: native execution is unsupported on " + runtime.GOARCH)
}

// exitPC is no address: nothing native runs on this host to call one.
func exitPC() uintptr {
	return 0
}
