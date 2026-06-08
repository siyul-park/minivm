//go:build !arm64

package interp

// newJITCompiler returns (nil, nil) on architectures without a native
// backend. A nil compiler is the interpreter's signal that JIT is
// unavailable, so callers gate on i.compiler == nil rather than an error.
// No lowerer is wired because jitCompiler is never constructed here.
func newJITCompiler(_ int) (*jitCompiler, error) { return nil, nil }
