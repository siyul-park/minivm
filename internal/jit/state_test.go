package jit_test

import (
	"testing"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestState_PC(t *testing.T) {
	testenv.MustHaveArch(t, "arm64")
	ctx, err := jit.NewContext(4096)
	require.NoError(t, err)
	body := exit(1, jit.TrapBridge)
	address := link(t, body...)

	require.Equal(t, jit.TrapBridge, jit.Enter(address, ctx))

	// link's two-row frame setup, then body ending in the stub's BLR.
	require.Equal(t, address+uintptr(4*(2+len(body))), ctx.PC())
}

func TestState_Exited(t *testing.T) {
	testenv.MustHaveArch(t, "arm64")
	ctx, err := jit.NewContext(4096)
	require.NoError(t, err)
	require.False(t, ctx.Exited())

	require.Equal(t, jit.TrapBridge, jit.Enter(link(t, exit(1, jit.TrapBridge)...), ctx))
	require.True(t, ctx.Exited())

	require.Equal(t, jit.TrapReturn, jit.Resume(ctx))
	require.False(t, ctx.Exited())
}

func TestState_Abandon(t *testing.T) {
	testenv.MustHaveArch(t, "arm64")

	t.Run("runs the next activation from the stack's top", func(t *testing.T) {
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		probe := link(t, append([]asm.Instruction{arm64.ADDI(arm64.X0, arm64.SP, 16)}, exit(1, jit.TrapBridge)...)...)

		require.Equal(t, jit.TrapBridge, jit.Enter(probe, ctx))
		first := ctx.Reg(arm64.X0)

		ctx.Abandon()

		require.Equal(t, jit.TrapBridge, jit.Enter(probe, ctx))
		require.Equal(t, first, ctx.Reg(arm64.X0))
	})

	t.Run("keeps the activation suspended below the abandoned one", func(t *testing.T) {
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		probe := link(t, append([]asm.Instruction{arm64.ADDI(arm64.X0, arm64.SP, 16)}, exit(1, jit.TrapBridge)...)...)

		require.Equal(t, jit.TrapBridge, jit.Enter(probe, ctx))
		outer := ctx.Reg(arm64.X0)
		require.Equal(t, jit.TrapBridge, jit.Enter(probe, ctx))
		nested := ctx.Reg(arm64.X0)
		require.Less(t, nested, outer)

		ctx.Abandon()

		require.Equal(t, jit.TrapBridge, jit.Enter(probe, ctx))
		require.Equal(t, nested, ctx.Reg(arm64.X0))
	})
}

func TestState_Reg(t *testing.T) {
	ctx, err := jit.NewContext(4096)
	require.NoError(t, err)

	for id := range 32 {
		ctx.SetReg(asm.NewPReg(uint8(id), asm.RegTypeInt, asm.Width64), uint64(id)+1)
		ctx.SetReg(asm.NewPReg(uint8(id), asm.RegTypeFloat, asm.Width64), ^uint64(id))
	}
	for id := range 32 {
		require.Equal(t, uint64(id)+1, ctx.Reg(asm.NewPReg(uint8(id), asm.RegTypeInt, asm.Width64)))
		require.Equal(t, ^uint64(id), ctx.Reg(asm.NewPReg(uint8(id), asm.RegTypeFloat, asm.Width64)))
	}
}

func TestState_SetReg(t *testing.T) {
	t.Run("widths share the same saved register", func(t *testing.T) {
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		ctx.SetReg(arm64.X3, ^uint64(0))
		require.Equal(t, ^uint64(0), ctx.Reg(arm64.W3))
		ctx.SetReg(arm64.W3, 0)
		require.Zero(t, ctx.Reg(arm64.X3))
	})

	t.Run("register files belong to their context", func(t *testing.T) {
		first, err := jit.NewContext(4096)
		require.NoError(t, err)
		second, err := jit.NewContext(4096)
		require.NoError(t, err)
		first.SetReg(arm64.X3, 42)
		require.Zero(t, second.Reg(arm64.X3))
		second.SetReg(arm64.X3, 84)
		require.Equal(t, uint64(42), first.Reg(arm64.X3))
	})
}

func TestState_Slot(t *testing.T) {
	t.Run("rejects a negative spill slot", func(t *testing.T) {
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		require.Panics(t, func() { ctx.Slot(-1) })
	})
	t.Run("reads a suspended spill slot", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		code := link(t, append([]asm.Instruction{
			arm64.MOVI(arm64.X0, 42),
			arm64.STR(arm64.X0, arm64.SP, 0),
		}, exit(1, jit.TrapBridge)...)...)

		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		require.Equal(t, uint64(42), ctx.Slot(0))
	})
}

func TestState_Word(t *testing.T) {
	t.Run("reads a word of the native stack by address", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		code := link(t, append([]asm.Instruction{
			arm64.MOVI(arm64.X1, 42),
			arm64.STR(arm64.X1, arm64.SP, 0),
			arm64.ADDI(arm64.X0, arm64.SP, 0),
		}, exit(1, jit.TrapBridge)...)...)

		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		require.Equal(t, uint64(42), ctx.Word(uintptr(ctx.Reg(arm64.X0))))
	})

	t.Run("rejects an address outside the native stack", func(t *testing.T) {
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		require.Panics(t, func() { ctx.Word(8) })
	})
}
