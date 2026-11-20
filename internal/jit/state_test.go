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

	intReg := asm.NewPReg(3, asm.RegTypeInt, asm.Width64)
	floatReg := asm.NewPReg(3, asm.RegTypeFloat, asm.Width64)
	ctx.SetReg(intReg, 42)
	ctx.SetReg(floatReg, 84)

	require.Equal(t, uint64(42), ctx.Reg(intReg))
	require.Equal(t, uint64(84), ctx.Reg(floatReg))
}

func TestState_SetReg(t *testing.T) {
	ctx, err := jit.NewContext(4096)
	require.NoError(t, err)

	reg := asm.NewPReg(3, asm.RegTypeInt, asm.Width64)
	ctx.SetReg(reg, 42)

	require.Equal(t, uint64(42), ctx.Reg(reg))
}

func TestState_Slot(t *testing.T) {
	testenv.MustHaveArch(t, "arm64")
	ctx, err := jit.NewContext(4096)
	require.NoError(t, err)
	code := link(t, append([]asm.Instruction{
		arm64.MOVI(arm64.X0, 42),
		arm64.STR(arm64.X0, arm64.SP, 0),
	}, exit(1, jit.TrapBridge)...)...)

	require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
	require.Equal(t, uint64(42), ctx.Slot(0))
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
