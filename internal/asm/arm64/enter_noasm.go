//go:build !arm64

package arm64

import (
	"runtime"

	"github.com/siyul-park/minivm/internal/asm"
)

// Enter cannot run on this host: no native code can be published for it, so
// reaching here is a programmer error.
func (Arch) Enter(uintptr, *asm.State) bool {
	panic("arm64: native execution is unsupported on " + runtime.GOARCH)
}

// Resume cannot run on this host; see Enter.
func (Arch) Resume(*asm.State) bool {
	panic("arm64: native execution is unsupported on " + runtime.GOARCH)
}

// Stub is no address: nothing native runs on this host to call one.
func (Arch) Stub() uintptr {
	return 0
}
