package compile_test

import (
	"testing"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/compile"
	"github.com/stretchr/testify/require"
)

func buffer(t *testing.T) *asm.Buffer {
	t.Helper()
	buf, err := asm.NewBuffer(4096)
	require.NoError(t, err)
	return buf
}

func TestNewStore(t *testing.T) {
	s := compile.NewStore()

	_, ok := s.Code(0)
	require.False(t, ok)
	require.False(t, s.Shared())
	require.NoError(t, s.Close())
}

func TestStore_Attach(t *testing.T) {
	t.Run("admits holders until the store closes", func(t *testing.T) {
		s := compile.NewStore()

		require.True(t, s.Attach())
		require.NoError(t, s.Close())
		require.False(t, s.Attach())
		require.NoError(t, s.Detach())
	})

	t.Run("marks the store shared", func(t *testing.T) {
		s := compile.NewStore()
		require.False(t, s.Shared())

		require.True(t, s.Attach())
		require.True(t, s.Shared())

		require.NoError(t, s.Detach())
		require.NoError(t, s.Close())
	})
}

func TestStore_Detach(t *testing.T) {
	t.Run("keeps published code installable until the last holder leaves", func(t *testing.T) {
		s := compile.NewStore()
		require.True(t, s.Attach())
		s.Publish(code(jit.Anchor{Addr: 1}), buffer(t))

		require.NoError(t, s.Detach())
		published, ok := s.Code(0)
		require.True(t, ok, "one holder leaving must not retire what everyone dispatches into")
		require.NotNil(t, published)

		require.NoError(t, s.Close())
	})
}

func TestStore_Close(t *testing.T) {
	s := compile.NewStore()
	s.Publish(code(jit.Anchor{Addr: 1}), buffer(t))

	require.NoError(t, s.Close())
	require.NoError(t, s.Close(), "closing twice must not free the buffers twice")
}

func TestStore_Shared(t *testing.T) {
	s := compile.NewStore()
	defer func() { require.NoError(t, s.Close()) }()

	require.False(t, s.Shared(), "a store nobody else attached to needs no polling")
	require.True(t, s.Attach())
	require.NoError(t, s.Detach())
	require.True(t, s.Shared(), "a holder that has left could still have published")
}

func TestStore_Publish(t *testing.T) {
	t.Run("appends code in publication order", func(t *testing.T) {
		s := compile.NewStore()
		defer func() { require.NoError(t, s.Close()) }()

		first := code(jit.Anchor{Addr: 1})
		second := code(jit.Anchor{Addr: 2})
		s.Publish(first, buffer(t))
		s.Publish(second, buffer(t))

		got, ok := s.Code(0)
		require.True(t, ok)
		require.Same(t, first, got)
		got, ok = s.Code(1)
		require.True(t, ok)
		require.Same(t, second, got)
	})

	t.Run("publishes nothing for code that emitted no entry", func(t *testing.T) {
		s := compile.NewStore()
		defer func() { require.NoError(t, s.Close()) }()

		s.Publish(nil, buffer(t))
		s.Publish(code(), buffer(t))

		_, ok := s.Code(0)
		require.False(t, ok)
	})
}

func TestStore_Code(t *testing.T) {
	s := compile.NewStore()
	defer func() { require.NoError(t, s.Close()) }()
	s.Publish(code(jit.Anchor{Addr: 1}), buffer(t))

	_, ok := s.Code(-1)
	require.False(t, ok)
	_, ok = s.Code(0)
	require.True(t, ok)
	_, ok = s.Code(1)
	require.False(t, ok, "a caller that has seen everything published waits for more")
}
