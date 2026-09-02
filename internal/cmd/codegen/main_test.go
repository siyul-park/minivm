package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	binary := filepath.Join(t.TempDir(), "codegen")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./internal/cmd/codegen")
	build.Dir = root
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))

	temp := t.TempDir()
	command := exec.CommandContext(t.Context(), binary)
	command.Dir = temp
	output, err = command.CombinedOutput()
	require.NoError(t, err, string(output))
	require.Equal(t, "interp/threaded.go\n", string(output))

	golden, err := os.ReadFile(filepath.Join(temp, "interp", "threaded.go"))
	require.NoError(t, err)

	cases := []struct {
		name     string
		content  []byte // nil means the file is absent
		wantErr  bool
		contains string
		after    func(t *testing.T, out string)
	}{
		{
			name:    "up to date output passes silently",
			content: golden,
		},
		{
			name:     "stale output is rejected without being rewritten",
			content:  []byte("stale"),
			wantErr:  true,
			contains: "interp/threaded.go is stale",
			after: func(t *testing.T, out string) {
				actual, err := os.ReadFile(out)
				require.NoError(t, err)
				require.Equal(t, []byte("stale"), actual)
			},
		},
		{
			name:     "missing output fails to read",
			wantErr:  true,
			contains: "read interp/threaded.go",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			out := filepath.Join(dir, "interp", "threaded.go")
			require.NoError(t, os.MkdirAll(filepath.Dir(out), 0o755))
			if tc.content != nil {
				require.NoError(t, os.WriteFile(out, tc.content, 0o644))
			}

			command := exec.CommandContext(t.Context(), binary, "-check")
			command.Dir = dir
			output, err := command.CombinedOutput()
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, string(output), tc.contains)
			} else {
				require.NoError(t, err, string(output))
				require.Empty(t, output)
			}
			if tc.after != nil {
				tc.after(t, out)
			}
		})
	}
}
