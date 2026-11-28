//go:build !darwin && !linux

package testenv

import "testing"

// Guarded skips the test: this platform has no page protection to place a
// guard with.
func Guarded(t testing.TB, n int) []byte {
	t.Helper()
	t.Skip("no guard pages on this platform")
	return nil
}
