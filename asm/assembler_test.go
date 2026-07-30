package asm_test

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
)

// noFrameArch disables spilling so register exhaustion stays observable.
type noFrameArch struct{ asm.Arch }

func (noFrameArch) Frame() asm.Frame { return nil }

func TestNew(t *testing.T) {
	require.NotNil(t, asm.New(arm64.New()))
}

func TestAssembler_Reg(t *testing.T) {
	assembler := asm.New(arm64.New())

	require.Equal(t, asm.NewVReg(0, asm.RegTypeInt, asm.Width64), assembler.Reg(asm.RegTypeInt, asm.Width64))
	require.Equal(t, asm.NewVReg(1, asm.RegTypeFloat, asm.Width32), assembler.Reg(asm.RegTypeFloat, asm.Width32))
}

func TestAssembler_Label(t *testing.T) {
	assembler := asm.New(arm64.New())
	require.NotEqual(t, assembler.Label(), assembler.Label())
}

func TestAssembler_Bind(t *testing.T) {
	t.Run("resolves a branch to the bound position", func(t *testing.T) {
		assembler := asm.New(arm64.New())
		target := assembler.Label()
		assembler.Emit(arm64.BLabel(target))
		assembler.Emit(arm64.RET())
		assembler.Bind(target)
		assembler.Emit(arm64.RET())

		code, err := assembler.Build()
		require.NoError(t, err)
		// B encodes its displacement as imm26 instruction counts, so the
		// branch over one RET is +2: 0x14000002, little endian.
		require.Equal(t, []byte{0x02, 0x00, 0x00, 0x14}, code[:4])
	})

	t.Run("rejects a branch to an unbound label", func(t *testing.T) {
		assembler := asm.New(arm64.New())
		assembler.Emit(arm64.BLabel(assembler.Label()))

		_, err := assembler.Build()
		require.ErrorIs(t, err, asm.ErrUnresolvedLabel)
	})
}

func TestAssembler_Pin(t *testing.T) {
	assembler := asm.New(arm64.New())
	v := assembler.Reg(asm.RegTypeInt, asm.Width64)

	require.NoError(t, assembler.Pin(v, arm64.X0))
	require.NoError(t, assembler.Pin(v, arm64.X0))
	require.ErrorIs(t, assembler.Pin(v, arm64.X1), asm.ErrConflictingPin)

	_, err := assembler.Build()
	require.ErrorIs(t, err, asm.ErrConflictingPin)
}

func TestAssembler_Emit(t *testing.T) {
	t.Run("encodes appended instructions", func(t *testing.T) {
		assembler := asm.New(arm64.New())
		assembler.Emit(arm64.RET())

		code, err := assembler.Build()
		require.NoError(t, err)
		require.NotEmpty(t, code)
	})

	t.Run("pseudo use emits no bytes", func(t *testing.T) {
		assembler := asm.New(arm64.New())
		v := assembler.Reg(asm.RegTypeInt, asm.Width64)
		assembler.Emit(arm64.LDI(v, 1)...)
		assembler.Emit(asm.Instruction{Op: asm.OpPseudoUse, Src1: asm.V(v)})
		assembler.Emit(arm64.RET())

		code, err := assembler.Build()
		require.NoError(t, err)

		without := asm.New(arm64.New())
		v = without.Reg(asm.RegTypeInt, asm.Width64)
		without.Emit(arm64.LDI(v, 1)...)
		without.Emit(arm64.RET())

		want, err := without.Build()
		require.NoError(t, err)
		require.Equal(t, want, code)
	})

	t.Run("pseudo use extends live ranges", func(t *testing.T) {
		const n = 64

		without := asm.New(noFrameArch{arm64.New()})
		for i := 0; i < n; i++ {
			v := without.Reg(asm.RegTypeInt, asm.Width64)
			without.Emit(arm64.LDI(v, uint64(i))...)
		}
		without.Emit(arm64.RET())
		_, err := without.Build()
		require.NoError(t, err)

		assembler := asm.New(noFrameArch{arm64.New()})
		values := make([]asm.VReg, n)
		for i := range values {
			values[i] = assembler.Reg(asm.RegTypeInt, asm.Width64)
			assembler.Emit(arm64.LDI(values[i], uint64(i))...)
		}
		for _, v := range values {
			assembler.Emit(asm.Instruction{Op: asm.OpPseudoUse, Src1: asm.V(v)})
		}
		assembler.Emit(arm64.RET())

		_, err = assembler.Build()
		require.ErrorIs(t, err, asm.ErrNoRegistersAvailable)
	})
}

func TestAssembler_Build(t *testing.T) {
	t.Run("rejects a virtual register the assembler never handed out", func(t *testing.T) {
		assembler := asm.New(arm64.New())
		assembler.Emit(arm64.MOV(asm.NewVReg(7, asm.RegTypeInt, asm.Width64), arm64.X0))

		_, err := assembler.Build()
		require.ErrorIs(t, err, asm.ErrInvalidOperand)
	})

	t.Run("arch without a frame rejects spilling", func(t *testing.T) {
		// An Arch whose Frame() returns nil disables spilling: allocation
		// fails with ErrNoRegistersAvailable instead of inserting a spill
		// frame. Callers that need this (e.g. interp's JIT policy for a
		// trace ending in a terminal heap mutation) wrap an existing Arch
		// rather than the Assembler exposing a dedicated toggle.
		assembler := asm.New(noFrameArch{arm64.New()})
		emitWideSum(assembler, 64)

		_, err := assembler.Build()
		require.ErrorIs(t, err, asm.ErrNoRegistersAvailable)
	})

	if runtime.GOARCH != "arm64" {
		t.Skipf("native invoke requires arm64, got %s", runtime.GOARCH)
	}

	t.Run("context pointer round trip", func(t *testing.T) {
		arch := arm64.New()

		assembler := asm.New(arch)
		ctx := assembler.Reg(asm.RegTypeInt, asm.Width64)
		left := assembler.Reg(asm.RegTypeInt, asm.Width64)
		right := assembler.Reg(asm.RegTypeInt, asm.Width64)
		sum := assembler.Reg(asm.RegTypeInt, asm.Width64)
		require.NoError(t, assembler.Pin(ctx, arm64.X0))

		assembler.Emit(arm64.LDR(left, ctx, 0))
		assembler.Emit(arm64.LDR(right, ctx, 8))
		assembler.Emit(arm64.ADD(sum, left, right))
		assembler.Emit(arm64.STR(sum, ctx, 16))
		assembler.Emit(arm64.RET())

		code, err := assembler.Build()
		require.NoError(t, err)
		require.NotEmpty(t, code)

		buffer, err := asm.NewBuffer(4096)
		require.NoError(t, err)
		defer buffer.Free()

		callable, err := asm.Link(buffer, arch, code)
		require.NoError(t, err)

		values := [3]uint64{3, 4, 0}
		require.NoError(t, callable.Call(unsafe.Pointer(&values[0])))
		require.Equal(t, [3]uint64{3, 4, 7}, values)
	})

	t.Run("relaxes out-of-range branches", func(t *testing.T) {
		// Each case emits a branch whose target is pushed past its +-1MB
		// imm19 range by over 1MB of filler, forcing Build to relax it.
		branches := []struct {
			name string
			emit func(assembler *asm.Assembler, flag asm.VReg, zero asm.Label)
		}{
			{"CBZ", func(assembler *asm.Assembler, flag asm.VReg, zero asm.Label) {
				assembler.Emit(arm64.CBZLabel(flag, zero))
			}},
			{"B.cond", func(assembler *asm.Assembler, flag asm.VReg, zero asm.Label) {
				assembler.Emit(arm64.CMPI(flag, 0))
				assembler.Emit(arm64.BCondLabel(arm64.OpBEQ, zero))
			}},
		}
		for _, tt := range branches {
			t.Run(tt.name, func(t *testing.T) {
				arch := arm64.New()

				assembler := asm.New(arch)
				ctx := assembler.Reg(asm.RegTypeInt, asm.Width64)
				flag := assembler.Reg(asm.RegTypeInt, asm.Width64)
				filler := assembler.Reg(asm.RegTypeInt, asm.Width64)
				result := assembler.Reg(asm.RegTypeInt, asm.Width64)
				require.NoError(t, assembler.Pin(ctx, arm64.X0))

				zero := assembler.Label()

				assembler.Emit(arm64.LDR(flag, ctx, 0))
				tt.emit(assembler, flag, zero)

				const fillerCount = 1 << 18
				assembler.Emit(arm64.LDI(filler, 1)...)
				for i := 0; i < fillerCount; i++ {
					assembler.Emit(arm64.ADDI(filler, filler, 1))
				}

				assembler.Emit(arm64.LDI(result, 1)...)
				assembler.Emit(arm64.STR(result, ctx, 8))
				assembler.Emit(arm64.RET())

				assembler.Bind(zero)
				assembler.Emit(arm64.LDI(result, 0)...)
				assembler.Emit(arm64.STR(result, ctx, 8))
				assembler.Emit(arm64.RET())

				code, err := assembler.Build()
				require.NoError(t, err)
				require.Greater(t, len(code), 1<<20)

				buffer, err := asm.NewBuffer(len(code) + 4096)
				require.NoError(t, err)
				defer buffer.Free()

				callable, err := asm.Link(buffer, arch, code)
				require.NoError(t, err)

				notTaken := []uint64{1, 0xFF}
				require.NoError(t, callable.Call(unsafe.Pointer(&notTaken[0])))
				require.Equal(t, uint64(1), notTaken[1])

				taken := []uint64{0, 0xFF}
				require.NoError(t, callable.Call(unsafe.Pointer(&taken[0])))
				require.Equal(t, uint64(0), taken[1])
			})
		}
	})

	t.Run("spills under register pressure", func(t *testing.T) {
		// Hold far more values live at once than the integer bank has
		// allocatable registers, forcing the allocator to spill. Every value
		// stays live until the final fold, so the allocator must keep
		// spilling and reloading; a balanced SP frame is proven by the call
		// returning cleanly with the correct sum.
		arch := arm64.New()

		assembler := asm.New(arch)
		ctx := assembler.Reg(asm.RegTypeInt, asm.Width64)
		require.NoError(t, assembler.Pin(ctx, arm64.X0))
		sum := emitWideSum(assembler, 256)
		assembler.Emit(arm64.STR(sum, ctx, 0))
		assembler.Emit(arm64.RET())

		code, err := assembler.Build()
		require.NoError(t, err)

		buffer, err := asm.NewBuffer(4096)
		require.NoError(t, err)
		defer buffer.Free()

		callable, err := asm.Link(buffer, arch, code)
		require.NoError(t, err)

		want := uint64(0)
		for i := 0; i < 256; i++ {
			want += uint64(i*7 + 1)
		}
		// Run on a fresh goroutine each time so the spill frame is exercised
		// against a stack the Go runtime may still grow and relocate.
		for range 64 {
			values := [1]uint64{}
			done := make(chan error, 1)
			go func() {
				done <- callable.Call(unsafe.Pointer(&values[0]))
			}()
			require.NoError(t, <-done)
			require.Equal(t, want, values[0])
		}
	})
}

// emitWideSum loads n distinct values, keeps every one live, and folds them
// into a single register, so the register bank is oversubscribed by n.
func emitWideSum(assembler *asm.Assembler, n int) asm.VReg {
	values := make([]asm.VReg, n)
	for i := range values {
		values[i] = assembler.Reg(asm.RegTypeInt, asm.Width64)
		assembler.Emit(arm64.LDI(values[i], uint64(i*7+1))...)
	}
	sum := values[0]
	for _, v := range values[1:] {
		next := assembler.Reg(asm.RegTypeInt, asm.Width64)
		assembler.Emit(arm64.ADD(next, sum, v))
		sum = next
	}
	return sum
}
