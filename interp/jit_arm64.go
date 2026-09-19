//go:build arm64

package interp

import (
	"github.com/siyul-park/minivm/internal/jit"
	jitarm64 "github.com/siyul-park/minivm/internal/jit/arm64"
)

const nativeBackend = true

func newCompiler() (*jit.Compiler, error) {
	return jit.New(jitarm64.New())
}
