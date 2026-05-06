package repl

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func run(t *testing.T, input string) string {
	t.Helper()
	var out bytes.Buffer
	r := New(strings.NewReader(input), &out)
	err := r.Run(context.Background())
	require.NoError(t, err)
	return out.String()
}

func TestREPL_BasicArithmetic(t *testing.T) {
	output := run(t, "i32.const 1\ni32.const 2\ni32.add\n.quit\n")
	require.Contains(t, output, "3")
}

func TestREPL_StackAccumulates(t *testing.T) {
	output := run(t, "i32.const 10\ni32.const 20\n.quit\n")
	// Each instruction prints a line with current stack values (bottom-to-top)
	lines := nonEmpty(strings.Split(output, "\n"))
	// Find the value output lines (not banner/prompt/bye lines)
	var valLines []string
	for _, l := range lines {
		l = strings.TrimPrefix(l, prompt)
		if l == "10" || l == "10 20" {
			valLines = append(valLines, l)
		}
	}
	require.Equal(t, []string{"10", "10 20"}, valLines)
}

func nonEmpty(lines []string) []string {
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func TestREPL_Reset(t *testing.T) {
	output := run(t, "i32.const 42\n.reset\n.quit\n")
	require.Contains(t, output, "reset.")
	output2 := run(t, "i32.const 42\n.reset\n.show\n.quit\n")
	require.Contains(t, output2, "(empty)")
}

func TestREPL_Show(t *testing.T) {
	output := run(t, "i32.const 1\ni32.const 2\ni32.add\n.show\n.quit\n")
	require.Contains(t, output, "i32.const")
	require.Contains(t, output, "i32.add")
}

func TestREPL_ErrorRejectsInstruction(t *testing.T) {
	output := run(t, "drop\n.show\n.quit\n")
	require.Contains(t, output, "error:")
	require.Contains(t, output, "(empty)")
}

func TestREPL_EmptyStackSilent(t *testing.T) {
	// nop produces no stack output (stack empty)
	output := run(t, "nop\n.quit\n")
	require.NotContains(t, output, "stack")
}

func TestREPL_UnknownMeta(t *testing.T) {
	output := run(t, ".unknown\n.quit\n")
	require.Contains(t, output, "unknown command")
}

func TestREPL_Help(t *testing.T) {
	output := run(t, ".help\n.quit\n")
	require.Contains(t, output, ".quit")
	require.Contains(t, output, ".reset")
}

func TestREPL_QuitClean(t *testing.T) {
	output := run(t, ".quit\n")
	require.Contains(t, output, "bye")
}

func TestREPL_ExitClean(t *testing.T) {
	output := run(t, ".exit\n")
	require.Contains(t, output, "bye")
}

func TestREPL_BlankLinesIgnored(t *testing.T) {
	output := run(t, "\n\ni32.const 5\n\n.quit\n")
	require.Contains(t, output, "5")
}

func TestREPL_UnknownMnemonic(t *testing.T) {
	output := run(t, "bad.opcode 1\n.quit\n")
	require.Contains(t, output, "error:")
}

func TestREPL_OffsetPrefixAccepted(t *testing.T) {
	output := run(t, "0000:\ti32.const 0x00000007\n.quit\n")
	require.Contains(t, output, "7")
}

func TestREPL_EOFClean(t *testing.T) {
	// EOF without .quit should also return nil
	var out bytes.Buffer
	r := New(strings.NewReader("i32.const 1\n"), &out)
	err := r.Run(context.Background())
	require.NoError(t, err)
}

func TestREPL_F32Const(t *testing.T) {
	output := run(t, "f32.const 1.0\n.quit\n")
	require.Contains(t, output, "1.000000")
	require.NotContains(t, output, "error:")
}
