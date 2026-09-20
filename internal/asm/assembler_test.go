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
	t.Run("unallocated register", func(t *testing.T) {
		assembler := asm.New(arm64.New())
		v := asm.NewVReg(0, asm.RegTypeInt, asm.Width64)
		assembler.Emit(arm64.MOV(v, arm64.X0))

		_, err := assembler.Build()
		require.ErrorIs(t, err, asm.ErrUnallocated)
	})
	t.Run("unallocated memory base", func(t *testing.T) {
		assembler := asm.New(arm64.New())
		v := asm.NewVReg(0, asm.RegTypeInt, asm.Width64)
		assembler.Emit(arm64.LDR(arm64.X0, v, 0))

		_, err := assembler.Build()
		require.ErrorIs(t, err, asm.ErrUnallocated)
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
}
