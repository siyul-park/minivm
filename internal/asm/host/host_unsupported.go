//go:build !arm64

package host

import "github.com/siyul-park/minivm/internal/asm/arm64"

// Arch is a stand-in where no JIT backend exists: its Enter and Resume panic,
// and jit/host.New reports false so nothing reaches them.
type Arch = arm64.Arch
