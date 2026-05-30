package bench

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/types"
)

func TestFib(t *testing.T) {
	t.Run("base case returns input", func(t *testing.T) {
		ctx := context.Background()
		i := interp.New(Fib(1))
		defer i.Close()

		err := i.Run(ctx)
		require.NoError(t, err)

		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(1), got)
		require.Equal(t, 0, i.Len())
	})

	t.Run("recursive case returns fibonacci value", func(t *testing.T) {
		ctx := context.Background()
		i := interp.New(Fib(7))
		defer i.Close()

		err := i.Run(ctx)
		require.NoError(t, err)

		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(13), got)
		require.Equal(t, 0, i.Len())
	})
}
