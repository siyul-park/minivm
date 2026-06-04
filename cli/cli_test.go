package cli

import (
	"bytes"
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

	t.Run("uses configured filesystem", func(t *testing.T) {
		out := bytes.NewBuffer(nil)
		fsys := newMemFS()
		fsys.files["main.vm"] = []byte("0000:\ti32.const 0x00000007\n")
		root := Root(WithFS(fsys))
		root.SetOut(out)
		root.SetErr(out)
		root.SetArgs([]string{"run", "main.vm"})

		require.NoError(t, root.Execute())
		require.Equal(t, "7\n", out.String())
	})
}
