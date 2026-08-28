package interp

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"testing"

	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/journal"
	"github.com/stretchr/testify/require"
)

// TestARM64_StackReserve ties nativeFrameLimit (interp's private native
// call-depth cap) to the arm64 invoke trampoline's hard-coded stack reserve
// and total frame size in abi_arm64.s. arm64.StackReserve and arm64.FrameSize
// own the byte arithmetic — one 64-bit spill slot per asm.MaxSpillSlots plus
// one journal frame record per native call-depth level, plus the
// callee-saved save area — so nativeFrameLimit changing without
// abi_arm64.s keeping pace fails this test instead of the mismatch
// surfacing as a corrupted native stack at runtime. arm64.TestFrameSize
// (internal/asm/arm64/stack_test.go) carries the complementary half of the
// invariant: that abi_arm64.s's own two literals are consistent with each
// other independent of nativeFrameLimit, a check this package cannot make
// since it owns neither literal. See docs/jit-internals.md for the full
// explanation.
func TestARM64_StackReserve(t *testing.T) {
	reserve := arm64.StackReserve(1<<journal.Shift, nativeFrameLimit)
	frame := arm64.FrameSize(1<<journal.Shift, nativeFrameLimit)

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	abiFile := filepath.Join(filepath.Dir(thisFile), "..", "internal", "asm", "arm64", "abi_arm64.s")
	src, err := os.ReadFile(abiFile)
	require.NoError(t, err)

	reserveLiteral := regexp.MustCompile(`ADD\s+\$(\d+),\s*RSP`).FindSubmatch(src)
	require.NotNil(t, reserveLiteral, "expected an ADD $N, RSP reserve instruction in %s", abiFile)
	reserveVal, err := strconv.Atoi(string(reserveLiteral[1]))
	require.NoError(t, err)
	require.Equal(t, reserveVal, reserve,
		"arm64.StackReserve(1<<journal.Shift, nativeFrameLimit) must equal the trampoline's ADD $N, RSP reserve")

	frameLiteral := regexp.MustCompile(`TEXT ·invoke\(SB\), \$(\d+)-`).FindSubmatch(src)
	require.NotNil(t, frameLiteral, "expected a TEXT ·invoke(SB), $N-M frame size in %s", abiFile)
	frameVal, err := strconv.Atoi(string(frameLiteral[1]))
	require.NoError(t, err)
	require.Equal(t, frameVal, frame,
		"arm64.FrameSize(1<<journal.Shift, nativeFrameLimit) must equal the trampoline's TEXT frame size")
}
