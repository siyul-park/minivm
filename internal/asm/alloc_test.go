package asm_test

import (
	"testing"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/stretchr/testify/require"
)

// narrow is an ARM64 frame whose integer bank holds one register.
type narrow struct {
	asm.Arch
	asm.Frame
}

func (narrow) Registers(typ asm.RegType) []asm.PReg {
	if typ == asm.RegTypeFloat {
		return arm64.New().Registers(typ)
	}
	return []asm.PReg{arm64.X0}
}

// vint and vfloat name the virtual registers a case allocates.
func vint(id int32) asm.VReg   { return asm.NewVReg(id, asm.RegTypeInt, asm.Width64) }
func vfloat(id int32) asm.VReg { return asm.NewVReg(id, asm.RegTypeFloat, asm.Width64) }

// slots is a prologue row that reserves the spill area Build sizes.
func slots(op arm64.Op) asm.Instruction {
	return asm.Instruction{Op: uint16(op), Dst: asm.Physical(arm64.SP), Src1: asm.Physical(arm64.SP), Src2: asm.Slots()}
}

// encode spells an expected stream: the physical rows a case states, encoded
// by the encoder whose goldens live in the arm64 package.
func encode(t *testing.T, insts ...asm.Instruction) []byte {
	t.Helper()
	a := asm.New(arm64.New())
	a.Emit(insts...)
	code, err := a.Build()
	require.NoError(t, err)
	return code
}

func TestAssembler_Rows(t *testing.T) {
	a := asm.New(arm64.New())
	want := []asm.Instruction{arm64.MOVI(arm64.X0, 1), arm64.MOVI(arm64.X1, 2), arm64.ADD(arm64.X2, arm64.X0, arm64.X1)}
	a.Emit(want...)
	require.Equal(t, want, a.Rows())
	_, err := a.Build()
	require.NoError(t, err)
	require.Equal(t, want, a.Rows())
}

func TestAssembler_Reserve(t *testing.T) {
	type pair struct {
		asm.Arch
		asm.Frame
	}
	frame := pair{Arch: arm64.New(), Frame: arm64.New()}
	a := asm.New(frame)
	a.Emit(
		arm64.MOVI(vint(0), 1),
		arm64.MOVI(vint(1), 2),
		arm64.ADD(vint(2), vint(0), vint(1)),
		arm64.STR(vint(2), arm64.Ctx, 0),
	)
	a.Reserve(arm64.X0)

	_, err := a.Build()
	require.NoError(t, err)
	loc, ok := a.Loc(vint(0))
	require.True(t, ok)
	require.NotEqual(t, arm64.X0, loc.Reg)
}

func TestAssembler_ReserveSlots(t *testing.T) {
	a := asm.New(arm64.New())
	a.ReserveSlots(2)
	a.Emit(
		arm64.MOVI(vint(0), 1),
		arm64.BLR(arm64.X1),
		arm64.STR(vint(0), arm64.SP, 16),
		arm64.RET(),
	)

	_, err := a.Build()
	require.NoError(t, err)
	loc, ok := a.Loc(vint(0))
	require.True(t, ok)
	require.Equal(t, asm.Loc{Slot: 2, Spilled: true}, loc)
}

func TestAssembler_Loc(t *testing.T) {
	a := asm.New(arm64.New())
	a.Emit(
		slots(arm64.OpSUBI),
		arm64.MOVI(vint(0), 1),
		arm64.MOVI(vint(1), 2),
		arm64.BLR(arm64.X1),
		arm64.ADD(vint(2), vint(0), vint(1)),
		arm64.STR(vint(2), arm64.Ctx, 0),
		slots(arm64.OpADDI),
		arm64.RET(),
	)
	_, ok := a.Loc(vint(0))
	require.False(t, ok)

	_, err := a.Build()
	require.NoError(t, err)

	tests := []struct {
		name string
		reg  asm.VReg
		want asm.Loc
		ok   bool
	}{
		{"spilled", vint(0), asm.Loc{Slot: 0, Spilled: true}, true},
		{"spilled next slot", vint(1), asm.Loc{Slot: 1, Spilled: true}, true},
		{"assigned", vint(2), asm.Loc{Reg: arm64.X2}, true},
		{"unknown", vint(3), asm.Loc{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loc, ok := a.Loc(tt.reg)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, loc)
		})
	}
}
