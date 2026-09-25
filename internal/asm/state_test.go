package asm_test

import (
	"runtime"
	"testing"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/internal/asm/arm64"
)

// linkExit links code that saves LR around a BLR to the exit stub, callable
// as a leaf activation the way State.PC and State.Abandon tests need, and
// reports the entry address and the published code's length.
func linkExit(t *testing.T, body ...asm.Instruction) (uintptr, int) {
	t.Helper()
	insts := []asm.Instruction{
		arm64.SUBI(arm64.SP, arm64.SP, 16),
		arm64.STR(arm64.LR, arm64.SP, 8),
	}
	insts = append(insts, body...)
	insts = append(insts,
		arm64.LDR(arm64.X16, arm64.Ctx, int16(asm.OffsetStub)),
		arm64.BLR(arm64.X16),
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
	address, err := asm.Link(buffer, code)
	require.NoError(t, err)
	return address, len(code)
}

func TestNewState(t *testing.T) {
	t.Run("rejects an empty stack", func(t *testing.T) {
		_, err := asm.NewState(0)
		require.ErrorIs(t, err, asm.ErrInvalidArgs)
	})

	t.Run("starts with an empty register file", func(t *testing.T) {
		s, err := asm.NewState(4096)
		require.NoError(t, err)
		require.Zero(t, s.Reg(asm.NewPReg(0, asm.RegTypeInt, asm.Width64)))
	})
}

func TestState_Reg(t *testing.T) {
	s, err := asm.NewState(4096)
	require.NoError(t, err)

	intReg := asm.NewPReg(3, asm.RegTypeInt, asm.Width64)
	floatReg := asm.NewPReg(3, asm.RegTypeFloat, asm.Width64)
	s.SetReg(intReg, 42)
	s.SetReg(floatReg, 84)

	require.Equal(t, uint64(42), s.Reg(intReg))
	require.Equal(t, uint64(84), s.Reg(floatReg))
}

func TestState_Slot(t *testing.T) {
	s, err := asm.NewState(4096)
	require.NoError(t, err)
	a := asm.New(arm64.New())
	a.Emit(
		arm64.MOVI(arm64.X0, 42),
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
	s.SetReg(arm64.X0, 42)

	require.True(t, asm.Enter(address, &s))
	require.Equal(t, uint64(42), s.Slot(0))
}

func TestState_Word(t *testing.T) {
	t.Run("reads a word of the native stack by address", func(t *testing.T) {
		if runtime.GOARCH != "arm64" {
			t.Skip("native execution needs arm64")
		}
		s, err := asm.NewState(4096)
		require.NoError(t, err)
		address, _ := linkExit(t,
			arm64.MOVI(arm64.X1, 42),
			arm64.STR(arm64.X1, arm64.SP, 0),
			arm64.ADDI(arm64.X0, arm64.SP, 0),
		)

		require.True(t, asm.Enter(address, &s))
		require.Equal(t, uint64(42), s.Word(uintptr(s.Reg(arm64.X0))))
	})

	t.Run("rejects an address outside the native stack", func(t *testing.T) {
		s, err := asm.NewState(4096)
		require.NoError(t, err)
		require.Panics(t, func() { s.Word(8) })
	})
}

func TestState_SetReg(t *testing.T) {
	s, err := asm.NewState(4096)
	require.NoError(t, err)

	reg := asm.NewPReg(3, asm.RegTypeInt, asm.Width64)
	s.SetReg(reg, 42)

	require.Equal(t, uint64(42), s.Reg(reg))
}

func TestState_PC(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("requires arm64")
	}
	s, err := asm.NewState(4096)
	require.NoError(t, err)
	address, size := linkExit(t)

	require.True(t, asm.Enter(address, &s))

	pc := s.PC()
	require.GreaterOrEqual(t, pc, address)
	require.Less(t, pc, address+uintptr(size))
}

func TestState_Abandon(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("requires arm64")
	}
	s, err := asm.NewState(4096)
	require.NoError(t, err)
	probe, _ := linkExit(t, arm64.ADDI(arm64.X0, arm64.SP, 16))

	require.True(t, asm.Enter(probe, &s))
	first := s.Reg(arm64.X0)

	s.Abandon()

	require.True(t, asm.Enter(probe, &s))
	second := s.Reg(arm64.X0)

	require.Equal(t, first, second)
}

func TestState_Exited(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("requires arm64")
	}
	s, err := asm.NewState(4096)
	require.NoError(t, err)
	require.False(t, s.Exited())

	address, _ := linkExit(t)
	require.True(t, asm.Enter(address, &s))
	require.True(t, s.Exited())

	require.False(t, asm.Resume(&s))
	require.False(t, s.Exited())
}
