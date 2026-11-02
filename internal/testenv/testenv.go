// Package testenv reports what the machine running a test can do, so a test
// that needs a capability skips itself the same way everywhere.
package testenv

import (
	"runtime"
	"testing"
)

// MustHaveNative skips the test unless native code can run. Native execution
// exists only on arm64: asm.Enter panics on any other architecture.
func MustHaveNative(t testing.TB) {
	t.Helper()
	if runtime.GOARCH != "arm64" {
		t.Skip("native execution needs arm64")
	}
}
