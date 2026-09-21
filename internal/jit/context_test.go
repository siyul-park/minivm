package jit_test

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/stretchr/testify/require"
)

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
}

func TestContext_Layout(t *testing.T) {
	tests := []struct {
		name string
		off  uintptr
		want uintptr
	}{
		{"state", unsafe.Offsetof(jit.Context{}.State), 0},
		{"stack", jit.OffsetStack, unsafe.Offsetof(jit.Context{}.Stack)},
		{"globals", jit.OffsetGlobals, unsafe.Offsetof(jit.Context{}.Globals)},
		{"rc", jit.OffsetRC, unsafe.Offsetof(jit.Context{}.RC)},
		{"natives", jit.OffsetNatives, unsafe.Offsetof(jit.Context{}.Natives)},
		{"top", jit.OffsetTop, unsafe.Offsetof(jit.Context{}.Top)},
		{"fb", jit.OffsetFB, unsafe.Offsetof(jit.Context{}.FB)},
		{"depth", jit.OffsetDepth, unsafe.Offsetof(jit.Context{}.Depth)},
		{"limit", jit.OffsetLimit, unsafe.Offsetof(jit.Context{}.Limit)},
		{"budget", jit.OffsetBudget, unsafe.Offsetof(jit.Context{}.Budget)},
		{"results", jit.OffsetResults, unsafe.Offsetof(jit.Context{}.Results)},
		{"records", jit.OffsetRecords, unsafe.Offsetof(jit.Context{}.Records)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.off)
		})
	}
	require.Equal(t, uintptr(32), unsafe.Sizeof(jit.Record{}))
	require.Equal(t, uintptr(0), jit.RecordFB)
	require.Equal(t, uintptr(8), jit.RecordSP)
	require.Equal(t, uintptr(16), jit.RecordPC)
	require.Equal(t, uintptr(24), jit.RecordExit)
}

func TestContext_Read(t *testing.T) {
	t.Run("innermost register value reads the saved register file", func(t *testing.T) {
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		ctx.Depth = 1
		ctx.SetReg(arm64.X0, 42)

		got := ctx.Read(0, jit.Value{Loc: asm.Loc{Reg: arm64.X0}})
		require.Equal(t, uint64(42), got)
	})

	t.Run("outer activation reads the spill slot at the record's SP", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("requires arm64")
		}
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		a := asm.New(arm64.New())
		a.Emit(
			arm64.SUBI(arm64.SP, arm64.SP, 32),
			arm64.MOVI(arm64.X1, 99),
			arm64.STR(arm64.X1, arm64.SP, 16),
			arm64.ADDI(arm64.X0, arm64.SP, 0),
			arm64.LDR(arm64.X16, arm64.Ctx, int16(asm.OffsetStub)),
			arm64.BLR(arm64.X16),
		)
		code, err := a.Build()
		require.NoError(t, err)
		buffer, err := asm.NewBuffer(len(code))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, buffer.Free()) })
		address, err := asm.Link(buffer, code)
		require.NoError(t, err)

		require.True(t, asm.Enter(address, &ctx.State))
		ctx.Depth = 2
		ctx.Records[0].SP = uintptr(ctx.Reg(arm64.X0))

		got := ctx.Read(0, jit.Value{Loc: asm.Loc{Spilled: true, Slot: 2}})
		require.Equal(t, uint64(99), got)
	})

	t.Run("outer activation with a register location is a programmer error", func(t *testing.T) {
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		ctx.Depth = 2

		require.Panics(t, func() { ctx.Read(0, jit.Value{Loc: asm.Loc{Reg: arm64.X0}}) })
	})

	t.Run("innermost spilled value reads the suspended native stack slot", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("requires arm64")
		}
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		a := asm.New(arm64.New())
		a.Emit(
			arm64.MOVI(arm64.X0, 7),
			arm64.STR(arm64.X0, arm64.SP, 0),
			arm64.LDR(arm64.X16, arm64.Ctx, int16(asm.OffsetStub)),
			arm64.BLR(arm64.X16),
		)
		code, err := a.Build()
		require.NoError(t, err)
		buffer, err := asm.NewBuffer(len(code))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, buffer.Free()) })
		address, err := asm.Link(buffer, code)
		require.NoError(t, err)
		ctx.Depth = 1

		require.True(t, asm.Enter(address, &ctx.State))

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
