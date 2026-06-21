package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestNewRunCommand(t *testing.T) {
	t.Run("runs program and prints final stack", func(t *testing.T) {
		fsys := fstest.MapFS{
			"add.mvm": &fstest.MapFile{Data: []byte("0000:\ti32.const 0x00000001\n0005:\ti32.const 0x00000002\n0010:\ti32.add\n")},
		}
		var out bytes.Buffer
		cmd := NewRunCommand(fsys)
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"add.mvm"})

		require.NoError(t, cmd.ExecuteContext(context.Background()))
		require.Contains(t, out.String(), "3")
	})

	t.Run("empty stack produces no output", func(t *testing.T) {
		fsys := fstest.MapFS{
			"nop.mvm": &fstest.MapFile{Data: []byte("0000:\tnop\n")},
		}
		var out bytes.Buffer
		cmd := NewRunCommand(fsys)
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"nop.mvm"})

		require.NoError(t, cmd.ExecuteContext(context.Background()))
		require.Empty(t, strings.TrimSpace(out.String()))
	})

	t.Run("missing file returns open error", func(t *testing.T) {
		var out bytes.Buffer
		cmd := NewRunCommand(fstest.MapFS{})
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"missing.mvm"})

		err := cmd.ExecuteContext(context.Background())
		require.Error(t, err)
		require.Contains(t, err.Error(), "open missing.mvm")
	})

	t.Run("parse error propagates", func(t *testing.T) {
		fsys := fstest.MapFS{
			"bad.mvm": &fstest.MapFile{Data: []byte("not-an-instruction xyz\n")},
		}
		var out bytes.Buffer
		cmd := NewRunCommand(fsys)
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"bad.mvm"})

		err := cmd.ExecuteContext(context.Background())
		require.Error(t, err)
		require.Contains(t, err.Error(), "parse bad.mvm")
	})

	t.Run("runtime error propagates", func(t *testing.T) {
		fsys := fstest.MapFS{
			"divzero.mvm": &fstest.MapFile{Data: []byte("0000:\ti32.const 0x00000001\n0005:\ti32.const 0x00000000\n0010:\ti32.div_s\n")},
		}
		var out bytes.Buffer
		cmd := NewRunCommand(fsys)
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"divzero.mvm"})

		err := cmd.ExecuteContext(context.Background())
		require.Error(t, err)
		require.Contains(t, err.Error(), "run divzero.mvm")
	})

	t.Run("verification rejects malformed program", func(t *testing.T) {
		fsys := fstest.MapFS{
			"underflow.mvm": &fstest.MapFile{Data: []byte("0000:\tdrop\n")},
		}
		var out bytes.Buffer
		cmd := NewRunCommand(fsys)
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"underflow.mvm"})

		err := cmd.ExecuteContext(context.Background())
		require.Error(t, err)
		require.Contains(t, err.Error(), "verify underflow.mvm")
	})

	t.Run("requires exactly one arg", func(t *testing.T) {
		cmd := NewRunCommand(fstest.MapFS{})
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs(nil)
		require.Error(t, cmd.ExecuteContext(context.Background()))
	})
}
