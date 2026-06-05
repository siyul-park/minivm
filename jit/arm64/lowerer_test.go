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
		// CONST_GET 1  (3-byte encoding: opcode + uint16 index)
		code := []byte{byte(instr.CONST_GET), 0x01, 0x00}
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

	t.Run("const_get ref rejects without immediate call", func(t *testing.T) {
		code := []byte{
			byte(instr.CONST_GET), 0x00, 0x00,
			byte(instr.DROP),
		}
		fn := &types.Function{Code: code}

		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		slots, err := c.Slots()
		require.NoError(t, err)
		require.NotNil(t, slots)

		mod, err := c.Compile(fn, 1, jit.Snapshot{
			Constants: []types.Boxed{types.BoxRef(7)},
			Functions: map[int]*types.Function{
				7: &types.Function{Typ: &types.FunctionType{}},
			},
		})
		require.NoError(t, err)
		require.Nil(t, mod.Entry)
		require.Empty(t, mod.Segments)
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

	t.Run("global_tee stores and keeps value on stack", func(t *testing.T) {
		code := instr.Marshal([]instr.Instruction{
			instr.New(instr.I32_CONST, 31),
			instr.New(instr.GLOBAL_TEE, 0),
		})
		fn := &types.Function{Code: code}

		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		globals := []types.Boxed{types.BoxI32(0)}
		mod, err := c.Compile(fn, 1, jit.Snapshot{Globals: globals})
		require.NoError(t, err)

		stack := make([]types.Boxed, 16)
		scratch := make([]uint64, jit.ScratchCount)
		scratch[jit.ScratchStack] = uint64(uintptr(unsafe.Pointer(&stack[0])))
		scratch[jit.ScratchGlobals] = uint64(uintptr(unsafe.Pointer(&globals[0])))
		got, err := mod.Segments[0].Call(nil, scratch)
		require.NoError(t, err)

		require.Equal(t, types.BoxI32(31), globals[0])
		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(31), jit.Ret(got[0]))
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

	t.Run("i32 logic ops preserve boxed results", func(t *testing.T) {
		tests := []struct {
			op   instr.Opcode
			want types.Boxed
		}{
			{instr.I32_AND, types.BoxI32(0x02)},
			{instr.I32_OR, types.BoxI32(0x07)},
			{instr.I32_XOR, types.BoxI32(0x05)},
		}
		for _, tt := range tests {
			code := instr.Marshal([]instr.Instruction{
				instr.New(instr.I32_CONST, 0x03),
				instr.New(instr.I32_CONST, 0x06),
				instr.New(tt.op),
			})
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
			require.Equal(t, tt.want, jit.Ret(got[0]))
		}
	})

	t.Run("integer conversions preserve boxed lane", func(t *testing.T) {
		tests := []struct {
			insts []instr.Instruction
			want  types.Boxed
		}{
			{
				insts: []instr.Instruction{
					instr.New(instr.I32_CONST, uint64(uint32(0xFFFFFFFF))),
					instr.New(instr.I32_TO_I64_S),
				},
				want: types.BoxI64(-1),
			},
			{
				insts: []instr.Instruction{
					instr.New(instr.I32_CONST, uint64(uint32(0xFFFFFFFF))),
					instr.New(instr.I32_TO_I64_U),
				},
				want: types.BoxI64(0xFFFFFFFF),
			},
			{
				insts: []instr.Instruction{
					instr.New(instr.I64_CONST, 0x100000001),
					instr.New(instr.I64_TO_I32),
				},
				want: types.BoxI32(1),
			},
		}
		for _, tt := range tests {
			fn := &types.Function{Code: instr.Marshal(tt.insts)}

			c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
			require.NoError(t, err)
			defer c.Close()

			mod, err := c.Compile(fn, 1, jit.Snapshot{})
			require.NoError(t, err)

			scratch := make([]uint64, jit.ScratchCount)
			got, err := mod.Segments[0].Call(nil, scratch)
			require.NoError(t, err)
			require.Len(t, got, 1)
			require.Equal(t, tt.want, jit.Ret(got[0]))
		}
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

	t.Run("f64_add adds two float64 values", func(t *testing.T) {
		// F64_CONST 2.0; F64_CONST 3.0; F64_ADD
		code := []byte{
			byte(instr.F64_CONST), 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x40, // 2.0
			byte(instr.F64_CONST), 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x08, 0x40, // 3.0
			byte(instr.F64_ADD),
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
		require.Equal(t, types.BoxF64(5.0), jit.Ret(got[0]))
	})

	t.Run("f32_add adds two float32 values", func(t *testing.T) {
		// F32_CONST 2.0; F32_CONST 3.0; F32_ADD
		code := []byte{
			byte(instr.F32_CONST), 0x00, 0x00, 0x00, 0x40, // 2.0
			byte(instr.F32_CONST), 0x00, 0x00, 0x40, 0x40, // 3.0
			byte(instr.F32_ADD),
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
		require.Equal(t, types.BoxF32(5.0), jit.Ret(got[0]))
	})

	t.Run("f32_lt returns 1 when less", func(t *testing.T) {
		// F32_CONST 1.0; F32_CONST 2.0; F32_LT  →  1.0 < 2.0 → 1
		code := []byte{
			byte(instr.F32_CONST), 0x00, 0x00, 0x80, 0x3F, // 1.0
			byte(instr.F32_CONST), 0x00, 0x00, 0x00, 0x40, // 2.0
			byte(instr.F32_LT),
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

	t.Run("f64_gt returns 1 when greater", func(t *testing.T) {
		// F64_CONST 3.0; F64_CONST 1.0; F64_GT  →  3.0 > 1.0 → 1
		code := []byte{
			byte(instr.F64_CONST), 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x08, 0x40, // 3.0
			byte(instr.F64_CONST), 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF0, 0x3F, // 1.0
			byte(instr.F64_GT),
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

	// NaN comparison tests: AArch64 FCMP with a NaN operand sets NZCV=0011
	// (N=0,Z=0,C=1,V=1). All six Wasm ordered-comparison conditions correctly
	// produce 0 (false) for NaN inputs except F32_NE which produces 1 (true).
	t.Run("f32_lt with NaN returns 0", func(t *testing.T) {
		// F32_CONST NaN(0x7FC00000); F32_CONST 1.0; F32_LT → 0
		code := []byte{
			byte(instr.F32_CONST), 0x00, 0x00, 0xC0, 0x7F, // NaN
			byte(instr.F32_CONST), 0x00, 0x00, 0x80, 0x3F, // 1.0
			byte(instr.F32_LT),
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

	t.Run("f32_gt with NaN returns 0", func(t *testing.T) {
		// F32_CONST NaN; F32_CONST 1.0; F32_GT → 0
		code := []byte{
			byte(instr.F32_CONST), 0x00, 0x00, 0xC0, 0x7F, // NaN
			byte(instr.F32_CONST), 0x00, 0x00, 0x80, 0x3F, // 1.0
			byte(instr.F32_GT),
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

	t.Run("f32_ge with NaN returns 0", func(t *testing.T) {
		// F32_CONST NaN; F32_CONST 1.0; F32_GE → 0
		code := []byte{
			byte(instr.F32_CONST), 0x00, 0x00, 0xC0, 0x7F, // NaN
			byte(instr.F32_CONST), 0x00, 0x00, 0x80, 0x3F, // 1.0
			byte(instr.F32_GE),
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

	t.Run("f32_ne with NaN returns 1", func(t *testing.T) {
		// F32_CONST NaN; F32_CONST 1.0; F32_NE → 1
		code := []byte{
			byte(instr.F32_CONST), 0x00, 0x00, 0xC0, 0x7F, // NaN
			byte(instr.F32_CONST), 0x00, 0x00, 0x80, 0x3F, // 1.0
			byte(instr.F32_NE),
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

	t.Run("i32_to_f64_s converts signed i32 to f64", func(t *testing.T) {
		// I32_CONST 42; I32_TO_F64_S
		code := []byte{
			byte(instr.I32_CONST), 42, 0, 0, 0,
			byte(instr.I32_TO_F64_S),
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
		require.Equal(t, types.BoxF64(42.0), jit.Ret(got[0]))
	})

	t.Run("f32_to_f64 widens float32 to float64", func(t *testing.T) {
		// F32_CONST 1.5; F32_TO_F64
		code := []byte{
			byte(instr.F32_CONST), 0x00, 0x00, 0xC0, 0x3F, // 1.5
			byte(instr.F32_TO_F64),
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
		require.Equal(t, types.BoxF64(1.5), jit.Ret(got[0]))
	})

	t.Run("f64_to_f32 narrows float64 to float32", func(t *testing.T) {
		// F64_CONST 1.5; F64_TO_F32
		code := []byte{
			byte(instr.F64_CONST), 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF8, 0x3F, // 1.5
			byte(instr.F64_TO_F32),
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
		require.Equal(t, types.BoxF32(1.5), jit.Ret(got[0]))
	})

	t.Run("f32_to_i32_s truncates toward zero", func(t *testing.T) {
		// F32_CONST -2.5; F32_TO_I32_S  → int32(-2.5) == -2
		code := []byte{
			byte(instr.F32_CONST), 0x00, 0x00, 0x20, 0xC0, // -2.5
			byte(instr.F32_TO_I32_S),
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
		require.Equal(t, types.BoxI32(-2), jit.Ret(got[0]))
	})

	t.Run("f32_to_i32_u wraps via 64-bit truncation", func(t *testing.T) {
		// F32_CONST 3e9; F32_TO_I32_U  → int32(uint32(3e9)) == -1294967296.
		// 3e9 fits in int64 so FCVTZS does not saturate; the low 32 bits are
		// architecture-independent, mirroring the interpreter's Go codegen.
		code := []byte{
			byte(instr.F32_CONST), 0x5E, 0xD0, 0x32, 0x4F, // 3e9
			byte(instr.F32_TO_I32_U),
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
		require.Equal(t, types.BoxI32(-1294967296), jit.Ret(got[0]))
	})

	t.Run("f64_to_i32_s truncates toward zero", func(t *testing.T) {
		// F64_CONST -2.5; F64_TO_I32_S  → int32(-2.5) == -2
		code := []byte{
			byte(instr.F64_CONST), 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0xC0, // -2.5
			byte(instr.F64_TO_I32_S),
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
		require.Equal(t, types.BoxI32(-2), jit.Ret(got[0]))
	})

	t.Run("f64_to_i32_u wraps via 64-bit truncation", func(t *testing.T) {
		// F64_CONST 3e9; F64_TO_I32_U  → int32(uint32(3e9)) == -1294967296
		code := []byte{
			byte(instr.F64_CONST), 0x00, 0x00, 0x00, 0xC0, 0x0B, 0x5A, 0xE6, 0x41, // 3e9
			byte(instr.F64_TO_I32_U),
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
		require.Equal(t, types.BoxI32(-1294967296), jit.Ret(got[0]))
	})

	t.Run("f32_to_i64_s rejects before heap-promotion-sensitive conversion", func(t *testing.T) {
		// F32_CONST 1.5; F32_TO_I64_S  — the i64 result can exceed the boxable
		// lane, so the conversion stays threaded and the segment exits at it.
		code := []byte{
			byte(instr.F32_CONST), 0x00, 0x00, 0xC0, 0x3F, // 1.5
			byte(instr.F32_TO_I64_S),
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
		require.Equal(t, uint64(5), scratch[jit.ScratchNext])
		require.Equal(t, types.BoxF32(1.5), jit.Ret(got[0]))
	})

	t.Run("entry: const-return function compiles to Entry", func(t *testing.T) {
		// I32_CONST 99; RETURN  — leaf function with no params, one i32 return.
		code := []byte{
			byte(instr.I32_CONST), 99, 0, 0, 0,
			byte(instr.RETURN),
		}
		fn := &types.Function{
			Code: code,
			Typ:  &types.FunctionType{Params: nil, Returns: []types.Type{types.TypeI32}},
		}
		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
		require.NoError(t, err)
		require.NotNil(t, mod.Entry, "whole-function Entry should be set")
		require.Empty(t, mod.Segments, "segments must be empty when Entry is set")

		scratch := make([]uint64, jit.ScratchCount)
		got, err := mod.Entry.Call(nil, scratch)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(99), jit.Ret(got[0]))
	})

	t.Run("entry: rejects declared return mismatch", func(t *testing.T) {
		code := []byte{
			byte(instr.I32_CONST), 99, 0, 0, 0,
			byte(instr.RETURN),
		}
		fn := &types.Function{
			Code: code,
			Typ:  &types.FunctionType{},
		}
		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
		require.NoError(t, err)
		require.Nil(t, mod.Entry)
	})

	t.Run("entry: rejects synthetic stack inputs", func(t *testing.T) {
		code := []byte{
			byte(instr.DROP),
			byte(instr.RETURN),
		}
		fn := &types.Function{
			Code: code,
			Typ:  &types.FunctionType{},
		}
		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
		require.NoError(t, err)
		require.Nil(t, mod.Entry)
	})

	t.Run("call: direct-BL call from Entry to compiled callee doubles value", func(t *testing.T) {
		// Callee: LOCAL_GET 0; I32_CONST 2; I32_MUL; RETURN  — (i32) → i32
		calleeFn := &types.Function{
			Code: []byte{
				byte(instr.LOCAL_GET), 0,
				byte(instr.I32_CONST), 2, 0, 0, 0,
				byte(instr.I32_MUL),
				byte(instr.RETURN),
			},
			Typ: &types.FunctionType{
				Params:  []types.Type{types.TypeI32},
				Returns: []types.Type{types.TypeI32},
			},
		}
		const calleeAddr = 7 // arbitrary heap addr for the callee

		// Caller: LOCAL_GET 0; CONST_GET 0; CALL; RETURN  — (i32) → i32
		callerFn := &types.Function{
			Code: []byte{
				byte(instr.LOCAL_GET), 0,
				byte(instr.CONST_GET), 0, 0,
				byte(instr.CALL),
				byte(instr.RETURN),
			},
			Typ: &types.FunctionType{
				Params:  []types.Type{types.TypeI32},
				Returns: []types.Type{types.TypeI32},
			},
		}

		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		// Build slots + fallback.
		slots, err := c.Slots()
		require.NoError(t, err)
		require.NotNil(t, slots)

		// Compile callee first so the compiler populates its slot.
		calleeSnap := jit.Snapshot{Locals: []types.Kind{types.KindI32}}
		calleeMod, err := c.Compile(calleeFn, calleeAddr, calleeSnap)
		require.NoError(t, err)
		require.NotNil(t, calleeMod.Entry, "callee must compile to Entry")

		// Compile caller with the callee visible in Snap.Functions.
		callerSnap := jit.Snapshot{
			Constants: []types.Boxed{types.BoxRef(calleeAddr)},
			Locals:    []types.Kind{types.KindI32},
			Functions: map[int]*types.Function{calleeAddr: calleeFn},
		}
		callerMod, err := c.Compile(callerFn, 2, callerSnap)
		require.NoError(t, err)
		require.NotNil(t, callerMod.Entry, "caller must compile to Entry")

		// Set up a fake VM stack: vmStack[0] = BoxI32(21) at bp=0.
		var vmStack [8]types.Boxed
		vmStack[0] = types.BoxI32(21)
		scratch := make([]uint64, jit.ScratchCount)
		scratch[jit.ScratchStack] = uint64(uintptr(unsafe.Pointer(&vmStack[0])))
		scratch[jit.ScratchGlobals] = scratch[jit.ScratchStack]
		scratch[jit.ScratchBP] = 0

		got, err := callerMod.Entry.Call(nil, scratch)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(42), jit.Ret(got[0]))
	})

	t.Run("call: direct-BL preserves survivor across second call", func(t *testing.T) {
		calleeFn := &types.Function{
			Code: []byte{
				byte(instr.LOCAL_GET), 0,
				byte(instr.I32_CONST), 2, 0, 0, 0,
				byte(instr.I32_MUL),
				byte(instr.RETURN),
			},
			Typ: &types.FunctionType{
				Params:  []types.Type{types.TypeI32},
				Returns: []types.Type{types.TypeI32},
			},
		}
		const calleeAddr = 7

		callerFn := &types.Function{
			Code: []byte{
				byte(instr.LOCAL_GET), 0,
				byte(instr.CONST_GET), 0, 0,
				byte(instr.CALL),
				byte(instr.LOCAL_GET), 0,
				byte(instr.CONST_GET), 0, 0,
				byte(instr.CALL),
				byte(instr.I32_ADD),
				byte(instr.RETURN),
			},
			Typ: &types.FunctionType{
				Params:  []types.Type{types.TypeI32},
				Returns: []types.Type{types.TypeI32},
			},
		}

		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()
		_, err = c.Slots()
		require.NoError(t, err)

		calleeMod, err := c.Compile(calleeFn, calleeAddr, jit.Snapshot{Locals: []types.Kind{types.KindI32}})
		require.NoError(t, err)
		require.NotNil(t, calleeMod.Entry)

		callerMod, err := c.Compile(callerFn, 2, jit.Snapshot{
			Constants: []types.Boxed{types.BoxRef(calleeAddr)},
			Locals:    []types.Kind{types.KindI32},
			Functions: map[int]*types.Function{calleeAddr: calleeFn},
		})
		require.NoError(t, err)
		require.NotNil(t, callerMod.Entry)

		var vmStack [8]types.Boxed
		vmStack[0] = types.BoxI32(21)
		scratch := make([]uint64, jit.ScratchCount)
		scratch[jit.ScratchStack] = uint64(uintptr(unsafe.Pointer(&vmStack[0])))
		scratch[jit.ScratchGlobals] = scratch[jit.ScratchStack]
		scratch[jit.ScratchBP] = 0

		got, err := callerMod.Entry.Call(nil, scratch)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(84), jit.Ret(got[0]))
	})

	t.Run("call: self-recursive fibonacci compiles to Entry", func(t *testing.T) {
		code := instr.Marshal([]instr.Instruction{
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_LT_S),
			instr.New(instr.BR_IF, 26),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
			instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.RETURN),
		})
		fn := &types.Function{
			Code: code,
			Typ: &types.FunctionType{
				Params:  []types.Type{types.TypeI32},
				Returns: []types.Type{types.TypeI32},
			},
		}
		const selfAddr = 11

		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()
		_, err = c.Slots()
		require.NoError(t, err)

		mod, err := c.Compile(fn, selfAddr, jit.Snapshot{
			Constants: []types.Boxed{types.BoxRef(selfAddr)},
			Locals:    []types.Kind{types.KindI32},
			Functions: map[int]*types.Function{selfAddr: fn},
		})
		require.NoError(t, err)
		require.NotNil(t, mod.Entry)
		require.Empty(t, mod.Segments)

		var vmStack [64]types.Boxed
		vmStack[0] = types.BoxI32(7)
		scratch := make([]uint64, jit.ScratchCount)
		scratch[jit.ScratchStack] = uint64(uintptr(unsafe.Pointer(&vmStack[0])))
		scratch[jit.ScratchGlobals] = scratch[jit.ScratchStack]
		scratch[jit.ScratchBP] = 0

		got, err := mod.Entry.Call(nil, scratch)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(13), jit.Ret(got[0]))
	})

	t.Run("entry: carries stack values across branch block", func(t *testing.T) {
		code := instr.Marshal([]instr.Instruction{
			instr.New(instr.I32_CONST, 40),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.BR_IF, 0),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
		})
		fn := &types.Function{
			Code: code,
			Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		}

		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		mod, err := c.Compile(fn, 1, jit.Snapshot{})
		require.NoError(t, err)
		require.NotNil(t, mod.Entry)

		scratch := make([]uint64, jit.ScratchCount)
		got, err := mod.Entry.Call(nil, scratch)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(42), jit.Ret(got[0]))
	})

	t.Run("entry: two-param add function compiles to Entry", func(t *testing.T) {
		// LOCAL_GET 0; LOCAL_GET 1; I32_ADD; RETURN  — (i32, i32) → i32.
		// Params live at stack[bp+0] and stack[bp+1] via scratch slots.
		code := []byte{
			byte(instr.LOCAL_GET), 0,
			byte(instr.LOCAL_GET), 1,
			byte(instr.I32_ADD),
			byte(instr.RETURN),
		}
		fn := &types.Function{
			Code: code,
			Typ: &types.FunctionType{
				Params:  []types.Type{types.TypeI32, types.TypeI32},
				Returns: []types.Type{types.TypeI32},
			},
		}
		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		snap := jit.Snapshot{Locals: []types.Kind{types.KindI32, types.KindI32}}
		mod, err := c.Compile(fn, 1, snap)
		require.NoError(t, err)
		require.NotNil(t, mod.Entry, "whole-function Entry should be set")

		// Place boxed params in a fake VM stack, set ScratchBP.
		vmStack := [8]types.Boxed{types.BoxI32(10), types.BoxI32(32)}
		scratch := make([]uint64, jit.ScratchCount)
		scratch[jit.ScratchStack] = uint64(uintptr(unsafe.Pointer(&vmStack[0])))
		scratch[jit.ScratchGlobals] = scratch[jit.ScratchStack] // unused but non-nil
		scratch[jit.ScratchBP] = 0                              // bp=0: params at vmStack[0..1]

		got, err := mod.Entry.Call(nil, scratch)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(42), jit.Ret(got[0]))
	})

	t.Run("blocks mode: backward br_if loop compiles to entry", func(t *testing.T) {
		// Countdown loop: param[0] starts at 3, decrements to 0, returns 0.
		// whole() cannot compile this (BR_IF sets Closed=true), so blocks() must
		// succeed and produce a non-nil Entry.
		//
		// IP 0: LOCAL_GET 0   (2 bytes)
		// IP 2: I32_CONST 1   (5 bytes)
		// IP 7: I32_SUB       (1 byte)
		// IP 8: LOCAL_TEE 0   (2 bytes)
		// IP 10: BR_IF -13    (3 bytes)  takenTarget = 10+3-13 = 0
		// IP 13: LOCAL_GET 0  (2 bytes)
		// IP 15: RETURN        (1 byte)
		fn := &types.Function{
			Code: instr.Marshal([]instr.Instruction{
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_SUB),
				instr.New(instr.LOCAL_TEE, 0),
				instr.New(instr.BR_IF, uint64(uint16(-13+1<<16))),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.RETURN),
			}),
			Typ: &types.FunctionType{
				Params:  []types.Type{types.TypeI32},
				Returns: []types.Type{types.TypeI32},
			},
		}

		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		snap := jit.Snapshot{Locals: []types.Kind{types.KindI32}}
		mod, err := c.Compile(fn, 1, snap)
		require.NoError(t, err)
		require.NotNil(t, mod.Entry, "blocks() should produce a whole-function Entry for BR_IF loops")

		// param[0] = 3: after 3 decrements, local[0] = 0 → returns I32(0).
		vmStack := [8]types.Boxed{types.BoxI32(3)}
		scratch := make([]uint64, jit.ScratchCount)
		scratch[jit.ScratchStack] = uint64(uintptr(unsafe.Pointer(&vmStack[0])))
		scratch[jit.ScratchGlobals] = scratch[jit.ScratchStack]
		scratch[jit.ScratchBP] = 0

		got, err := mod.Entry.Call(nil, scratch)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, types.BoxI32(0), jit.Ret(got[0]))
	})

	t.Run("ref_null pushes null reference", func(t *testing.T) {
		code := []byte{byte(instr.REF_NULL)}
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
		require.Equal(t, types.BoxedNull, jit.Ret(got[0]))
	})

	t.Run("ref_is_null returns 1 for null ref", func(t *testing.T) {
		code := []byte{
			byte(instr.REF_NULL),
			byte(instr.REF_IS_NULL),
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
		require.Equal(t, types.BoxI32(1), jit.Ret(got[0]))
	})

	t.Run("ref_eq returns 1 for same ref", func(t *testing.T) {
		// REF_NULL; REF_NULL; REF_EQ  → 1 (both null)
		code := []byte{
			byte(instr.REF_NULL),
			byte(instr.REF_NULL),
			byte(instr.REF_EQ),
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
		require.Equal(t, types.BoxI32(1), jit.Ret(got[0]))
	})

	t.Run("global_tee rejects large offset", func(t *testing.T) {
		code := []byte{
			byte(instr.GLOBAL_TEE), 0x00, 0x10,
		}
		fn := &types.Function{Code: code}
		c, err := jit.New(jit.WithLowerer(jitarm64.Lowerer{}), jit.WithCutoff(1))
		require.NoError(t, err)
		defer c.Close()

		globals := make([]types.Boxed, 4097)
		mod, err := c.Compile(fn, 1, jit.Snapshot{Globals: globals})
		require.NoError(t, err)
		require.Nil(t, mod.Entry)
		require.Empty(t, mod.Segments)
	})

	// BR_TABLE tests: I32_CONST <cond>; BR_TABLE 2, 0, 8, 16
	//   IP 0: I32_CONST (5 bytes)
	//   IP 5: BR_TABLE count=2, width=8
	//   targets: [5+8+0=13, 5+8+8=21, default=5+8+16=29]
	t.Run("br_table first case routes to targets[0]", func(t *testing.T) {
		code := []byte{
			byte(instr.I32_CONST), 0, 0, 0, 0, // cond = 0  (IP 0)
			byte(instr.BR_TABLE), 2, 0, 0, 8, 0, 16, 0, // IP 5, width=8
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
		require.Equal(t, uint64(13), scratch[jit.ScratchNext]) // 5+8+0
	})

	t.Run("br_table second case routes to targets[1]", func(t *testing.T) {
		code := []byte{
			byte(instr.I32_CONST), 1, 0, 0, 0, // cond = 1  (IP 0)
			byte(instr.BR_TABLE), 2, 0, 0, 8, 0, 16, 0, // IP 5, width=8
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
		require.Equal(t, uint64(21), scratch[jit.ScratchNext]) // 5+8+8
	})

	t.Run("br_table out-of-range uses default", func(t *testing.T) {
		code := []byte{
			byte(instr.I32_CONST), 5, 0, 0, 0, // cond = 5 >= count (IP 0)
			byte(instr.BR_TABLE), 2, 0, 0, 8, 0, 16, 0, // IP 5, width=8
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
		require.Equal(t, uint64(29), scratch[jit.ScratchNext]) // 5+8+16
	})

	t.Run("br_table negative i32 uses default", func(t *testing.T) {
		code := []byte{
			byte(instr.I32_CONST), 0xFF, 0xFF, 0xFF, 0xFF, // cond = -1 as i32 (IP 0)
			byte(instr.BR_TABLE), 2, 0, 0, 8, 0, 16, 0, // IP 5, width=8
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
		require.Equal(t, uint64(29), scratch[jit.ScratchNext]) // 5+8+16
	})
}
