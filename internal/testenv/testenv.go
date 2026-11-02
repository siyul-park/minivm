// Package testenv reports what the machine running a test can do, so a test
// that needs a capability skips itself the same way everywhere.
package testenv

import (
	"runtime"
	"slices"
	"testing"

	"github.com/siyul-park/minivm/internal/asm"
)

// MustHaveNative skips the test unless native code can run here, as asm.Native
// reports. Use it for a test that runs guest programs through whatever native
// backend the architecture has.
func MustHaveNative(t testing.TB) {
	t.Helper()
	if !asm.Native {
		t.Skip("native execution is unsupported on " + runtime.GOARCH)
	}
}

// MustHaveArch skips the test unless the architecture running it is one of
// archs, as runtime.GOARCH names them. Use it for a test that builds machine
// code of one architecture and runs it.
func MustHaveArch(t testing.TB, archs ...string) {
	t.Helper()
	if !slices.Contains(archs, runtime.GOARCH) {
		t.Skipf("needs one of %v, not %s", archs, runtime.GOARCH)
	}
}
