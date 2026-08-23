package cli_test

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"testing/fstest"

	cli "github.com/siyul-park/minivm/cli"
	"github.com/stretchr/testify/require"
)

func TestNewRunCommand(t *testing.T) {
	tests := []struct {
		name            string
		files           fstest.MapFS
		args            []string
		wantErr         bool
		wantErrContains string
		wantOutContains string
		wantOutEmpty    bool
	}{
		{
			name: "runs program and prints final stack",
			files: fstest.MapFS{
				"add.mvm": &fstest.MapFile{Data: []byte("0000:\ti32.const 0x00000001\n0005:\ti32.const 0x00000002\n0010:\ti32.add\n")},
			},
			args:            []string{"add.mvm"},
			wantOutContains: "3",
		},
		{
			name: "empty stack produces no output",
			files: fstest.MapFS{
				"nop.mvm": &fstest.MapFile{Data: []byte("0000:\tnop\n")},
			},
			args:         []string{"nop.mvm"},
			wantOutEmpty: true,
		},
		{
			name:            "missing file returns open error",
			files:           fstest.MapFS{},
			args:            []string{"missing.mvm"},
			wantErr:         true,
			wantErrContains: "open missing.mvm",
		},
		{
			name: "parse error propagates",
			files: fstest.MapFS{
				"bad.mvm": &fstest.MapFile{Data: []byte("not-an-instruction xyz\n")},
			},
			args:            []string{"bad.mvm"},
			wantErr:         true,
			wantErrContains: "parse bad.mvm",
		},
		{
			name: "runtime error propagates",
			files: fstest.MapFS{
				"divzero.mvm": &fstest.MapFile{Data: []byte("0000:\ti32.const 0x00000001\n0005:\ti32.const 0x00000000\n0010:\ti32.div_s\n")},
			},
			args:            []string{"divzero.mvm"},
			wantErr:         true,
			wantErrContains: "run divzero.mvm",
		},
		{
			name: "verification rejects malformed program",
			files: fstest.MapFS{
				"underflow.mvm": &fstest.MapFile{Data: []byte("0000:\tdrop\n")},
			},
			args:            []string{"underflow.mvm"},
			wantErr:         true,
			wantErrContains: "verify underflow.mvm",
		},
		{
			name:    "requires exactly one arg",
			files:   fstest.MapFS{},
			args:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			cmd := cli.NewRunCommand(tt.files)
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(tt.args)

			err := cmd.ExecuteContext(context.Background())
			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrContains != "" {
					require.Contains(t, err.Error(), tt.wantErrContains)
				}
				return
			}
			require.NoError(t, err)
			if tt.wantOutEmpty {
				require.Empty(t, strings.TrimSpace(out.String()))
			}
			if tt.wantOutContains != "" {
				require.Contains(t, out.String(), tt.wantOutContains)
			}
		})
	}
}
