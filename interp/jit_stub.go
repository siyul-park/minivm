//go:build !arm64

package interp

// newCompiler returns (nil, nil) on architectures without a native
// backend. A nil compiler is the interpreter's signal that JIT is
// unavailable, so callers gate on i.compiler == nil rather than an error.
// No lowerer is wired because compiler is never constructed here.
func newCompiler() (*compiler, error) { return nil, nil }
