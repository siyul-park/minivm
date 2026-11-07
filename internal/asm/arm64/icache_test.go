package arm64_test

import (
	"testing"

	"github.com/siyul-park/minivm/internal/asm/arm64"
)

func TestArch_Flush(t *testing.T) {
	t.Run("accepts no code", func(t *testing.T) {
		arm64.New().Flush(nil)
	})

	t.Run("flushes written code", func(t *testing.T) {
		arm64.New().Flush(make([]byte, 64))
	})
}
