//go:build arm64

package arm64

// invoke calls the compiled native block at addr with ctx passed in X0.
func invoke(addr uintptr, ctx uintptr)
