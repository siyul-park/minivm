package jit_test

import (
	"testing"
	"unsafe"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/stretchr/testify/require"
)

func TestNewContext(t *testing.T) {
	t.Run("rejects an empty stack", func(t *testing.T) {
		_, err := jit.NewContext(0)
		require.ErrorIs(t, err, asm.ErrInvalidArgs)
	})

	t.Run("starts with no exit taken", func(t *testing.T) {
		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		require.Zero(t, ctx.Exit())
	})
}

func TestContext_Layout(t *testing.T) {
	tests := []struct {
		name string
		off  uintptr
		want uintptr
	}{
		{"state", unsafe.Offsetof(jit.Context{}.State), 0},
		{"stack", jit.OffsetStack, unsafe.Offsetof(jit.Context{}.Stack)},
		{"globals", jit.OffsetGlobals, unsafe.Offsetof(jit.Context{}.Globals)},
		{"rc", jit.OffsetRC, unsafe.Offsetof(jit.Context{}.RC)},
		{"natives", jit.OffsetNatives, unsafe.Offsetof(jit.Context{}.Natives)},
		{"fb", jit.OffsetFB, unsafe.Offsetof(jit.Context{}.FB)},
		{"depth", jit.OffsetDepth, unsafe.Offsetof(jit.Context{}.Depth)},
		{"limit", jit.OffsetLimit, unsafe.Offsetof(jit.Context{}.Limit)},
		{"budget", jit.OffsetBudget, unsafe.Offsetof(jit.Context{}.Budget)},
		{"results", jit.OffsetResults, unsafe.Offsetof(jit.Context{}.Results)},
		{"records", jit.OffsetRecords, unsafe.Offsetof(jit.Context{}.Records)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.off)
		})
	}
	require.Equal(t, uintptr(32), unsafe.Sizeof(jit.Record{}))
	require.Equal(t, uintptr(0), jit.RecordFB)
	require.Equal(t, uintptr(8), jit.RecordSP)
	require.Equal(t, uintptr(16), jit.RecordPC)
	require.Equal(t, uintptr(24), jit.RecordExit)
}

func TestTrap_String(t *testing.T) {
	tests := []struct {
		trap jit.Trap
		want string
	}{
		{jit.TrapReturn, "return"},
		{jit.TrapDeopt, "deopt"},
		{jit.TrapBridge, "bridge"},
		{jit.Trap(99), "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			require.Equal(t, tt.want, tt.trap.String())
		})
	}
}
