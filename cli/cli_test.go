package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	cli "github.com/siyul-park/minivm/cli"
	"github.com/stretchr/testify/require"
)

func TestWithFS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.vm")
	require.NoError(t, os.WriteFile(path, []byte("0000:\ti32.const 0x00000007\n"), 0o644))

	out := bytes.NewBuffer(nil)
	root := cli.Root(cli.WithFS(cli.OS()))
	root.SetOut(out)
	root.SetErr(out)
	root.SetArgs([]string{"run", path})

	require.NoError(t, root.Execute())
	require.Equal(t, "7\n", out.String())
}

func TestRoot(t *testing.T) {
	t.Run("exposes run subcommand", func(t *testing.T) {
		root := cli.Root()
		_, _, err := root.Find([]string{"run"})
		require.NoError(t, err)
	})

	t.Run("default Use is minivm", func(t *testing.T) {
		require.Equal(t, "minivm", cli.Root().Use)
	})
}
