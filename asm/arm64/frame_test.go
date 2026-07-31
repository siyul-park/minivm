package arm64_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/asm"
	arm64 "github.com/siyul-park/minivm/asm/arm64"
)

func TestArch_Frame(t *testing.T) {
	t.Run("chunks large spill areas", func(t *testing.T) {
		// 512 slots need 4096 bytes, one byte past the unshifted add/sub
		// immediate range, so each adjustment splits into two steps that keep
		// SP 16-byte aligned throughout.
		frame := arm64.New().Frame()

		require.Equal(t, []asm.Instruction{
			arm64.SUBI(arm64.SP, arm64.SP, 4080),
			arm64.SUBI(arm64.SP, arm64.SP, 16),
			arm64.ADDI(arm64.X26, arm64.SP, 0),
		}, frame.Enter(512))
		require.Equal(t, []asm.Instruction{
			arm64.SUBI(arm64.SP, arm64.SP, 4080),
			arm64.SUBI(arm64.SP, arm64.SP, 16),
		}, frame.Resume(512))
		require.Equal(t, []asm.Instruction{
			arm64.ADDI(arm64.SP, arm64.SP, 4080),
			arm64.ADDI(arm64.SP, arm64.SP, 16),
		}, frame.Leave(512))
	})
}
