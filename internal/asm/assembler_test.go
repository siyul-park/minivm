package asm_test

import (
	"testing"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	require.NotNil(t, asm.New(arm64.New()))
}

func TestAssembler_Label(t *testing.T) {
	assembler := asm.New(arm64.New())
	target := assembler.Label()
	assembler.Emit(arm64.BLabel(target), arm64.RET())
	assembler.Bind(target)
	assembler.Emit(arm64.RET())

	code, err := assembler.Build()
	require.NoError(t, err)
	require.Equal(t, []byte{0x02, 0x00, 0x00, 0x14}, code[:4])
}

func TestAssembler_Emit(t *testing.T) {
	assembler := asm.New(arm64.New())
	assembler.Emit(arm64.MOV(arm64.X0, arm64.X1), arm64.RET())

	code, err := assembler.Build()
	require.NoError(t, err)
	require.NotEmpty(t, code)
}

func TestAssembler_Build(t *testing.T) {
	t.Run("unresolved label", func(t *testing.T) {
		assembler := asm.New(arm64.New())
		assembler.Emit(arm64.BLabel(assembler.Label()))

		_, err := assembler.Build()
		require.ErrorIs(t, err, asm.ErrUnresolvedLabel)
	})
	t.Run("backward branch", func(t *testing.T) {
		assembler := asm.New(arm64.New())
		head := assembler.Label()
		assembler.Bind(head)
		assembler.Emit(arm64.NOP(), arm64.BLLabel(head))

		code, err := assembler.Build()
		require.NoError(t, err)
		require.Equal(t, []byte{0xff, 0xff, 0xff, 0x97}, code[4:8])
	})
	t.Run("relaxes conditional branch", func(t *testing.T) {
		assembler := asm.New(arm64.New())
		target := assembler.Label()
		assembler.Emit(arm64.CBZLabel(arm64.X0, target))
		for range 1 << 18 {
			assembler.Emit(arm64.ADDI(arm64.X1, arm64.X1, 1))
		}
		assembler.Bind(target)
		assembler.Emit(arm64.RET())

		code, err := assembler.Build()
		require.NoError(t, err)
		require.Greater(t, len(code), 1<<20)
	})
	t.Run("straight line", func(t *testing.T) {
		a := asm.New(arm64.New())
		a.Emit(
			slots(arm64.OpSUBI),
			arm64.MOVI(vint(0), 1),
			arm64.ADDI(vint(1), vint(0), 2),
			arm64.STR(vint(1), arm64.Ctx, 8),
			slots(arm64.OpADDI),
			arm64.RET(),
		)

		code, err := a.Build()
		require.NoError(t, err)
		require.Equal(t, encode(t,
			arm64.SUBI(arm64.SP, arm64.SP, 0),
			arm64.MOVI(arm64.X0, 1),
			arm64.ADDI(arm64.X1, arm64.X0, 2),
			arm64.STR(arm64.X1, arm64.Ctx, 8),
			arm64.ADDI(arm64.SP, arm64.SP, 0),
			arm64.RET(),
		), code)
	})

	t.Run("reuses a register after its value dies", func(t *testing.T) {
		a := asm.New(arm64.New())
		a.Emit(
			arm64.MOVI(vint(0), 1),
			arm64.STR(vint(0), arm64.Ctx, 0),
			arm64.MOVI(vint(1), 2),
			arm64.STR(vint(1), arm64.Ctx, 8),
			arm64.RET(),
		)

		code, err := a.Build()
		require.NoError(t, err)
		require.Equal(t, encode(t,
			arm64.MOVI(arm64.X0, 1),
			arm64.STR(arm64.X0, arm64.Ctx, 0),
			arm64.MOVI(arm64.X0, 2),
			arm64.STR(arm64.X0, arm64.Ctx, 8),
			arm64.RET(),
		), code)
	})

	t.Run("spills the value that lives longest under pressure in one arm", func(t *testing.T) {
		regs := arm64.New().Registers(asm.RegTypeInt)
		a := asm.New(arm64.New())
		other, join := a.Label(), a.Label()
		a.Emit(slots(arm64.OpSUBI), arm64.MOVI(vint(0), 7), arm64.CBZLabel(arm64.X1, other))
		for i := range int32(len(regs)) {
			a.Emit(arm64.MOVI(vint(1+i), int64(i)))
		}
		for i := int32(len(regs)) - 1; i >= 0; i-- {
			a.Emit(arm64.STR(vint(1+i), arm64.Ctx, int16(8*i)))
		}
		a.Emit(arm64.STR(vint(0), arm64.Ctx, 200), arm64.BLabel(join))
		a.Bind(other)
		a.Emit(arm64.STR(vint(0), arm64.Ctx, 208))
		a.Bind(join)
		a.Emit(arm64.STR(vint(0), arm64.Ctx, 216), slots(arm64.OpADDI), arm64.RET())

		code, err := a.Build()
		require.NoError(t, err)
		want := []asm.Instruction{
			arm64.SUBI(arm64.SP, arm64.SP, 16),
			arm64.MOVI(arm64.X0, 7),
			arm64.STR(arm64.X0, arm64.SP, 0),
			arm64.CBZ(arm64.X1, int32(4*(2*len(regs)+4))),
		}
		for i, r := range regs {
			want = append(want, arm64.MOVI(r, int64(i)))
		}
		for i := len(regs) - 1; i >= 0; i-- {
			want = append(want, arm64.STR(regs[i], arm64.Ctx, int16(8*i)))
		}
		want = append(want,
			arm64.LDR(arm64.X0, arm64.SP, 0),
			arm64.STR(arm64.X0, arm64.Ctx, 200),
			arm64.B(3*4),
			arm64.LDR(arm64.X0, arm64.SP, 0),
			arm64.STR(arm64.X0, arm64.Ctx, 208),
			arm64.LDR(arm64.X0, arm64.SP, 0),
			arm64.STR(arm64.X0, arm64.Ctx, 216),
			arm64.ADDI(arm64.SP, arm64.SP, 16),
			arm64.RET(),
		)
		require.Equal(t, encode(t, want...), code)
	})

	t.Run("keeps a loop-carried value in one register", func(t *testing.T) {
		a := asm.New(arm64.New())
		loop := a.Label()
		a.Emit(arm64.MOVI(vint(0), 0))
		a.Bind(loop)
		a.Emit(
			arm64.ADDI(vint(0), vint(0), 1),
			arm64.CMPI(vint(0), 10),
			arm64.BCondLabel(arm64.OpBNE, loop),
			arm64.STR(vint(0), arm64.Ctx, 0),
			arm64.RET(),
		)

		code, err := a.Build()
		require.NoError(t, err)
		require.Equal(t, encode(t,
			arm64.MOVI(arm64.X0, 0),
			arm64.ADDI(arm64.X0, arm64.X0, 1),
			arm64.CMPI(arm64.X0, 10),
			arm64.BNE(-8),
			arm64.STR(arm64.X0, arm64.Ctx, 0),
			arm64.RET(),
		), code)
	})

	t.Run("spills a value live across a call", func(t *testing.T) {
		a := asm.New(arm64.New())
		a.Emit(
			slots(arm64.OpSUBI),
			arm64.MOVI(vint(0), 5),
			arm64.BLR(arm64.X1),
			arm64.STR(vint(0), arm64.Ctx, 0),
			slots(arm64.OpADDI),
			arm64.RET(),
		)

		code, err := a.Build()
		require.NoError(t, err)
		require.Equal(t, encode(t,
			arm64.SUBI(arm64.SP, arm64.SP, 16),
			arm64.MOVI(arm64.X0, 5),
			arm64.STR(arm64.X0, arm64.SP, 0),
			arm64.BLR(arm64.X1),
			arm64.LDR(arm64.X0, arm64.SP, 0),
			arm64.STR(arm64.X0, arm64.Ctx, 0),
			arm64.ADDI(arm64.SP, arm64.SP, 16),
			arm64.RET(),
		), code)
	})

	t.Run("reloads and parks one register for a row that reads and writes a spilled value", func(t *testing.T) {
		a := asm.New(arm64.New())
		a.Emit(
			slots(arm64.OpSUBI),
			arm64.MOVZ(vint(0), 5, 0),
			arm64.MOVK(vint(0), 1, 48),
			arm64.BLR(arm64.X1),
			arm64.STR(vint(0), arm64.Ctx, 0),
			slots(arm64.OpADDI),
			arm64.RET(),
		)

		code, err := a.Build()
		require.NoError(t, err)
		require.Equal(t, encode(t,
			arm64.SUBI(arm64.SP, arm64.SP, 16),
			arm64.MOVZ(arm64.X0, 5, 0),
			arm64.STR(arm64.X0, arm64.SP, 0),
			arm64.LDR(arm64.X0, arm64.SP, 0),
			arm64.MOVK(arm64.X0, 1, 48),
			arm64.STR(arm64.X0, arm64.SP, 0),
			arm64.BLR(arm64.X1),
			arm64.LDR(arm64.X0, arm64.SP, 0),
			arm64.STR(arm64.X0, arm64.Ctx, 0),
			arm64.ADDI(arm64.SP, arm64.SP, 16),
			arm64.RET(),
		), code)
	})

	t.Run("spills a float live across a call in its own bank", func(t *testing.T) {
		a := asm.New(arm64.New())
		a.Emit(
			arm64.FMOV(vfloat(0), arm64.X2),
			arm64.BLR(arm64.X1),
			arm64.FMOV(arm64.X0, vfloat(0)),
			arm64.STR(arm64.X0, arm64.Ctx, 0),
			arm64.RET(),
		)

		code, err := a.Build()
		require.NoError(t, err)
		require.Equal(t, encode(t,
			arm64.FMOV(arm64.D0, arm64.X2),
			arm64.STR(arm64.D0, arm64.SP, 0),
			arm64.BLR(arm64.X1),
			arm64.LDR(arm64.D0, arm64.SP, 0),
			arm64.FMOV(arm64.X0, arm64.D0),
			arm64.STR(arm64.X0, arm64.Ctx, 0),
			arm64.RET(),
		), code)
	})

	t.Run("keeps a physical register its rows write", func(t *testing.T) {
		a := asm.New(arm64.New())
		a.Emit(
			arm64.MOVI(vint(0), 1),
			arm64.MOVI(arm64.X0, 2),
			arm64.STR(arm64.X0, arm64.Ctx, 0),
			arm64.STR(vint(0), arm64.Ctx, 8),
			arm64.RET(),
		)

		code, err := a.Build()
		require.NoError(t, err)
		require.Equal(t, encode(t,
			arm64.MOVI(arm64.X1, 1),
			arm64.MOVI(arm64.X0, 2),
			arm64.STR(arm64.X0, arm64.Ctx, 0),
			arm64.STR(arm64.X1, arm64.Ctx, 8),
			arm64.RET(),
		), code)
	})

	t.Run("allocates each bank on its own", func(t *testing.T) {
		a := asm.New(arm64.New())
		a.Emit(
			arm64.MOVI(vint(0), 1),
			arm64.FMOV(vfloat(0), arm64.X2),
			arm64.STR(vint(0), arm64.Ctx, 0),
			arm64.STR(vfloat(0), arm64.Ctx, 8),
			arm64.RET(),
		)

		code, err := a.Build()
		require.NoError(t, err)
		require.Equal(t, encode(t,
			arm64.MOVI(arm64.X0, 1),
			arm64.FMOV(arm64.D0, arm64.X2),
			arm64.STR(arm64.X0, arm64.Ctx, 0),
			arm64.STR(arm64.D0, arm64.Ctx, 8),
			arm64.RET(),
		), code)
	})

	t.Run("narrows the register to a 32-bit value", func(t *testing.T) {
		a := asm.New(arm64.New())
		w := asm.NewVReg(0, asm.RegTypeInt, asm.Width32)
		a.Emit(
			arm64.MOVI(w, 1),
			arm64.BLR(arm64.X1),
			arm64.STRW(w, arm64.Ctx, 0),
			arm64.RET(),
		)

		code, err := a.Build()
		require.NoError(t, err)
		require.Equal(t, encode(t,
			arm64.MOVI(arm64.W0, 1),
			arm64.STRW(arm64.W0, arm64.SP, 0),
			arm64.BLR(arm64.X1),
			arm64.LDR(arm64.W0, arm64.SP, 0),
			arm64.STRW(arm64.W0, arm64.Ctx, 0),
			arm64.RET(),
		), code)
	})

	t.Run("sizes an empty spill area without virtual registers", func(t *testing.T) {
		a := asm.New(arm64.New())
		a.Emit(slots(arm64.OpSUBI), arm64.RET())

		code, err := a.Build()
		require.NoError(t, err)
		require.Equal(t, encode(t, arm64.SUBI(arm64.SP, arm64.SP, 0), arm64.RET()), code)
	})

	t.Run("needs the Frame capability", func(t *testing.T) {
		a := asm.New(struct{ asm.Arch }{arm64.New()})
		a.Emit(arm64.MOV(vint(0), arm64.X0))

		_, err := a.Build()
		require.ErrorIs(t, err, asm.ErrUnallocated)
	})

	t.Run("parks both registers a pair load writes", func(t *testing.T) {
		a := asm.New(arm64.New())
		a.Emit(
			arm64.LDP(vint(0), vint(1), arm64.Ctx, 0),
			arm64.BLR(arm64.X1),
			arm64.ADD(vint(2), vint(0), vint(1)),
			arm64.STR(vint(2), arm64.Ctx, 16),
			arm64.RET(),
		)

		code, err := a.Build()
		require.NoError(t, err)
		require.Equal(t, encode(t,
			arm64.LDP(arm64.X0, arm64.X2, arm64.Ctx, 0),
			arm64.STR(arm64.X0, arm64.SP, 0),
			arm64.STR(arm64.X2, arm64.SP, 8),
			arm64.BLR(arm64.X1),
			arm64.LDR(arm64.X0, arm64.SP, 0),
			arm64.LDR(arm64.X1, arm64.SP, 8),
			arm64.ADD(arm64.X2, arm64.X0, arm64.X1),
			arm64.STR(arm64.X2, arm64.Ctx, 16),
			arm64.RET(),
		), code)
	})

	t.Run("keeps a value in its register past a call it is not live at", func(t *testing.T) {
		a := asm.New(arm64.New())
		other, join := a.Label(), a.Label()
		a.Emit(
			arm64.MOVI(vint(0), 7),
			arm64.CBZLabel(arm64.X1, other),
			arm64.BLR(arm64.X2),
			arm64.BLabel(join),
		)
		a.Bind(other)
		a.Emit(arm64.STR(vint(0), arm64.Ctx, 0))
		a.Bind(join)
		a.Emit(arm64.RET())

		code, err := a.Build()
		require.NoError(t, err)
		require.Equal(t, encode(t,
			arm64.MOVI(arm64.X0, 7),
			arm64.CBZ(arm64.X1, 12),
			arm64.BLR(arm64.X2),
			arm64.B(8),
			arm64.STR(arm64.X0, arm64.Ctx, 0),
			arm64.RET(),
		), code)
	})

	t.Run("spills a single float in its own width", func(t *testing.T) {
		a := asm.New(arm64.New())
		s := asm.NewVReg(0, asm.RegTypeFloat, asm.Width32)
		a.Emit(
			arm64.FMOV(s, arm64.W2),
			arm64.BLR(arm64.X1),
			arm64.FMOV(arm64.W0, s),
			arm64.STRW(arm64.W0, arm64.Ctx, 0),
			arm64.RET(),
		)

		code, err := a.Build()
		require.NoError(t, err)
		require.Equal(t, encode(t,
			arm64.FMOV(arm64.S0, arm64.W2),
			arm64.STR(arm64.S0, arm64.SP, 0),
			arm64.BLR(arm64.X1),
			arm64.LDR(arm64.S0, arm64.SP, 0),
			arm64.FMOV(arm64.W0, arm64.S0),
			arm64.STRW(arm64.W0, arm64.Ctx, 0),
			arm64.RET(),
		), code)
	})

	t.Run("fails when one row needs more registers than the bank holds", func(t *testing.T) {
		a := asm.New(narrow{arm64.New(), arm64.New()})
		a.Emit(
			arm64.MOVI(vint(0), 1),
			arm64.MOVI(vint(1), 2),
			arm64.ADD(vint(2), vint(0), vint(1)),
			arm64.STR(vint(2), arm64.Ctx, 0),
			arm64.RET(),
		)

		_, err := a.Build()
		require.ErrorIs(t, err, asm.ErrNoRegistersAvailable)
	})

	t.Run("keeps a register value untouched through the exit stub alongside reloaded locals", func(t *testing.T) {
		// v0, v1 are call-live locals an earlier BLR forces to spill; v2 is
		// a value defined after that call and used only past EXIT, mirroring
		// an exit map naming both a register value and spilled ones. EXIT is
		// FlowNext, so v2's register is never up for reuse across it.
		a := asm.New(arm64.New())
		a.Emit(
			slots(arm64.OpSUBI),
			arm64.MOVI(vint(0), 1),
			arm64.MOVI(vint(1), 2),
			arm64.BLR(arm64.X2),
			arm64.MOVI(vint(2), 3),
			arm64.EXIT(arm64.X16),
			arm64.USE(vint(0)), arm64.USE(vint(1)), arm64.USE(vint(2)),
			slots(arm64.OpADDI),
			arm64.RET(),
		)

		code, err := a.Build()
		require.NoError(t, err)
		require.Equal(t, encode(t,
			arm64.SUBI(arm64.SP, arm64.SP, 16),
			arm64.MOVI(arm64.X0, 1),
			arm64.STR(arm64.X0, arm64.SP, 0),
			arm64.MOVI(arm64.X0, 2),
			arm64.STR(arm64.X0, arm64.SP, 8),
			arm64.BLR(arm64.X2),
			arm64.MOVI(arm64.X0, 3),
			arm64.EXIT(arm64.X16),
			arm64.LDR(arm64.X1, arm64.SP, 0),
			arm64.LDR(arm64.X1, arm64.SP, 8),
			arm64.ADDI(arm64.SP, arm64.SP, 16),
			arm64.RET(),
		), code)
	})
}
