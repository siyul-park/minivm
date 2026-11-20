package jit_test

import (
	"runtime"
	"slices"
	"testing"
	"unsafe"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/testenv"
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
		arm64.LDR(arm64.X16, arm64.Ctx, int16(jit.OffsetStub)),
		arm64.BLR(arm64.X16),
	}
}

func grow(n int) int {
	var pad [64]byte
	if n == 0 {
		return int(pad[0])
	}
	return grow(n-1) + 1
}

func TestContext_Layout(t *testing.T) {
	tests := []struct {
		name string
		off  uintptr
		want uintptr
	}{
		{"state", unsafe.Offsetof(jit.Context{}.State), 0},
		{"record size", jit.SizeofRecord, 32},
		{"record fb", jit.RecordFB, 0},
		{"record sp", jit.RecordSP, 8},
		{"record pc", jit.RecordPC, 16},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.off)
		})
	}
}

func TestEnter(t *testing.T) {
	t.Run("returns when the code returns", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)

		require.Equal(t, jit.TrapReturn, jit.Enter(link(t), ctx))
	})

	t.Run("reports the trap and exit native code wrote", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		code := link(t, append([]asm.Instruction{arm64.MOVI(arm64.X0, 42)}, exit(7, jit.TrapDeopt)...)...)

		require.Equal(t, jit.TrapDeopt, jit.Enter(code, ctx))
		require.Equal(t, uint64(7), ctx.Exit())
		require.Equal(t, uint64(42), ctx.Reg(arm64.X0))
	})

	t.Run("keeps the native stack pointer aligned", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		code := link(t, append([]asm.Instruction{arm64.ADDI(arm64.X0, arm64.SP, 16)}, exit(1, jit.TrapBridge)...)...)

		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		stack := ctx.Reg(arm64.X0)
		require.NotZero(t, stack)
		require.Zero(t, stack%16)
	})

	t.Run("handles a small stack allocation", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")
		ctx, err := jit.NewContext(8)
		require.NoError(t, err)

		require.Equal(t, jit.TrapReturn, jit.Enter(link(t), ctx))
	})

	t.Run("nests below a suspended activation", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		outer := link(t, slices.Concat(
			[]asm.Instruction{arm64.MOVI(arm64.X0, 1), arm64.STR(arm64.X0, arm64.SP, 0)},
			exit(1, jit.TrapBridge),
			[]asm.Instruction{arm64.LDR(arm64.X0, arm64.SP, 0)},
			exit(2, jit.TrapBridge),
		)...)
		inner := link(t,
			arm64.MOVI(arm64.X1, 2),
			arm64.SUBI(arm64.SP, arm64.SP, 16),
			arm64.STR(arm64.X1, arm64.SP, 0),
			arm64.ADDI(arm64.SP, arm64.SP, 16),
		)

		require.Equal(t, jit.TrapBridge, jit.Enter(outer, ctx))
		require.Equal(t, jit.TrapReturn, jit.Enter(inner, ctx))
		require.Equal(t, jit.TrapBridge, jit.Resume(ctx))
		require.Equal(t, uint64(1), ctx.Reg(arm64.X0))
		require.Equal(t, jit.TrapReturn, jit.Resume(ctx))
	})
}

func TestResume(t *testing.T) {
	t.Run("returns when the resumed code returns", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		code := link(t, exit(1, jit.TrapBridge)...)

		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		require.Equal(t, jit.TrapReturn, jit.Resume(ctx))
	})

	t.Run("reports the next trap", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		code := link(t, append(exit(1, jit.TrapBridge), exit(2, jit.TrapDeopt)...)...)

		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		require.Equal(t, jit.TrapDeopt, jit.Resume(ctx))
		require.Equal(t, uint64(2), ctx.Exit())
	})

	t.Run("continues with the registers Go set", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		code := link(t, slices.Concat(
			[]asm.Instruction{arm64.MOVI(arm64.X0, 42)},
			exit(1, jit.TrapBridge),
			[]asm.Instruction{arm64.ADDI(arm64.X0, arm64.X0, 1)},
			exit(2, jit.TrapBridge),
		)...)

		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		ctx.SetReg(arm64.X0, 100)
		require.Equal(t, jit.TrapBridge, jit.Resume(ctx))
		require.Equal(t, uint64(101), ctx.Reg(arm64.X0))
		require.Equal(t, jit.TrapReturn, jit.Resume(ctx))
	})

	t.Run("restores every saved register", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")
		ctx, err := jit.NewContext(4096)
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
		insts = append(insts, exit(1, jit.TrapBridge)...)
		insts = append(insts, exit(2, jit.TrapBridge)...)
		code := link(t, insts...)

		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		for _, r := range saved {
			require.Equal(t, uint64(r.ID())+1, ctx.Reg(r), r.String())
			ctx.SetReg(r, uint64(r.ID())+1000)
		}
		for i := range 32 {
			require.Equal(t, uint64(i)+100, ctx.Reg(fregs[i]), fregs[i].String())
			ctx.SetReg(fregs[i], uint64(i)+2000)
		}
		require.Equal(t, jit.TrapBridge, jit.Resume(ctx))
		for _, r := range saved {
			require.Equal(t, uint64(r.ID())+1000, ctx.Reg(r), r.String())
		}
		for i := range 32 {
			require.Equal(t, uint64(i)+2000, ctx.Reg(fregs[i]), fregs[i].String())
		}
		require.Equal(t, jit.TrapReturn, jit.Resume(ctx))
	})

	t.Run("survives Go stack growth and collection while suspended", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		code := link(t, slices.Concat(
			[]asm.Instruction{arm64.MOVI(arm64.X0, 3), arm64.STR(arm64.X0, arm64.SP, 0)},
			exit(1, jit.TrapBridge),
			[]asm.Instruction{arm64.LDR(arm64.X1, arm64.SP, 0), arm64.ADD(arm64.X0, arm64.X0, arm64.X1)},
			exit(2, jit.TrapBridge),
		)...)

		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		require.Equal(t, 1<<16, grow(1<<16))
		runtime.GC()
		require.Equal(t, jit.TrapBridge, jit.Resume(ctx))
		require.Equal(t, uint64(6), ctx.Reg(arm64.X0))
		require.Equal(t, jit.TrapReturn, jit.Resume(ctx))
	})

}

func TestNewContext(t *testing.T) {
	t.Run("rejects an empty stack", func(t *testing.T) {
		_, err := jit.NewContext(0)
		require.ErrorIs(t, err, asm.ErrInvalidArgs)
	})

	t.Run("starts with no exit taken", func(t *testing.T) {
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		require.Zero(t, ctx.Exit())
	})

	t.Run("starts with an empty register file", func(t *testing.T) {
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		require.Zero(t, ctx.Reg(asm.NewPReg(0, asm.RegTypeInt, asm.Width64)))
	})

	t.Run("names the exit stub", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		code := link(t, append([]asm.Instruction{arm64.LDR(arm64.X0, arm64.Ctx, int16(jit.OffsetStub))}, exit(1, jit.TrapBridge)...)...)

		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		require.NotZero(t, ctx.Reg(arm64.X0))
	})
}

func TestContext_Level(t *testing.T) {
	t.Run("counts the records below Next", func(t *testing.T) {
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		require.Zero(t, ctx.Level())

		ctx.Next += 3 * jit.SizeofRecord
		require.Equal(t, 3, ctx.Level())
	})
}

func TestContext_SetLevel(t *testing.T) {
	t.Run("points Next at the record after n activations", func(t *testing.T) {
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)

		ctx.SetLevel(2)
		require.Equal(t, uintptr(unsafe.Pointer(&ctx.Records[2])), ctx.Next)
		require.Equal(t, 2, ctx.Level())
	})
}

func TestContext_SetEdge(t *testing.T) {
	t.Run("points Edge at the record n activations in", func(t *testing.T) {
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)

		ctx.SetEdge(len(ctx.Records))
		require.Equal(t, uintptr(unsafe.Pointer(&ctx.Records[0]))+uintptr(len(ctx.Records))*jit.SizeofRecord, ctx.Edge)
	})
}

func TestContext_Trap(t *testing.T) {
	testenv.MustHaveArch(t, "arm64")
	ctx, err := jit.NewContext(4096)
	require.NoError(t, err)
	require.Equal(t, jit.TrapReturn, ctx.Trap())

	require.Equal(t, jit.TrapBridge, jit.Enter(link(t, exit(1, jit.TrapBridge)...), ctx))
	require.Equal(t, jit.TrapBridge, ctx.Trap())
}

func TestContext_Read(t *testing.T) {
	t.Run("innermost register value reads the saved register file", func(t *testing.T) {
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		ctx.SetLevel(1)
		ctx.SetReg(arm64.X0, 42)

		got := ctx.Read(0, jit.Value{Loc: asm.Loc{Reg: arm64.X0}})
		require.Equal(t, uint64(42), got)
	})

	t.Run("outer activation reads the spill slot at the next record's SP", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		a := asm.New(arm64.New())
		a.Emit(
			arm64.SUBI(arm64.SP, arm64.SP, 32),
			arm64.MOVI(arm64.X1, 99),
			arm64.STR(arm64.X1, arm64.SP, 16),
			arm64.ADDI(arm64.X0, arm64.SP, 0),
			arm64.LDR(arm64.X16, arm64.Ctx, int16(jit.OffsetStub)),
			arm64.BLR(arm64.X16),
		)
		code, err := a.Build()
		require.NoError(t, err)
		buffer, err := asm.NewBuffer(arm64.New(), len(code))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, buffer.Free()) })
		address, err := buffer.Link(code)
		require.NoError(t, err)

		jit.Enter(address, ctx)
		require.True(t, ctx.Exited())
		ctx.SetLevel(2)
		ctx.Records[1].SP = uintptr(ctx.Reg(arm64.X0))

		got := ctx.Read(0, jit.Value{Loc: asm.Loc{Spilled: true, Slot: 2}})
		require.Equal(t, uint64(99), got)
	})

	t.Run("outer activation with a register location is a programmer error", func(t *testing.T) {
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		ctx.SetLevel(2)

		require.Panics(t, func() { ctx.Read(0, jit.Value{Loc: asm.Loc{Reg: arm64.X0}}) })
	})

	t.Run("innermost spilled value reads the suspended native stack slot", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		a := asm.New(arm64.New())
		a.Emit(
			arm64.MOVI(arm64.X0, 7),
			arm64.STR(arm64.X0, arm64.SP, 0),
			arm64.LDR(arm64.X16, arm64.Ctx, int16(jit.OffsetStub)),
			arm64.BLR(arm64.X16),
		)
		code, err := a.Build()
		require.NoError(t, err)
		buffer, err := asm.NewBuffer(arm64.New(), len(code))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, buffer.Free()) })
		address, err := buffer.Link(code)
		require.NoError(t, err)
		ctx.SetLevel(1)

		jit.Enter(address, ctx)
		require.True(t, ctx.Exited())

		got := ctx.Read(0, jit.Value{Loc: asm.Loc{Spilled: true, Slot: 0}})
		require.Equal(t, uint64(7), got)
	})
}

func TestTrap_String(t *testing.T) {
	tests := []struct {
		trap jit.Trap
		want string
	}{
		{jit.TrapReturn, "return"},
		{jit.TrapDeopt, "deopt"},
		{jit.TrapBridge, "bridge"},
		{jit.Trap(99), "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			require.Equal(t, tt.want, tt.trap.String())
		})
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
	buffer, err := asm.NewBuffer(arm64.New(), len(code))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, buffer.Free()) })
	addr, err := buffer.Link(code)
	require.NoError(t, err)
	return addr
}
