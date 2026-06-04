package cli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/siyul-park/minivm/instr"
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
			contains: []string{"1"},
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
			contains: []string{".quit", ".reset", ".profile"},
		},
		{
			input:    ".profile\n.quit\n",
			contains: []string{"(empty)"},
		},
		{
			input: "i32.const 7\ndrop\n.profile\n.quit\n",
			contains: []string{
				"profile samples: 2",
				"functions:",
				"func 0 ips:",
				"0000\t1\t50.0%",
				"0005\t1\t50.0%",
				"opcodes:",
				"i32.const\t1\t50.0%",
				"drop\t1\t50.0%",
			},
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
			// declare a function constant without offset prefix
			input:    ".const\nfunc() i32\ni32.const 42\nreturn\n\n.show\n.quit\n",
			contains: []string{"constant 0 added.", "func() i32"},
		},
		{
			// declare a function with locals and no offset prefix
			input:    ".const\nfunc(i32) i32\ni32\ni32.const 42\nreturn\n\n.show\n.quit\n",
			contains: []string{"constant 0 added.", "func(i32) i32"},
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
			// block .type: single type
			input:    ".type\n[]i32\n\n.show\n.quit\n",
			contains: []string{"type 0 added.", "[]i32"},
		},
		{
			// block .type: struct type
			input:    ".type\nstruct {i32; f64}\n\n.show\n.quit\n",
			contains: []string{"type 0 added.", "struct {i32; f64}"},
		},
		{
			// block .type: multiple types in one block
			input:    ".type\nstruct {i32; f64}\n[]i32\n\n.show\n.quit\n",
			contains: []string{"type 0 added.", "type 1 added."},
		},
		{
			// block .type: accepts program.String() "N:\t" index prefix
			input:    ".type\n0:\tstruct {i32; f64}\n\n.show\n.quit\n",
			contains: []string{"type 0 added.", "struct {i32; f64}"},
		},
		{
			// empty .type block reports error
			input:    ".type\n\n.quit\n",
			contains: []string{"error:"},
		},
		{
			// .reset clears types
			input:    ".type\n[]i32\n\n.reset\n.show\n.quit\n",
			contains: []string{"reset.", "(empty)"},
		},
		{
			// array.new_default: KindRef persists across steps
			input:    ".type\n[]i32\n\ni32.const 1\narray.new_default 0\n.quit\n",
			excludes: []string{"error:"},
		},
		{
			// struct.new_default: KindRef persists across steps
			input:    ".type\nstruct {i32; f64}\n\nstruct.new_default 0\n.quit\n",
			excludes: []string{"error:"},
		},
		{
			// string constant accessible across steps
			input:    ".const\nfunc() i32\ni32.const 3\nreturn\n\nconst.get 0\ncall\n.quit\n",
			contains: []string{"3"},
			excludes: []string{"error:"},
		},
		{
			// i64 value shows type suffix
			input:    "i64.const 42\n.quit\n",
			contains: []string{"42 (i64)"},
			excludes: []string{"error:"},
		},
		{
			// f32 value shows type suffix
			input:    "f32.const 1.5\n.quit\n",
			contains: []string{"1.5 (f32)"},
			excludes: []string{"error:"},
		},
		{
			// f64 value shows type suffix
			input:    "f64.const 3.14\n.quit\n",
			contains: []string{"3.14 (f64)"},
			excludes: []string{"error:"},
		},
		{
			// array on stack shows element content, not raw heap index
			input:    ".type\n[]i32\n\ni32.const 3\narray.new_default 0\n.quit\n",
			contains: []string{"[]i32{"},
			excludes: []string{"error:"},
		},
		{
			// offset-prefixed absolute branch syntax works
			input:    "i32.const 0\n0005:\tbr_if @8\n.quit\n",
			excludes: []string{"error:"},
		},
		{
			// @-absolute branch syntax: br_if @8 at offset 5, rel=0, condition false → no error
			input:    "i32.const 0\nbr_if @8\n.quit\n",
			excludes: []string{"error:"},
		},
		{
			// @-absolute branch syntax: br_if @8 with hex notation
			input:    "i32.const 0\nbr_if @0x0008\n.quit\n",
			excludes: []string{"error:"},
		},
		{
			// relative branch syntax still works unchanged
			input:    "i32.const 0\nbr_if 0x0000\n.quit\n",
			excludes: []string{"error:"},
		},
		{
			// br_table also accepts @-absolute targets
			input:    "i32.const 0\nbr_table 1 @11 @11\nnop\n.quit\n",
			excludes: []string{"error:"},
		},
		{
			// out-of-range absolute target reports error
			input:    "br @0x0000\n.quit\n",
			contains: []string{"error:"},
		},

		// --- debug commands ---
		{
			// .debug with empty program
			input:    ".debug\n.quit\n",
			contains: []string{"(empty)"},
		},
		{
			// .breaks with no breakpoints set
			input:    ".breaks\n.quit\n",
			contains: []string{"no breakpoints"},
		},
		{
			// .break sets a breakpoint, .breaks lists it
			input:    ".break 0\n.breaks\n.quit\n",
			contains: []string{"breakpoint 1", "func=0 ip=0"},
		},
		{
			// .break with fn:ip notation
			input:    ".break 0:5\n.breaks\n.quit\n",
			contains: []string{"breakpoint 1", "func=0 ip=5"},
		},
		{
			// .clear removes a breakpoint
			input:    ".break 0\n.clear 1\n.breaks\n.quit\n",
			contains: []string{"no breakpoints"},
		},
		{
			// .clear nonexistent id reports error
			input:    ".clear 99\n.quit\n",
			contains: []string{"error:"},
		},
		{
			// .disable and .enable change state
			input:    ".break 0\n.disable 1\n.breaks\n.enable 1\n.breaks\n.quit\n",
			contains: []string{"disabled", "enabled"},
		},
		{
			// .reset clears breakpoints
			input:    ".break 0\n.reset\n.breaks\n.quit\n",
			contains: []string{"reset.", "no breakpoints"},
		},
		{
			// .debug stops at first instruction in step mode
			input:    "i32.const 42\n.debug\nstep\n.quit\n",
			contains: []string{"stopped at", "42"},
			excludes: []string{"error:"},
		},
		{
			// .debug quit exits session cleanly
			input:    "i32.const 42\n.debug\nquit\n.quit\n",
			contains: []string{"stopped at", "debug session ended"},
			excludes: []string{"error:"},
		},
		{
			// .debug with breakpoint hit shows breakpoint info
			input:    "i32.const 42\ni32.const 8\n.break 5\n.debug\ncontinue\nquit\n.quit\n",
			contains: []string{"breakpoint"},
			excludes: []string{"error:"},
		},
		{
			// stack command in debug sub-loop shows values
			input:    "i32.const 42\ni32.const 8\n.debug\nstep\nstack\nquit\n.quit\n",
			contains: []string{"stopped at", "42"},
			excludes: []string{"error:"},
		},
		{
			// frames command shows call stack
			input:    "i32.const 42\n.debug\nframes\nquit\n.quit\n",
			contains: []string{"frame[0]"},
			excludes: []string{"error:"},
		},
		{
			// continue in debug sub-loop runs to completion
			input:    "i32.const 42\n.debug\ncontinue\n.quit\n",
			contains: []string{"stopped at", "42"},
			excludes: []string{"error:"},
		},
		{
			// shorthand: s for step, c for continue, q for quit (two instrs needed so s stops mid-program)
			input:    "i32.const 42\ni32.const 8\n.debug\ns\nc\n.quit\n",
			contains: []string{"stopped at", "42"},
			excludes: []string{"error:"},
		},
		{
			// unknown debug command reports error without crashing
			input:    "i32.const 42\n.debug\nbadcmd\nquit\n.quit\n",
			contains: []string{"unknown debug command"},
			excludes: []string{"error:"},
		},
		{
			// globals command shows (no globals) when none set
			input:    "i32.const 42\n.debug\nglobals\nquit\n.quit\n",
			contains: []string{"(no globals)"},
			excludes: []string{"error:"},
		},
		{
			// locals command shows (no locals) at top level
			input:    "i32.const 42\n.debug\nlocals\nquit\n.quit\n",
			contains: []string{"(no locals)"},
			excludes: []string{"error:"},
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.input), func(t *testing.T) {
			var out bytes.Buffer
			r := NewREPL(strings.NewReader(tt.input), &out, nil)
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
		r := NewREPL(strings.NewReader("i32.const 1\n"), &out, nil)
		require.NoError(t, r.Run(context.Background()))
	})

	t.Run("stack accumulates bottom to top", func(t *testing.T) {
		var out bytes.Buffer
		r := NewREPL(strings.NewReader("i32.const 10\ni32.const 20\n.quit\n"), &out, nil)
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

	t.Run("profile does not mutate history", func(t *testing.T) {
		var out bytes.Buffer
		r := NewREPL(strings.NewReader("i32.const 1\n.profile\n.quit\n"), &out, nil)
		require.NoError(t, r.Run(context.Background()))
		require.Len(t, r.instrs, 1)
		require.Equal(t, 5, r.codeLen)
	})

	t.Run("profile command renders runtime errors", func(t *testing.T) {
		var out bytes.Buffer
		r := NewREPL(strings.NewReader(""), &out, nil)
		r.instrs = []instr.Instruction{instr.New(instr.DROP)}
		done, err := r.command(context.Background(), bufio.NewScanner(strings.NewReader("")), ".profile")
		require.NoError(t, err)
		require.False(t, done)
		require.Contains(t, out.String(), "error: stack underflow")
	})

	t.Run("save then load round-trips through file", func(t *testing.T) {
		memFS := newMemFS()

		var out1 bytes.Buffer
		r1 := NewREPL(
			strings.NewReader("i32.const 1\ni32.const 2\ni32.add\n.save prog.mvm\n.quit\n"),
			&out1,
			memFS,
		)
		require.NoError(t, r1.Run(context.Background()))
		require.Contains(t, out1.String(), "saved prog.mvm")
		require.Contains(t, memFS.files, "prog.mvm")

		var out2 bytes.Buffer
		r2 := NewREPL(
			strings.NewReader(".load prog.mvm\n.show\n.quit\n"),
			&out2,
			memFS,
		)
		require.NoError(t, r2.Run(context.Background()))
		require.Contains(t, out2.String(), "loaded prog.mvm")
		require.Contains(t, out2.String(), "i32.add")
		require.Equal(t, 3, len(r2.instrs))
	})

	t.Run("load replaces current state", func(t *testing.T) {
		memFS := newMemFS()
		memFS.files["replacement.mvm"] = []byte("0000:\ti32.const 0x00000063\n0005:\treturn\n")

		var out bytes.Buffer
		r := NewREPL(
			strings.NewReader("i32.const 1\ni32.const 2\n.load replacement.mvm\n.show\n.quit\n"),
			&out,
			memFS,
		)
		require.NoError(t, r.Run(context.Background()))
		output := out.String()
		require.Contains(t, output, "loaded replacement.mvm")
		require.Contains(t, output, "i32.const 0x00000063")
		require.NotContains(t, output, "i32.const 0x00000001")
		require.Equal(t, 2, len(r.instrs))
	})

	t.Run("load reports parse errors", func(t *testing.T) {
		memFS := newMemFS()
		memFS.files["broken.mvm"] = []byte("not-an-instruction xyz\n")

		var out bytes.Buffer
		r := NewREPL(strings.NewReader(".load broken.mvm\n.quit\n"), &out, memFS)
		require.NoError(t, r.Run(context.Background()))
		require.Contains(t, out.String(), "error:")
	})

	t.Run("load reports missing file", func(t *testing.T) {
		var out bytes.Buffer
		r := NewREPL(strings.NewReader(".load missing.mvm\n.quit\n"), &out, newMemFS())
		require.NoError(t, r.Run(context.Background()))
		require.Contains(t, out.String(), "error:")
	})

	t.Run("save and load require a path", func(t *testing.T) {
		var out bytes.Buffer
		r := NewREPL(strings.NewReader(".save\n.load\n.quit\n"), &out, newMemFS())
		require.NoError(t, r.Run(context.Background()))
		require.Contains(t, out.String(), "usage: .save")
		require.Contains(t, out.String(), "usage: .load")
	})
}

// memFS is a tiny in-memory WriteFS used only by the load/save tests.
// It deliberately stays self-contained instead of routing through
// fstest.MapFS to avoid forcing the production code to depend on a
// specific map representation.
type memFS struct {
	files map[string][]byte
}

func newMemFS() *memFS { return &memFS{files: map[string][]byte{}} }

func (m *memFS) Open(name string) (fs.File, error) {
	data, ok := m.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	mapFS := fstest.MapFS{name: &fstest.MapFile{Data: append([]byte(nil), data...), ModTime: time.Now()}}
	return mapFS.Open(name)
}

func (m *memFS) Create(name string) (io.WriteCloser, error) {
	return &memWriter{fs: m, name: name}, nil
}

type memWriter struct {
	fs   *memFS
	name string
	buf  bytes.Buffer
}

func (w *memWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }

func (w *memWriter) Close() error {
	w.fs.files[w.name] = w.buf.Bytes()
	return nil
}
