package host

import (
	"github.com/siyul-park/minivm/internal/jit/arm64"
	"github.com/siyul-park/minivm/internal/jit/compile"
)

// New returns a Machine for the architecture the program runs on.
func New() (compile.Machine, bool) {
	return arm64.New(), true
}
