//go:build !arm64

package host

import "github.com/siyul-park/minivm/internal/jit/compile"

// New reports false: this architecture has no JIT backend.
func New() (compile.Machine, bool) {
	return nil, false
}
