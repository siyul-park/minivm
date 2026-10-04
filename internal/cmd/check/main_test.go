package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommand(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	binary := filepath.Join(t.TempDir(), "check")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./internal/cmd/check")
	build.Dir = root
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))

	t.Run("diagnostics use go-style locations", func(t *testing.T) {
		dir := fixture(t)
		command := exec.CommandContext(t.Context(), binary, "./...")
		command.Dir = dir

		output, err := command.CombinedOutput()
		require.Error(t, err)
		require.Contains(t, string(output), "thing.go:3:6: warning: [CP001]")
	})

	t.Run("json emits one object per diagnostic", func(t *testing.T) {
		dir := fixture(t)
		command := exec.CommandContext(t.Context(), binary, "-json", "./...")
		command.Dir = dir

		output, err := command.CombinedOutput()
		require.Error(t, err)
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		var diagnostic map[string]any
		require.NoError(t, json.Unmarshal([]byte(lines[0]), &diagnostic))
		require.Equal(t, "CP001", diagnostic["rule"])
		require.Equal(t, float64(3), diagnostic["line"])
		require.Equal(t, float64(6), diagnostic["column"])
	})

	t.Run("rules can be listed", func(t *testing.T) {
		command := exec.CommandContext(t.Context(), binary, "-list-rules")
		output, err := command.CombinedOutput()
		require.NoError(t, err)
		require.Contains(t, string(output), "CP001")
		require.Contains(t, string(output), "CP006")
		require.Contains(t, string(output), "CP007")
		require.Contains(t, string(output), "warning")
	})

	t.Run("warnings do not fail by default", func(t *testing.T) {
		dir := warningFixture(t)
		command := exec.CommandContext(t.Context(), binary, "./...")
		command.Dir = dir
		output, err := command.CombinedOutput()
		require.NoError(t, err, string(output))
		require.Contains(t, string(output), "warning: [CP007]")
	})

	t.Run("split owner tests fail", func(t *testing.T) {
		dir := splitOwnerFixture(t)
		command := exec.CommandContext(t.Context(), binary, "./...")
		command.Dir = dir
		output, err := command.CombinedOutput()
		require.Error(t, err)
		require.Contains(t, string(output), "error: [TP005] public symbol is split across 2 top-level tests")
		require.Contains(t, string(output), "warning: [TP005] public symbol has no top-level owner test TestOther")
	})

	t.Run("strict treats warnings as errors", func(t *testing.T) {
		dir := warningFixture(t)
		command := exec.CommandContext(t.Context(), binary, "-strict", "./...")
		command.Dir = dir
		require.Error(t, command.Run())
	})
}

func fixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/fixture\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "thing.go"), []byte("package fixture\n\ntype Thing struct{}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "thing_test.go"), []byte("package fixture\n\nimport \"testing\"\n\nfunc TestThing(t *testing.T) {\n\tt.Run(\"outer\", func(t *testing.T) {\n\t\tt.Run(\"inner\", func(t *testing.T) {})\n\t})\n}\n"), 0o644))
	return dir
}

func warningFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/warning\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "warning.go"), []byte("package warning\n\nfunc caller() { helper() }\n\nfunc helper() { implementation() }\n\nfunc implementation() {}\n"), 0o644))
	return dir
}

func splitOwnerFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/split\n\ngo 1.26\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "thing.go"), []byte("package split\n\n// Thing is the fixture symbol.\nfunc Thing() {}\n\n// Other is intentionally untested.\nfunc Other() {}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "thing_test.go"), []byte("package split_test\n\nimport \"testing\"\n\nfunc TestThing(t *testing.T) {}\n\nfunc TestThing_Error(t *testing.T) {}\n"), 0o644))
	return dir
}
