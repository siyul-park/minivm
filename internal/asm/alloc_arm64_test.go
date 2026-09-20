package asm_test

import (
	"testing"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/stretchr/testify/require"
)

func TestAssembler_Build_arm64(t *testing.T) {
	t.Run("runs a value spilled across an exit", func(t *testing.T) {
		s, err := asm.NewState(4096)
		require.NoError(t, err)
		a := asm.New(arm64.New())
		a.Emit(
			arm64.SUBI(arm64.SP, arm64.SP, 16),
			arm64.STR(arm64.LR, arm64.SP, 8),
			slots(arm64.OpSUBI),
			arm64.MOVI(vint(0), 42),
		)
		a.Emit(exit()...)
		a.Emit(
			arm64.ADDI(arm64.X0, vint(0), 1),
		)
		a.Emit(exit()...)
		a.Emit(
			slots(arm64.OpADDI),
			arm64.LDR(arm64.LR, arm64.SP, 8),
			arm64.ADDI(arm64.SP, arm64.SP, 16),
			arm64.RET(),
		)
		code, err := a.Build()
		require.NoError(t, err)
		buffer, err := asm.NewBuffer(len(code))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, buffer.Free()) })
		addr, err := asm.Link(buffer, code)
		require.NoError(t, err)

		require.True(t, asm.Enter(addr, &s))
		require.True(t, asm.Resume(&s))
		require.Equal(t, uint64(43), s.Reg(arm64.X0))
		require.False(t, asm.Resume(&s))
	})
}
