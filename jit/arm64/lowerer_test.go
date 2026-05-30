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

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
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

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
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

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
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

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
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

	t.Run("swap reorders top two", func(t *testing.T) {
		// I32_CONST 5; I32_CONST 9; SWAP
		code := []byte{
			byte(instr.I32_CONST), 0x05, 0x00, 0x00, 0x00,
			byte(instr.I32_CONST), 0x09, 0x00, 0x00, 0x00,
			byte(instr.SWAP),
		}
		fn := &types.Function{Code: code}

		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
		require.NoError(t, err)

		stack := make([]types.Boxed, 16)
		scratch := []uint64{
			uint64(uintptr(unsafe.Pointer(&stack[0]))),
			0, 0, 0, 0,
		}
		_, err = mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Equal(t, uint64(2), scratch[1])
		require.Equal(t, types.BoxI32(9), stack[0])
		require.Equal(t, types.BoxI32(5), stack[1])
	})

	t.Run("const_get emits compile-time immediate", func(t *testing.T) {
		// CONST_GET 1
		code := []byte{byte(instr.CONST_GET), 0x01}
		fn := &types.Function{Code: code}

		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		snap := jit.Snapshot{Constants: []types.Boxed{types.BoxI32(0), types.BoxI32(77)}}
		mod, err := c.Compile(fn, 1, snap)
		require.NoError(t, err)

		stack := make([]types.Boxed, 16)
		scratch := []uint64{
			uint64(uintptr(unsafe.Pointer(&stack[0]))),
			0, 0, 0, 0,
		}
		_, err = mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Equal(t, uint64(1), scratch[1])
		require.Equal(t, types.BoxI32(77), stack[0])
	})

	t.Run("global_set then global_get roundtrips through memory", func(t *testing.T) {
		// I32_CONST 25; GLOBAL_SET 0; GLOBAL_GET 0
		code := []byte{
			byte(instr.I32_CONST), 0x19, 0x00, 0x00, 0x00,
			byte(instr.GLOBAL_SET), 0x00, 0x00,
			byte(instr.GLOBAL_GET), 0x00, 0x00,
		}
		fn := &types.Function{Code: code}

		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		globals := []types.Boxed{types.BoxI32(0)}
		snap := jit.Snapshot{Globals: globals}
		mod, err := c.Compile(fn, 1, snap)
		require.NoError(t, err)

		stack := make([]types.Boxed, 16)
		scratch := []uint64{
			uint64(uintptr(unsafe.Pointer(&stack[0]))),
			0,
			uint64(uintptr(unsafe.Pointer(&globals[0]))),
			0,
			0,
		}
		_, err = mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Equal(t, types.BoxI32(25), globals[0])
		require.Equal(t, uint64(1), scratch[1])
		require.Equal(t, types.BoxI32(25), stack[0])
	})

	t.Run("local_set then local_get with bp offset", func(t *testing.T) {
		// I32_CONST 88; LOCAL_SET 1; LOCAL_GET 1
		code := []byte{
			byte(instr.I32_CONST), 0x58, 0x00, 0x00, 0x00,
			byte(instr.LOCAL_SET), 0x01,
			byte(instr.LOCAL_GET), 0x01,
		}
		fn := &types.Function{
			Typ:  &types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeI32}},
			Code: code,
		}

		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		snap := jit.Snapshot{Locals: []types.Kind{types.KindI32, types.KindI32}}
		mod, err := c.Compile(fn, 1, snap)
		require.NoError(t, err)

		// Frame layout: stack[bp..bp+2] hold the two locals; entry sp = bp + 2.
		const bp = 0
		stack := make([]types.Boxed, 16)
		scratch := []uint64{
			uint64(uintptr(unsafe.Pointer(&stack[0]))),
			2, // entry sp
			0,
			uint64(bp),
			0,
		}
		_, err = mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Equal(t, types.BoxI32(88), stack[bp+1])
		require.Equal(t, types.BoxI32(88), stack[2])
		require.Equal(t, uint64(3), scratch[1])
	})

	t.Run("i32_add of two consts produces boxed sum", func(t *testing.T) {
		// I32_CONST 7; I32_CONST 5; I32_ADD
		code := []byte{
			byte(instr.I32_CONST), 0x07, 0x00, 0x00, 0x00,
			byte(instr.I32_CONST), 0x05, 0x00, 0x00, 0x00,
			byte(instr.I32_ADD),
		}
		fn := &types.Function{Code: code}

		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
		require.NoError(t, err)

		stack := make([]types.Boxed, 16)
		scratch := []uint64{
			uint64(uintptr(unsafe.Pointer(&stack[0]))),
			0, 0, 0, 0,
		}
		_, err = mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Equal(t, uint64(1), scratch[1])
		require.Equal(t, types.BoxI32(12), stack[0])
	})

	t.Run("i32_eqz returns boxed 1 for zero and 0 otherwise", func(t *testing.T) {
		t.Run("zero", func(t *testing.T) {
			// I32_CONST 0; I32_EQZ
			code := []byte{
				byte(instr.I32_CONST), 0x00, 0x00, 0x00, 0x00,
				byte(instr.I32_EQZ),
			}
			fn := &types.Function{Code: code}
			c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
			require.NoError(t, err)
			defer c.Close()

			mod, err := c.Compile(fn, 1, jit.Snapshot{})
			require.NoError(t, err)

			stack := make([]types.Boxed, 16)
			scratch := []uint64{
				uint64(uintptr(unsafe.Pointer(&stack[0]))),
				0, 0, 0, 0,
			}
			_, err = mod.Segments[0].Call(nil, scratch)
			require.NoError(t, err)

			require.Equal(t, types.BoxI32(1), stack[0])
		})

		t.Run("non-zero", func(t *testing.T) {
			// I32_CONST 42; I32_EQZ
			code := []byte{
				byte(instr.I32_CONST), 0x2A, 0x00, 0x00, 0x00,
				byte(instr.I32_EQZ),
			}
			fn := &types.Function{Code: code}
			c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
			require.NoError(t, err)
			defer c.Close()

			mod, err := c.Compile(fn, 1, jit.Snapshot{})
			require.NoError(t, err)

			stack := make([]types.Boxed, 16)
			scratch := []uint64{
				uint64(uintptr(unsafe.Pointer(&stack[0]))),
				0, 0, 0, 0,
			}
			_, err = mod.Segments[0].Call(nil, scratch)
			require.NoError(t, err)

			require.Equal(t, types.BoxI32(0), stack[0])
		})
	})
}
