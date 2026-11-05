//go:build !arm64

package asm

import "runtime"

// registers is empty: no exit protocol runs on this architecture.
type registers struct{}

func (*registers) cell(PReg) *uint64 {
	panic("asm: native execution is unsupported on " + runtime.GOARCH)
}
