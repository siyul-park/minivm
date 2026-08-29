package arm64_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"testing"

	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/stretchr/testify/require"
)

// TestFrameSize verifies abi_arm64.s's own internal consistency: its
// TEXT ·invoke(SB) frame-size literal must equal its ADD $N, RSP reserve
// literal plus arm64.SaveAreaBytes, independent of whatever native
// call-depth cap the reserve was sized for. A hand-written .s literal has no
// way to read a Go constant, so this narrow check is the part of the
// invariant this package can verify on its own.
//
// interp.TestARM64_StackReserve (interp/tier_test.go) carries the other
// half: that the reserve literal itself equals arm64.StackReserve for
// interp's own native-call-depth cap, a private interp constant this
// package cannot see. See docs/jit-internals.md.
func TestFrameSize(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	abiFile := filepath.Join(filepath.Dir(thisFile), "abi_arm64.s")
	src, err := os.ReadFile(abiFile)
	require.NoError(t, err)

	reserve := parseStackLiteral(t, src, `ADD\s+\$(\d+),\s*RSP`, abiFile)
	frame := parseStackLiteral(t, src, `TEXT ·invoke\(SB\), \$(\d+)-`, abiFile)

	require.Equal(t, frame, reserve+arm64.SaveAreaBytes,
		"abi_arm64.s's TEXT frame size must equal its ADD reserve plus arm64.SaveAreaBytes")
}

func parseStackLiteral(t *testing.T, src []byte, pattern, file string) int {
	t.Helper()
	match := regexp.MustCompile(pattern).FindSubmatch(src)
	require.NotNil(t, match, "expected %s in %s", pattern, file)
	n, err := strconv.Atoi(string(match[1]))
	require.NoError(t, err)
	return n
}
