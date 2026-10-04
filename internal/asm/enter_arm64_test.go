package asm_test

import (
	"runtime"
	"slices"
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

// exit is the native side of the exit protocol: call the stub the state
// names.
func exit() []asm.Instruction {
	return []asm.Instruction{
		arm64.LDR(arm64.X16, arm64.Ctx, int16(asm.OffsetStub)),
		arm64.BLR(arm64.X16),
	}
}

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
		s, err := asm.NewState(4096)
		require.NoError(t, err)
		code := link(t, arm64.RET())

		require.False(t, asm.Enter(code, &s))
	})

	t.Run("keeps the native stack pointer aligned", func(t *testing.T) {
		s, err := asm.NewState(4096)
		require.NoError(t, err)
		code := link(t, frame(slices.Concat([]asm.Instruction{arm64.ADDI(arm64.X0, arm64.SP, 16)}, exit())...)...)

		require.True(t, asm.Enter(code, &s))
		require.False(t, asm.Resume(&s))
		stack := s.Reg(arm64.X0)
		require.NotZero(t, stack)
		require.Zero(t, stack%16)
	})

	t.Run("handles a small stack allocation", func(t *testing.T) {
		s, err := asm.NewState(8)
		require.NoError(t, err)

		require.False(t, asm.Enter(link(t, frame()...), &s))
	})

	t.Run("suspends at an exit", func(t *testing.T) {
		s, err := asm.NewState(4096)
		require.NoError(t, err)
		code := link(t, frame(slices.Concat([]asm.Instruction{arm64.MOVI(arm64.X0, 42)}, exit())...)...)

		require.True(t, asm.Enter(code, &s))
		require.Equal(t, uint64(42), s.Reg(arm64.X0))
	})

	t.Run("nests below a suspended activation", func(t *testing.T) {
		s, err := asm.NewState(4096)
		require.NoError(t, err)
		outer := link(t, frame(slices.Concat(
			[]asm.Instruction{arm64.MOVI(arm64.X0, 1), arm64.STR(arm64.X0, arm64.SP, 0)},
			exit(),
			[]asm.Instruction{arm64.LDR(arm64.X0, arm64.SP, 0)},
			exit(),
		)...)...)
		inner := link(t,
			arm64.MOVI(arm64.X1, 2),
			arm64.SUBI(arm64.SP, arm64.SP, 16),
			arm64.STR(arm64.X1, arm64.SP, 0),
			arm64.ADDI(arm64.SP, arm64.SP, 16),
			arm64.RET(),
		)

		require.True(t, asm.Enter(outer, &s))
		require.False(t, asm.Enter(inner, &s))
		require.True(t, asm.Resume(&s))
		require.Equal(t, uint64(1), s.Reg(arm64.X0))
		require.False(t, asm.Resume(&s))
	})
}

func TestResume(t *testing.T) {
	t.Run("continues with the registers Go set", func(t *testing.T) {
		s, err := asm.NewState(4096)
		require.NoError(t, err)
		code := link(t, frame(slices.Concat(
			[]asm.Instruction{arm64.MOVI(arm64.X0, 42)},
			exit(),
			[]asm.Instruction{arm64.ADDI(arm64.X0, arm64.X0, 1)},
			exit(),
		)...)...)

		require.True(t, asm.Enter(code, &s))
		s.SetReg(arm64.X0, 100)
		require.True(t, asm.Resume(&s))
		require.Equal(t, uint64(101), s.Reg(arm64.X0))
		require.False(t, asm.Resume(&s))
	})

	t.Run("restores every saved register", func(t *testing.T) {
		s, err := asm.NewState(4096)
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
		insts = append(insts, exit()...)
		insts = append(insts, exit()...)
		code := link(t, frame(insts...)...)

		require.True(t, asm.Enter(code, &s))
		for _, r := range saved {
			require.Equal(t, uint64(r.ID())+1, s.Reg(r), r.String())
			s.SetReg(r, uint64(r.ID())+1000)
		}
		for i := range 32 {
			require.Equal(t, uint64(i)+100, s.Reg(fregs[i]), fregs[i].String())
			s.SetReg(fregs[i], uint64(i)+2000)
		}
		require.True(t, asm.Resume(&s))
		for _, r := range saved {
			require.Equal(t, uint64(r.ID())+1000, s.Reg(r), r.String())
		}
		for i := range 32 {
			require.Equal(t, uint64(i)+2000, s.Reg(fregs[i]), fregs[i].String())
		}
		require.False(t, asm.Resume(&s))
	})

	t.Run("survives Go stack growth and collection while suspended", func(t *testing.T) {
		s, err := asm.NewState(4096)
		require.NoError(t, err)
		code := link(t, frame(slices.Concat(
			[]asm.Instruction{arm64.MOVI(arm64.X0, 3), arm64.STR(arm64.X0, arm64.SP, 0)},
			exit(),
			[]asm.Instruction{arm64.LDR(arm64.X1, arm64.SP, 0), arm64.ADD(arm64.X0, arm64.X0, arm64.X1)},
			exit(),
		)...)...)

		require.True(t, asm.Enter(code, &s))
		require.Equal(t, 1<<16, grow(1<<16))
		runtime.GC()
		require.True(t, asm.Resume(&s))
		require.Equal(t, uint64(6), s.Reg(arm64.X0))
		require.False(t, asm.Resume(&s))
	})
}

func grow(n int) int {
	var pad [64]byte
	if n == 0 {
		return int(pad[0])
	}
	return grow(n-1) + 1
}
