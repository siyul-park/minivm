package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoot(t *testing.T) {
	t.Run("exposes run subcommand", func(t *testing.T) {
		root := Root()
		_, _, err := root.Find([]string{"run"})
		require.NoError(t, err)
	})

	t.Run("default Use is minivm", func(t *testing.T) {
		require.Equal(t, "minivm", Root().Use)
	})
}
