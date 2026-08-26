package cli_test

import (
	"bytes"
	"fmt"
	"io"
	"testing"
	"testing/fstest"

	cli "github.com/siyul-park/minivm/cli"
	"github.com/stretchr/testify/require"
)

// mapWriteFS adapts fstest.MapFS to cli.WriteFS so it can be passed to
// cli.WithFS. Create is unused by the `run` subcommand exercised below.
type mapWriteFS struct {
	fstest.MapFS
}

func (mapWriteFS) Create(name string) (io.WriteCloser, error) {
	return nil, fmt.Errorf("create %s: not supported", name)
}

func TestWithFS(t *testing.T) {
	// This path is not a real file on the OS filesystem, so the default
	// OS() filesystem would fail to find it. Only if WithFS actually
	// overrides Root's filesystem does `run` succeed here.
	const path = "virtual/main.vm"
	fsys := mapWriteFS{MapFS: fstest.MapFS{
		path: &fstest.MapFile{Data: []byte("0000:\ti32.const 0x00000007\n")},
	}}

	out := bytes.NewBuffer(nil)
	root := cli.Root(cli.WithFS(fsys))
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
