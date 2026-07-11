package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	t.Run("writes deterministic inline fusion sources", func(t *testing.T) {
		first, err := generate()
		require.NoError(t, err)
		second, err := generate()
		require.NoError(t, err)
		require.Equal(t, first, second)

		paths := make([]string, len(first))
		for idx, output := range first {
			paths[idx] = output.path
		}
		require.IsIncreasing(t, paths)
		require.Contains(t, paths, "docs/fusion.md")
	})

	t.Run("checks generated files", func(t *testing.T) {
		t.Chdir(t.TempDir())

		var stdout bytes.Buffer
		require.NoError(t, run(false, &stdout))
		require.Contains(t, stdout.String(), "interp/fusion_gen.go")
		require.NoError(t, run(true, &stdout))

		path := filepath.Join("interp", "fusion_gen.go")
		require.NoError(t, os.WriteFile(path, []byte("stale"), 0o644))
		require.ErrorContains(t, run(true, &stdout), "is stale")
	})
}
