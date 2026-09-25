package jit_test

import (
	"testing"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/stretchr/testify/require"
)

// exit is the native side of the protocol: write the exit id and the trap
// into the context, then call the stub the state names.
func exit(id int64, trap jit.Trap) []asm.Instruction {
	return []asm.Instruction{
		arm64.MOVI(arm64.X16, id),
		arm64.STR(arm64.X16, arm64.Ctx, int16(jit.OffsetExit)),
		arm64.MOVI(arm64.X16, int64(trap)),
		arm64.STR(arm64.X16, arm64.Ctx, int16(jit.OffsetTrap)),
		arm64.LDR(arm64.X16, arm64.Ctx, int16(asm.OffsetStub)),
		arm64.BLR(arm64.X16),
	}
}

// link publishes body inside the frame any exiting activation owns.
func link(t *testing.T, body ...asm.Instruction) uintptr {
	t.Helper()
	insts := []asm.Instruction{
		arm64.SUBI(arm64.SP, arm64.SP, 16),
		arm64.STR(arm64.LR, arm64.SP, 8),
	}
	insts = append(insts, body...)
	insts = append(insts,
		arm64.LDR(arm64.LR, arm64.SP, 8),
		arm64.ADDI(arm64.SP, arm64.SP, 16),
		arm64.RET(),
	)
	a := asm.New(arm64.New())
	a.Emit(insts...)
	code, err := a.Build()
	require.NoError(t, err)
	buffer, err := asm.NewBuffer(len(code))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, buffer.Free()) })
	addr, err := asm.Link(buffer, code)
	require.NoError(t, err)
	return addr
}

func TestEnter(t *testing.T) {
	t.Run("returns when the code returns", func(t *testing.T) {
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)

		require.Equal(t, jit.TrapReturn, jit.Enter(link(t), ctx))
	})

	t.Run("reports the trap and exit native code wrote", func(t *testing.T) {
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		code := link(t, append([]asm.Instruction{arm64.MOVI(arm64.X0, 42)}, exit(7, jit.TrapDeopt)...)...)

		require.Equal(t, jit.TrapDeopt, jit.Enter(code, ctx))
		require.Equal(t, uint64(7), ctx.Exit())
		require.Equal(t, uint64(42), ctx.Reg(arm64.X0))
	})
}

func TestResume(t *testing.T) {
	t.Run("returns when the resumed code returns", func(t *testing.T) {
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		code := link(t, exit(1, jit.TrapBridge)...)

		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		require.Equal(t, jit.TrapReturn, jit.Resume(ctx))
	})

	t.Run("reports the next trap", func(t *testing.T) {
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		code := link(t, append(exit(1, jit.TrapBridge), exit(2, jit.TrapDeopt)...)...)

		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		require.Equal(t, jit.TrapDeopt, jit.Resume(ctx))
		require.Equal(t, uint64(2), ctx.Exit())
	})
}

func TestContext_Trap(t *testing.T) {
	ctx, err := jit.NewContext(4096)
	require.NoError(t, err)
	require.Equal(t, jit.TrapReturn, ctx.Trap())

	require.Equal(t, jit.TrapBridge, jit.Enter(link(t, exit(1, jit.TrapBridge)...), ctx))
	require.Equal(t, jit.TrapBridge, ctx.Trap())
}
