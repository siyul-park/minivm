package arm64

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/asm"
)

func TestArch_Frame(t *testing.T) {
	t.Run("chunks large spill areas", func(t *testing.T) {
		// 512 slots need 4096 bytes, one byte past the unshifted add/sub
		// immediate range, so each adjustment splits into two steps that keep
		// SP 16-byte aligned throughout.
		frame := New().Frame()

		require.Equal(t, []asm.Instruction{
			SUBI(SP, SP, 4080),
			SUBI(SP, SP, 16),
			ADDI(X26, SP, 0),
		}, frame.Enter(512))
		require.Equal(t, []asm.Instruction{
			SUBI(SP, SP, 4080),
			SUBI(SP, SP, 16),
		}, frame.Resume(512))
		require.Equal(t, []asm.Instruction{
			ADDI(SP, SP, 4080),
			ADDI(SP, SP, 16),
		}, frame.Leave(512))
	})
}
