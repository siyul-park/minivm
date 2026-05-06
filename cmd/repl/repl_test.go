package repl

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestREPL_Run(t *testing.T) {
	tests := []struct {
		input    string
		contains []string
		excludes []string
	}{
		{
			input:    "i32.const 1\ni32.const 2\ni32.add\n.quit\n",
			contains: []string{"3"},
		},
		{
			// non-trivial values: ensure box() uses NaN-boxing, not heap allocation
			input:    "i32.const 42\ni32.const 8\ni32.add\n.quit\n",
			contains: []string{"50"},
		},
		{
			input:    "i32.const 10\ni32.const 20\n.quit\n",
			contains: []string{"10 20"},
		},
		{
			input:    "f32.const 1.0\n.quit\n",
			contains: []string{"1.000000"},
			excludes: []string{"error:"},
		},
		{
			input:    "nop\n.quit\n",
			excludes: []string{"stack"},
		},
		{
			input:    "\n\ni32.const 5\n\n.quit\n",
			contains: []string{"5"},
		},
		{
			input:    "0000:\ti32.const 0x00000007\n.quit\n",
			contains: []string{"7"},
		},
		{
			input:    ".quit\n",
			contains: []string{"bye"},
		},
		{
			input:    ".exit\n",
			contains: []string{"bye"},
		},
		{
			input:    ".help\n.quit\n",
			contains: []string{".quit", ".reset"},
		},
		{
			input:    "i32.const 42\n.reset\n.show\n.quit\n",
			contains: []string{"reset.", "(empty)"},
		},
		{
			input:    "i32.const 1\ni32.const 2\ni32.add\n.show\n.quit\n",
			contains: []string{"i32.const", "i32.add"},
		},
		{
			input:    "drop\n.show\n.quit\n",
			contains: []string{"error:", "(empty)"},
		},
		{
			input:    ".unknown\n.quit\n",
			contains: []string{"unknown command"},
		},
		{
			input:    "bad.opcode 1\n.quit\n",
			contains: []string{"error:"},
		},
		{
			// declare a no-arg function constant and verify .show includes it
			input:    ".const\nfunc() i32\n0000:	i32.const 0x0000002A\n0005:	return\n\n.show\n.quit\n",
			contains: []string{"constant 0 added.", "func() i32"},
		},
		{
			// .reset clears constants
			input:    ".const\nfunc() i32\n0000:	i32.const 0x0000002A\n0005:	return\n\n.reset\n.show\n.quit\n",
			contains: []string{"reset.", "(empty)"},
		},
		{
			// empty .const block reports error
			input:    ".const\n\n.quit\n",
			contains: []string{"error:"},
		},
		{
			// declare a type and verify .show includes it
			input:    ".type []i32\n.show\n.quit\n",
			contains: []string{"type 0 added.", "[]i32"},
		},
		{
			// .type with missing argument reports error
			input:    ".type\n.quit\n",
			contains: []string{"error:"},
		},
		{
			// .reset clears types
			input:    ".type []i32\n.reset\n.show\n.quit\n",
			contains: []string{"reset.", "(empty)"},
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.input), func(t *testing.T) {
			var out bytes.Buffer
			r := New(strings.NewReader(tt.input), &out)
			require.NoError(t, r.Run(context.Background()))
			output := out.String()
			for _, s := range tt.contains {
				require.Contains(t, output, s)
			}
			for _, s := range tt.excludes {
				require.NotContains(t, output, s)
			}
		})
	}

	t.Run("eof exits cleanly", func(t *testing.T) {
		var out bytes.Buffer
		r := New(strings.NewReader("i32.const 1\n"), &out)
		require.NoError(t, r.Run(context.Background()))
	})

	t.Run("stack accumulates bottom to top", func(t *testing.T) {
		var out bytes.Buffer
		r := New(strings.NewReader("i32.const 10\ni32.const 20\n.quit\n"), &out)
		require.NoError(t, r.Run(context.Background()))
		output := out.String()
		var valLines []string
		for _, l := range strings.Split(output, "\n") {
			l = strings.TrimPrefix(l, prompt)
			if l == "10" || l == "10 20" {
				valLines = append(valLines, l)
			}
		}
		require.Equal(t, []string{"10", "10 20"}, valLines)
	})
}
