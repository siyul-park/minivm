// Package testenv reports what the machine running a test can do, so a test
// that needs a capability skips itself the same way everywhere.
package testenv

import (
	"runtime"
	"slices"
	"testing"

	"github.com/siyul-park/minivm/internal/jit/host"
)

// MustHaveJIT skips the test unless the host has a JIT backend, as host.New
// reports. Use it for a test that runs guest programs through that backend.
func MustHaveJIT(t testing.TB) {
	t.Helper()
	if _, ok := host.New(); !ok {
		t.Skip("no JIT backend for " + runtime.GOARCH)
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
