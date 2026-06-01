package arm64_test

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/asm"
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

		scratch := make([]uint64, jit.ScratchCount)
		_, err = mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)
		require.Equal(t, uint64(nopCount), scratch[jit.ScratchNext])
	})

	t.Run("i32_const sequence returns boxed values", func(t *testing.T) {
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

		scratch := make([]uint64, jit.ScratchCount)
		got, err := mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Equal(t, uint64(len(code)), scratch[jit.ScratchNext])
		require.Len(t, got, 3)
		require.Equal(t, types.BoxI32(7), jit.Ret(got[0]))
		require.Equal(t, types.BoxI32(11), jit.Ret(got[1]))
		require.Equal(t, types.BoxI32(13), jit.Ret(got[2]))
	})

	t.Run("drop after const removes top from returns", func(t *testing.T) {
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

		scratch := make([]uint64, jit.ScratchCount)
		got, err := mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(9), jit.Ret(got[0]))
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

		scratch := make([]uint64, jit.ScratchCount)
		got, err := mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Len(t, got, 2)
		require.Equal(t, types.BoxI32(42), jit.Ret(got[0]))
		require.Equal(t, types.BoxI32(42), jit.Ret(got[1]))
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

		scratch := make([]uint64, jit.ScratchCount)
		got, err := mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Len(t, got, 2)
		require.Equal(t, types.BoxI32(9), jit.Ret(got[0]))
		require.Equal(t, types.BoxI32(5), jit.Ret(got[1]))
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

		scratch := make([]uint64, jit.ScratchCount)
		got, err := mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(77), jit.Ret(got[0]))
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
		scratch := make([]uint64, jit.ScratchCount)
		scratch[jit.ScratchStack] = uint64(uintptr(unsafe.Pointer(&stack[0])))
		scratch[jit.ScratchGlobals] = uint64(uintptr(unsafe.Pointer(&globals[0])))
		got, err := mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Equal(t, types.BoxI32(25), globals[0])
		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(25), jit.Ret(got[0]))
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
		scratch := make([]uint64, jit.ScratchCount)
		scratch[jit.ScratchStack] = uint64(uintptr(unsafe.Pointer(&stack[0])))
		scratch[jit.ScratchBP] = uint64(bp)
		got, err := mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Equal(t, types.BoxI32(88), stack[bp+1])
		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(88), jit.Ret(got[0]))
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

		scratch := make([]uint64, jit.ScratchCount)
		got, err := mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(12), jit.Ret(got[0]))
	})

	t.Run("i32_add consumes caller args", func(t *testing.T) {
		code := []byte{byte(instr.I32_ADD)}
		fn := &types.Function{Code: code}

		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
		require.NoError(t, err)
		require.Contains(t, mod.Segments, 0)
		require.Equal(t, 2, mod.Stacks[0])

		args := []asm.Value{jit.Arg(types.BoxI32(7)), jit.Arg(types.BoxI32(5))}
		scratch := make([]uint64, jit.ScratchCount)
		got, err := mod.Segments[0].Call(args, scratch)
		require.NoError(t, err)

		require.Equal(t, uint64(len(code)), scratch[jit.ScratchNext])
		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(12), jit.Ret(got[0]))
	})

	t.Run("caller args keep stack order across staged underflow", func(t *testing.T) {
		code := []byte{
			byte(instr.I32_CONST), 0x01, 0x00, 0x00, 0x00,
			byte(instr.I32_ADD),
			byte(instr.I32_ADD),
		}
		fn := &types.Function{Code: code}

		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
		require.NoError(t, err)
		require.Contains(t, mod.Segments, 0)
		require.Equal(t, 2, mod.Stacks[0])

		args := []asm.Value{jit.Arg(types.BoxI32(7)), jit.Arg(types.BoxI32(5))}
		scratch := make([]uint64, jit.ScratchCount)
		got, err := mod.Segments[0].Call(args, scratch)
		require.NoError(t, err)

		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(13), jit.Ret(got[0]))
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

			scratch := make([]uint64, jit.ScratchCount)
			got, err := mod.Segments[0].Call(nil, scratch)
			require.NoError(t, err)

			require.Len(t, got, 1)
			require.Equal(t, types.BoxI32(1), jit.Ret(got[0]))
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

			scratch := make([]uint64, jit.ScratchCount)
			got, err := mod.Segments[0].Call(nil, scratch)
			require.NoError(t, err)

			require.Len(t, got, 1)
			require.Equal(t, types.BoxI32(0), jit.Ret(got[0]))
		})
	})

	t.Run("i32_lt_s distinguishes signed less-than from unsigned bit pattern", func(t *testing.T) {
		// I32_CONST -1; I32_CONST 1; I32_LT_S — signed compare must say -1 < 1 → 1.
		code := []byte{
			byte(instr.I32_CONST), 0xFF, 0xFF, 0xFF, 0xFF,
			byte(instr.I32_CONST), 0x01, 0x00, 0x00, 0x00,
			byte(instr.I32_LT_S),
		}
		fn := &types.Function{Code: code}
		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
		require.NoError(t, err)

		scratch := make([]uint64, jit.ScratchCount)
		got, err := mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(1), jit.Ret(got[0]))
	})

	t.Run("i32_lt_u treats high bit as positive", func(t *testing.T) {
		// I32_CONST -1; I32_CONST 1; I32_LT_U — unsigned -1 == 0xFFFFFFFF, so NOT < 1.
		code := []byte{
			byte(instr.I32_CONST), 0xFF, 0xFF, 0xFF, 0xFF,
			byte(instr.I32_CONST), 0x01, 0x00, 0x00, 0x00,
			byte(instr.I32_LT_U),
		}
		fn := &types.Function{Code: code}
		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
		require.NoError(t, err)

		scratch := make([]uint64, jit.ScratchCount)
		got, err := mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(0), jit.Ret(got[0]))
	})

	t.Run("i32_shl masks shift count to five bits", func(t *testing.T) {
		// I32_CONST 1; I32_CONST 3; I32_SHL → 8.
		code := []byte{
			byte(instr.I32_CONST), 0x01, 0x00, 0x00, 0x00,
			byte(instr.I32_CONST), 0x03, 0x00, 0x00, 0x00,
			byte(instr.I32_SHL),
		}
		fn := &types.Function{Code: code}
		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
		require.NoError(t, err)

		scratch := make([]uint64, jit.ScratchCount)
		got, err := mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(8), jit.Ret(got[0]))
	})

	t.Run("i32_shr_s preserves sign for negative inputs", func(t *testing.T) {
		// I32_CONST -8; I32_CONST 1; I32_SHR_S → -4.
		code := []byte{
			byte(instr.I32_CONST), 0xF8, 0xFF, 0xFF, 0xFF,
			byte(instr.I32_CONST), 0x01, 0x00, 0x00, 0x00,
			byte(instr.I32_SHR_S),
		}
		fn := &types.Function{Code: code}
		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
		require.NoError(t, err)

		scratch := make([]uint64, jit.ScratchCount)
		got, err := mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(-4), jit.Ret(got[0]))
	})

	t.Run("i32_shr_u zero-fills the high bit", func(t *testing.T) {
		// I32_CONST -8; I32_CONST 1; I32_SHR_U → unsigned -8 >> 1 = 0x7FFFFFFC.
		code := []byte{
			byte(instr.I32_CONST), 0xF8, 0xFF, 0xFF, 0xFF,
			byte(instr.I32_CONST), 0x01, 0x00, 0x00, 0x00,
			byte(instr.I32_SHR_U),
		}
		fn := &types.Function{Code: code}
		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
		require.NoError(t, err)

		scratch := make([]uint64, jit.ScratchCount)
		got, err := mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(0x7FFFFFFC), jit.Ret(got[0]))
	})

	t.Run("i64_add rejects before heap-promotion-sensitive arithmetic", func(t *testing.T) {
		// I64_CONST 7; I64_CONST 5; I64_ADD
		code := []byte{
			byte(instr.I64_CONST), 0x07, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			byte(instr.I64_CONST), 0x05, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			byte(instr.I64_ADD),
		}
		fn := &types.Function{Code: code}
		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
		require.NoError(t, err)

		scratch := make([]uint64, jit.ScratchCount)
		got, err := mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Len(t, got, 2)
		require.Equal(t, uint64(18), scratch[jit.ScratchNext])
		require.Equal(t, types.BoxI64(7), jit.Ret(got[0]))
		require.Equal(t, types.BoxI64(5), jit.Ret(got[1]))
	})

	t.Run("i64_lt_s recognises negative values via sign extension", func(t *testing.T) {
		// I64_CONST -3; I64_CONST 2; I64_LT_S → 1 (signed).
		code := []byte{
			byte(instr.I64_CONST), 0xFD, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
			byte(instr.I64_CONST), 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			byte(instr.I64_LT_S),
		}
		fn := &types.Function{Code: code}
		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
		require.NoError(t, err)

		scratch := make([]uint64, jit.ScratchCount)
		got, err := mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(1), jit.Ret(got[0]))
	})

	t.Run("i64_eqz returns boxed 1 for zero and 0 otherwise", func(t *testing.T) {
		t.Run("zero", func(t *testing.T) {
			code := []byte{
				byte(instr.I64_CONST), 0, 0, 0, 0, 0, 0, 0, 0,
				byte(instr.I64_EQZ),
			}
			fn := &types.Function{Code: code}
			c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
			require.NoError(t, err)
			defer c.Close()
			mod, err := c.Compile(fn, 1, jit.Snapshot{})
			require.NoError(t, err)
			scratch := make([]uint64, jit.ScratchCount)
			got, err := mod.Segments[0].Call(nil, scratch)
			require.NoError(t, err)
			require.Len(t, got, 1)
			require.Equal(t, types.BoxI32(1), jit.Ret(got[0]))
		})
		t.Run("non-zero", func(t *testing.T) {
			code := []byte{
				byte(instr.I64_CONST), 0x09, 0, 0, 0, 0, 0, 0, 0,
				byte(instr.I64_EQZ),
			}
			fn := &types.Function{Code: code}
			c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
			require.NoError(t, err)
			defer c.Close()
			mod, err := c.Compile(fn, 1, jit.Snapshot{})
			require.NoError(t, err)
			scratch := make([]uint64, jit.ScratchCount)
			got, err := mod.Segments[0].Call(nil, scratch)
			require.NoError(t, err)
			require.Len(t, got, 1)
			require.Equal(t, types.BoxI32(0), jit.Ret(got[0]))
		})
	})

	t.Run("br writes branch target to scratch", func(t *testing.T) {
		// 10 NOPs then BR +5: target = 10 + 3 + 5 = 18
		const nopCount = 10
		const offset int16 = 5
		code := make([]byte, nopCount+3)
		for i := 0; i < nopCount; i++ {
			code[i] = byte(instr.NOP)
		}
		code[nopCount] = byte(instr.BR)
		code[nopCount+1] = byte(offset)
		code[nopCount+2] = byte(offset >> 8)

		fn := &types.Function{Code: code}
		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
		require.NoError(t, err)
		require.Contains(t, mod.Segments, 0)

		scratch := make([]uint64, jit.ScratchCount)
		_, err = mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)
		require.Equal(t, uint64(nopCount+3+int(offset)), scratch[jit.ScratchNext])
	})

	t.Run("br_if taken writes taken-target to scratch", func(t *testing.T) {
		// I32_CONST 42; I32_CONST 1; BR_IF +7
		// takenTarget = 10 + 3 + 7 = 20; falseTarget = 10 + 3 = 13
		const offset int16 = 7
		code := []byte{
			byte(instr.I32_CONST), 42, 0, 0, 0, // IP 0..4
			byte(instr.I32_CONST), 1, 0, 0, 0, // IP 5..9
			byte(instr.BR_IF), byte(offset), byte(offset >> 8), // IP 10..12
		}
		fn := &types.Function{Code: code}
		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
		require.NoError(t, err)
		require.Contains(t, mod.Segments, 0)

		scratch := make([]uint64, jit.ScratchCount)
		got, err := mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)
		// condition was 1 (non-zero): taken path
		require.Equal(t, uint64(10+3+int(offset)), scratch[jit.ScratchNext])
		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(42), jit.Ret(got[0]))
	})

	t.Run("br_if not-taken writes fall-through IP to scratch", func(t *testing.T) {
		// I32_CONST 42; I32_CONST 0; BR_IF +7
		// falseTarget = 10 + 3 = 13
		const offset int16 = 7
		code := []byte{
			byte(instr.I32_CONST), 42, 0, 0, 0, // IP 0..4
			byte(instr.I32_CONST), 0, 0, 0, 0, // IP 5..9
			byte(instr.BR_IF), byte(offset), byte(offset >> 8), // IP 10..12
		}
		fn := &types.Function{Code: code}
		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
		require.NoError(t, err)
		require.Contains(t, mod.Segments, 0)

		scratch := make([]uint64, jit.ScratchCount)
		got, err := mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)
		// condition was 0: fall-through path
		require.Equal(t, uint64(10+3), scratch[jit.ScratchNext])
		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(42), jit.Ret(got[0]))
	})

	t.Run("i64_shr_s preserves sign for negative inputs", func(t *testing.T) {
		// I64_CONST -8; I64_CONST 1; I64_SHR_S
		code := []byte{
			byte(instr.I64_CONST), 0xF8, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
			byte(instr.I64_CONST), 1, 0, 0, 0, 0, 0, 0, 0,
			byte(instr.I64_SHR_S),
		}
		fn := &types.Function{Code: code}
		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
		require.NoError(t, err)

		scratch := make([]uint64, jit.ScratchCount)
		got, err := mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Len(t, got, 1)
		require.Equal(t, types.BoxI64(-4), jit.Ret(got[0]))
	})
}
