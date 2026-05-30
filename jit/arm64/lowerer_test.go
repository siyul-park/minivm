package arm64_test

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/jit"
	jitarm64 "github.com/siyul-park/minivm/jit/arm64"
	"github.com/siyul-park/minivm/types"
)

// TestLowerer_Compile drives Phase A segments through the full
// jit.Compile → asm.Link → asm.Callable pipeline.
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
		require.Contains(t, mod.Segments, 0)

		scratch := make([]uint64, 5)
		_, err = mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)
		require.Equal(t, uint64(nopCount), scratch[4])
	})

	t.Run("i32_const sequence spills boxed values to stack memory", func(t *testing.T) {
		// I32_CONST 7; I32_CONST 11; I32_CONST 13
		code := []byte{
			byte(instr.I32_CONST), 0x07, 0x00, 0x00, 0x00,
			byte(instr.I32_CONST), 0x0B, 0x00, 0x00, 0x00,
			byte(instr.I32_CONST), 0x0D, 0x00, 0x00, 0x00,
		}
		fn := &types.Function{Code: code}

		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1)
		require.NoError(t, err)
		require.Contains(t, mod.Segments, 0)

		stack := make([]types.Boxed, 16)
		scratch := []uint64{
			uint64(uintptr(unsafe.Pointer(&stack[0]))),
			2, // entry sp
			0,
			0,
			0,
		}
		_, err = mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Equal(t, uint64(len(code)), scratch[4])
		require.Equal(t, uint64(5), scratch[1])
		require.Equal(t, types.BoxI32(7), stack[2])
		require.Equal(t, types.BoxI32(11), stack[3])
		require.Equal(t, types.BoxI32(13), stack[4])
	})

	t.Run("drop after const removes top without spilling it", func(t *testing.T) {
		// I32_CONST 9; I32_CONST 21; DROP
		code := []byte{
			byte(instr.I32_CONST), 0x09, 0x00, 0x00, 0x00,
			byte(instr.I32_CONST), 0x15, 0x00, 0x00, 0x00,
			byte(instr.DROP),
		}
		fn := &types.Function{Code: code}

		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1)
		require.NoError(t, err)
		require.Contains(t, mod.Segments, 0)

		stack := make([]types.Boxed, 16)
		scratch := []uint64{
			uint64(uintptr(unsafe.Pointer(&stack[0]))),
			0,
			0,
			0,
			0,
		}
		_, err = mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Equal(t, uint64(1), scratch[1])
		require.Equal(t, types.BoxI32(9), stack[0])
	})

	t.Run("dup duplicates top of stack", func(t *testing.T) {
		// I32_CONST 42; DUP
		code := []byte{
			byte(instr.I32_CONST), 0x2A, 0x00, 0x00, 0x00,
			byte(instr.DUP),
		}
		fn := &types.Function{Code: code}

		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1)
		require.NoError(t, err)
		require.Contains(t, mod.Segments, 0)

		stack := make([]types.Boxed, 16)
		scratch := []uint64{
			uint64(uintptr(unsafe.Pointer(&stack[0]))),
			0,
			0,
			0,
			0,
		}
		_, err = mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Equal(t, uint64(2), scratch[1])
		require.Equal(t, types.BoxI32(42), stack[0])
		require.Equal(t, types.BoxI32(42), stack[1])
	})
}
