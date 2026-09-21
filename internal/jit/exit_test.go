package jit_test

import (
	"testing"

	"github.com/siyul-park/minivm/internal/jit"
	"github.com/stretchr/testify/require"
)

func TestKind_String(t *testing.T) {
	tests := []struct {
		kind jit.Kind
		want string
	}{
		{jit.ExitDeopt, "deopt"},
		{jit.ExitBridge, "bridge"},
		{jit.ExitSafepoint, "safepoint"},
		{jit.ExitRelease, "release"},
		{jit.ExitCall, "call"},
		{jit.Kind(99), "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			require.Equal(t, tt.want, tt.kind.String())
		})
	}
}
