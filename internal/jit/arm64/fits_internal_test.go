package arm64

import (
	"testing"

	"github.com/siyul-park/minivm/internal/jit/backend"

	"github.com/stretchr/testify/require"
)

// TestFits protects the backend-to-ARM64 deopt contract: every stack,
// frame, and flush coordinate must fit the immediate addressing range before
// a native cold path is emitted. This is an internal boundary contract, not a
// public ARM64 API.
func TestFits(t *testing.T) {
	t.Run("empty frames cannot be rebuilt", func(t *testing.T) {
		require.False(t, fits(backend.Deopt{}))
	})
	t.Run("one frame within range", func(t *testing.T) {
		require.True(t, fits(backend.Deopt{SP: 8, Frames: []backend.Record{{BP: 0}}}))
	})
	t.Run("inlined frames within range", func(t *testing.T) {
		require.True(t, fits(backend.Deopt{
			SP:     10,
			Slots:  []backend.Flush{{Slot: 8}, {Slot: 9}},
			Frames: []backend.Record{{BP: 0}, {BP: 8}},
		}))
	})
	t.Run("coordinates past the immediate stay threaded", func(t *testing.T) {
		require.False(t, fits(backend.Deopt{SP: maxSlot + 1, Frames: []backend.Record{{}}}))
		require.False(t, fits(backend.Deopt{Frames: []backend.Record{{BP: maxSlot + 1}}}))
		require.False(t, fits(backend.Deopt{
			Frames: []backend.Record{{}},
			Slots:  []backend.Flush{{Slot: maxSlot + 1}},
		}))
	})
}
