package asm_test

import (
	"runtime"
	"testing"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/stretchr/testify/require"
)

// frame wraps body in the frame any non-leaf activation owns: an exit is a
// call, so LR survives it only if the code keeps it on the stack.
func frame(body ...asm.Instruction) []asm.Instruction {
	insts := []asm.Instruction{
		arm64.SUBI(arm64.SP, arm64.SP, 16),
		arm64.STR(arm64.LR, arm64.SP, 8),
	}
	insts = append(insts, body...)
	return append(insts,
		arm64.LDR(arm64.LR, arm64.SP, 8),
		arm64.ADDI(arm64.SP, arm64.SP, 16),
		arm64.RET(),
	)
}

// exit is the native side of the exit protocol: write the exit id and the
// trap, then call the stub the context names.
func exit(id int64, trap asm.Trap) []asm.Instruction {
	return []asm.Instruction{
		arm64.MOVI(arm64.X16, id),
		arm64.STR(arm64.X16, arm64.Ctx, int16(asm.OffsetExit)),
		arm64.MOVI(arm64.X16, int64(trap)),
		arm64.STR(arm64.X16, arm64.Ctx, int16(asm.OffsetTrap)),
		arm64.LDR(arm64.X16, arm64.Ctx, int16(asm.OffsetStub)),
		arm64.BLR(arm64.X16),
	}
}

// reg is the offset of Xi's slot in Context.regs.
func reg(i int) int16 { return int16(asm.OffsetRegs) + int16(i)*8 }

func link(t *testing.T, insts ...asm.Instruction) uintptr {
	t.Helper()
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
		ctx, err := asm.NewContext(4096)
		require.NoError(t, err)
		code := link(t, arm64.RET())

		require.Equal(t, asm.TrapReturn, asm.Enter(code, ctx))
	})

	t.Run("keeps the native stack pointer aligned", func(t *testing.T) {
		ctx, err := asm.NewContext(4096)
		require.NoError(t, err)
		code := link(t,
			arm64.ADDI(arm64.X0, arm64.SP, 0),
			arm64.STR(arm64.X0, arm64.Ctx, reg(0)),
			arm64.RET(),
		)

		require.Equal(t, asm.TrapReturn, asm.Enter(code, ctx))
		stack := ctx.Reg(arm64.X0)
		require.NotZero(t, stack)
		require.Zero(t, stack%16)
	})

	t.Run("handles a small stack allocation", func(t *testing.T) {
		ctx, err := asm.NewContext(8)
		require.NoError(t, err)

		require.Equal(t, asm.TrapReturn, asm.Enter(link(t, frame()...), ctx))
	})

	t.Run("suspends at an exit", func(t *testing.T) {
		ctx, err := asm.NewContext(4096)
		require.NoError(t, err)
		code := link(t, frame(append([]asm.Instruction{arm64.MOVI(arm64.X0, 42)}, exit(7, asm.TrapBridge)...)...)...)

		require.Equal(t, asm.TrapBridge, asm.Enter(code, ctx))
		require.Equal(t, uint64(7), ctx.Exit())
		require.Equal(t, uint64(42), ctx.Reg(arm64.X0))
	})

	t.Run("nests below a suspended activation", func(t *testing.T) {
		ctx, err := asm.NewContext(4096)
		require.NoError(t, err)
		outer := link(t, frame(append(append([]asm.Instruction{
			arm64.MOVI(arm64.X0, 1),
			arm64.STR(arm64.X0, arm64.SP, 0),
		}, exit(1, asm.TrapBridge)...),
			arm64.LDR(arm64.X0, arm64.SP, 0),
			arm64.STR(arm64.X0, arm64.Ctx, reg(0)),
		)...)...)
		inner := link(t,
			arm64.MOVI(arm64.X1, 2),
			arm64.SUBI(arm64.SP, arm64.SP, 16),
			arm64.STR(arm64.X1, arm64.SP, 0),
			arm64.ADDI(arm64.SP, arm64.SP, 16),
			arm64.RET(),
		)

		require.Equal(t, asm.TrapBridge, asm.Enter(outer, ctx))
		require.Equal(t, asm.TrapReturn, asm.Enter(inner, ctx))
		require.Equal(t, asm.TrapReturn, asm.Resume(ctx))
		require.Equal(t, uint64(1), ctx.Reg(arm64.X0))
	})
}

func TestResume(t *testing.T) {
	t.Run("continues with the registers Go set", func(t *testing.T) {
		ctx, err := asm.NewContext(4096)
		require.NoError(t, err)
		code := link(t, frame(append(append([]asm.Instruction{arm64.MOVI(arm64.X0, 42)}, exit(1, asm.TrapBridge)...),
			arm64.ADDI(arm64.X0, arm64.X0, 1),
			arm64.STR(arm64.X0, arm64.Ctx, reg(0)),
		)...)...)

		require.Equal(t, asm.TrapBridge, asm.Enter(code, ctx))
		ctx.SetReg(arm64.X0, 100)
		require.Equal(t, asm.TrapReturn, asm.Resume(ctx))
		require.Equal(t, uint64(101), ctx.Reg(arm64.X0))
	})

	t.Run("restores every saved register", func(t *testing.T) {
		ctx, err := asm.NewContext(4096)
		require.NoError(t, err)
		fregs := arm64.New().Registers(asm.RegTypeFloat)
		require.Len(t, fregs, 32)
		saved := []asm.PReg{
			arm64.X0, arm64.X1, arm64.X2, arm64.X3, arm64.X4, arm64.X5, arm64.X6, arm64.X7,
			arm64.X8, arm64.X9, arm64.X10, arm64.X11, arm64.X12, arm64.X13, arm64.X14, arm64.X15,
			arm64.X19, arm64.X20, arm64.X21, arm64.X22, arm64.X23, arm64.X24, arm64.X25, arm64.X27, arm64.X29,
		}
		var insts []asm.Instruction
		for _, r := range saved {
			insts = append(insts, arm64.MOVI(r, int64(r.ID())+1))
		}
		for i := range 32 {
			insts = append(insts, arm64.MOVI(arm64.X16, int64(i)+100), arm64.FMOV(fregs[i], arm64.X16))
		}
		insts = append(insts, exit(1, asm.TrapBridge)...)
		insts = append(insts, exit(2, asm.TrapBridge)...)
		code := link(t, frame(insts...)...)

		require.Equal(t, asm.TrapBridge, asm.Enter(code, ctx))
		for _, r := range saved {
			require.Equal(t, uint64(r.ID())+1, ctx.Reg(r), r.String())
			ctx.SetReg(r, uint64(r.ID())+1000)
		}
		for i := range 32 {
			require.Equal(t, uint64(i)+100, ctx.Reg(fregs[i]), fregs[i].String())
			ctx.SetReg(fregs[i], uint64(i)+2000)
		}
		require.Equal(t, asm.TrapBridge, asm.Resume(ctx))
		require.Equal(t, uint64(2), ctx.Exit())
		for _, r := range saved {
			require.Equal(t, uint64(r.ID())+1000, ctx.Reg(r), r.String())
		}
		for i := range 32 {
			require.Equal(t, uint64(i)+2000, ctx.Reg(fregs[i]), fregs[i].String())
		}
		require.Equal(t, asm.TrapReturn, asm.Resume(ctx))
	})

	t.Run("survives Go stack growth and collection while suspended", func(t *testing.T) {
		ctx, err := asm.NewContext(4096)
		require.NoError(t, err)
		code := link(t, frame(append(append([]asm.Instruction{
			arm64.MOVI(arm64.X0, 3),
			arm64.STR(arm64.X0, arm64.SP, 0),
		}, exit(1, asm.TrapBridge)...),
			arm64.LDR(arm64.X1, arm64.SP, 0),
			arm64.ADD(arm64.X0, arm64.X0, arm64.X1),
			arm64.STR(arm64.X0, arm64.Ctx, reg(0)),
		)...)...)

		require.Equal(t, asm.TrapBridge, asm.Enter(code, ctx))
		require.Equal(t, 1<<16, grow(1<<16))
		runtime.GC()
		require.Equal(t, asm.TrapReturn, asm.Resume(ctx))
		require.Equal(t, uint64(6), ctx.Reg(arm64.X0))
	})
}

func TestContext_Exit(t *testing.T) {
	ctx, err := asm.NewContext(4096)
	require.NoError(t, err)
	code := link(t, frame(exit(7, asm.TrapBridge)...)...)

	require.Equal(t, asm.TrapBridge, asm.Enter(code, ctx))
	require.Equal(t, uint64(7), ctx.Exit())
}

func grow(n int) int {
	var pad [64]byte
	if n == 0 {
		return int(pad[0])
	}
	return grow(n-1) + 1
}
