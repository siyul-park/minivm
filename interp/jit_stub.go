//go:build !arm64

package interp

import "github.com/siyul-park/minivm/internal/jit"

const nativeBackend = false

// newCompiler returns (nil, nil) on architectures without a native backend.
// A nil compiler is the interpreter's signal that JIT is unavailable, so
// serve reports the build rejected rather than failed.
func newCompiler() (*jit.Compiler, error) { return nil, nil }
