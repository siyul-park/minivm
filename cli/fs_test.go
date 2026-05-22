package cli

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOS(t *testing.T) {
	t.Run("Create then Open round-trips file contents", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "hello.txt")

		w, err := OS().Create(path)
		require.NoError(t, err)
		_, err = io.WriteString(w, "hi")
		require.NoError(t, err)
		require.NoError(t, w.Close())

		f, err := OS().Open(path)
		require.NoError(t, err)
		defer f.Close()

		got, err := io.ReadAll(f)
		require.NoError(t, err)
		require.Equal(t, "hi", string(got))
	})

	t.Run("Open returns error for missing file", func(t *testing.T) {
		_, err := OS().Open(filepath.Join(t.TempDir(), "missing"))
		require.True(t, os.IsNotExist(err))
	})
}
