package arm64

import (
	"testing"

	"github.com/siyul-park/minivm/asm"
	"github.com/stretchr/testify/require"
)

func TestFrame(t *testing.T) {
	t.Run("chunks large spill areas", func(t *testing.T) {
		require.Equal(t, []asm.Instruction{
			SUBI(SP, SP, maxFrameAdjust),
			SUBI(SP, SP, 16),
		}, frame{}.Enter(512))
		require.Equal(t, []asm.Instruction{
			ADDI(SP, SP, maxFrameAdjust),
			ADDI(SP, SP, 16),
		}, frame{}.Leave(512))
	})
}
