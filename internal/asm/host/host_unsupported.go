//go:build !arm64

package host

import "github.com/siyul-park/minivm/internal/asm/arm64"

// Arch is a stand-in where no JIT backend exists: jit/host.New reports false,
// so no code is linked with it.
type Arch = arm64.Arch
