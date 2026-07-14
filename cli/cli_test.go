package cli

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithFS(t *testing.T) {
	t.Chdir(t.TempDir())
	require.NoError(t, os.WriteFile("main.vm", []byte("0000:\ti32.const 0x00000007\n"), 0o644))

	out := bytes.NewBuffer(nil)
	root := Root(WithFS(OS()))
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"run", "main.vm"})

	require.NoError(t, root.Execute())
	require.Equal(t, "7\n", out.String())
}

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
