package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	cases := []struct {
		name      string
		content   []byte
		generate  bool
		wantErr   bool
		contains  string
		unchanged bool
	}{
		{
			name:     "generates threaded output by default",
			generate: true,
		},

		{
			name:     "up to date output passes silently",
			generate: true,
		},
		{
			name:      "stale output is rejected without being rewritten",
			content:   []byte("stale"),
			wantErr:   true,
			contains:  "interp/threaded.go is stale",
			unchanged: true,
		},
		{
			name:     "missing output fails to read",
			wantErr:  true,
			contains: "read interp/threaded.go",
		},
	}
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			binary := filepath.Join(t.TempDir(), "codegen")
			build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./internal/cmd/codegen")
			build.Dir = root
			output, err := build.CombinedOutput()
			require.NoError(t, err, string(output))

			dir := t.TempDir()
			out := filepath.Join(dir, "interp", "threaded.go")
			require.NoError(t, os.MkdirAll(filepath.Dir(out), 0o755))
			content := tc.content
			if tc.generate {
				command := exec.CommandContext(t.Context(), binary)
				command.Dir = dir
				output, err := command.CombinedOutput()
				require.NoError(t, err)
				require.Equal(t, "interp/threaded.go\n", string(output))
				content, err = os.ReadFile(out)
				require.NoError(t, err)
			}
			if content != nil {
				require.NoError(t, os.WriteFile(out, content, 0o644))
			}
			command := exec.CommandContext(t.Context(), binary, "-check")
			command.Dir = dir
			output, err = command.CombinedOutput()
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, string(output), tc.contains)
			} else {
				require.NoError(t, err)
				require.Empty(t, output)
			}
			if tc.unchanged {
				actual, err := os.ReadFile(out)
				require.NoError(t, err)
				require.Equal(t, tc.content, actual)
			}
		})
	}
}
