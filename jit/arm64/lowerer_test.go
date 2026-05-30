package arm64_test

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/jit"
	jitarm64 "github.com/siyul-park/minivm/jit/arm64"
	"github.com/siyul-park/minivm/types"
)

// TestLowerer_Compile drives a NOP-only segment through the full
// jit.Compile → asm.Link → asm.Callable pipeline to confirm the segment
// ABI (scratch slot conventions, exit IP write) is consistent end-to-end.
func TestLowerer_Compile(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skipf("native invoke requires arm64, got %s", runtime.GOARCH)
	}

	t.Run("nop chain writes exit IP to scratch", func(t *testing.T) {
		const nopCount = 10
		code := make([]byte, nopCount)
		for i := range code {
			code[i] = byte(instr.NOP)
		}
		fn := &types.Function{Code: code}

		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1)
		require.NoError(t, err)
		require.NotNil(t, mod)
		require.Contains(t, mod.Segments, 0)

		// scratch layout: 0=stack, 1=sp, 2=globals, 3=constants, 4=next IP.
		scratch := make([]uint64, 5)
		_, err = mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)
		require.Equal(t, uint64(nopCount), scratch[4])
	})
}
