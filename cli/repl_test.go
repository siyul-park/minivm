package cli_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cli "github.com/siyul-park/minivm/cli"
	"github.com/stretchr/testify/require"
)

func TestNewREPL(t *testing.T) {
	out := bytes.NewBuffer(nil)
	repl := cli.NewREPL(strings.NewReader(""), out, nil)
	require.NotNil(t, repl)
	require.NoError(t, repl.Run(context.Background()))
	require.Contains(t, out.String(), "MiniVM Assembly REPL")
}

func TestREPL_Run(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		files    map[string]string // seeded into $DIR before the session
		contains []string
		excludes []string
		wantFile map[string]string // $DIR-relative file -> substring it must contain after the session
	}{
		{
			name:     "i32 add",
			input:    "i32.const 1\ni32.const 2\ni32.add\n.quit\n",
			contains: []string{"3"}},
		{
			name:     "stack shows multiple values in order",
			input:    "i32.const 10\ni32.const 20\n.quit\n",
			contains: []string{"10 20"}},
		{
			name:     "f32 const",
			input:    "f32.const 1.0\n.quit\n",
			contains: []string{"1"},
			excludes: []string{"error:"}},
		{
			name:     "nop with empty stack omits stack line",
			input:    "nop\n.quit\n",
			excludes: []string{"stack"}},
		{
			name:     "blank lines are ignored",
			input:    "\n\ni32.const 5\n\n.quit\n",
			contains: []string{"5"}},
		{
			name:     "offset-prefixed instruction line",
			input:    "0000:\ti32.const 0x00000007\n.quit\n",
			contains: []string{"7"}},
		{
			name:     ".quit exits",
			input:    ".quit\n",
			contains: []string{"bye"}},
		{
			name:     ".exit exits",
			input:    ".exit\n",
			contains: []string{"bye"}},
		{
			name:     ".help lists commands",
			input:    ".help\n.quit\n",
			contains: []string{".quit", ".reset", ".profile"}},
		{
			name:     ".profile with no samples",
			input:    ".profile\n.quit\n",
			contains: []string{"(empty)"},
			excludes: []string{"profile samples:"}},
		{
			name:  ".profile reports samples, hot functions, opcodes",
			input: "i32.const 7\ndrop\n.profile\n.quit\n",
			contains: []string{
				"profile samples: 2",
				"hot functions (top 10):",
				"func\tsamples\ttotal%",
				"0\t2\t100.0%",
				"hot ips for func 0 (top 10):",
				"ip\tsamples\tfunc%",
				"0000\t1\t50.0%",
				"0005\t1\t50.0%",
				"hot opcodes (top 10):",
				"opcode\tsamples\ttotal%",
				"i32.const\t1\t50.0%",
				"drop\t1\t50.0%"}},
		{
			name:     ".reset clears stack",
			input:    "i32.const 42\n.reset\n.show\n.quit\n",
			contains: []string{"reset.", "(empty)"}},
		{
			name:     ".show lists program instructions",
			input:    "i32.const 1\ni32.const 2\ni32.add\n.show\n.quit\n",
			contains: []string{"i32.const", "i32.add"}},
		{
			name:     ".show after stack underflow error",
			input:    "drop\n.show\n.quit\n",
			contains: []string{"error:", "(empty)"}},
		{
			name:     "unknown REPL command",
			input:    ".unknown\n.quit\n",
			contains: []string{"unknown command"}},
		{
			name:     "invalid opcode reports error",
			input:    "bad.opcode 1\n.quit\n",
			contains: []string{"error:"}},
		{
			name: ".const block with offset-prefixed function body",
			// declare a no-arg function constant and verify .show includes it
			input:    ".const\nfunc() i32\n0000:	i32.const 0x0000002A\n0005:	return\n\n.show\n.quit\n",
			contains: []string{"constant 0 added.", "func() i32"}},
		{
			name: ".const block without offset prefix",
			// declare a function constant without offset prefix
			input:    ".const\nfunc() i32\ni32.const 42\nreturn\n\n.show\n.quit\n",
			contains: []string{"constant 0 added.", "func() i32"}},
		{
			name: ".const block with locals declaration",
			// declare a function with locals and no offset prefix
			input:    ".const\nfunc(i32) i32\ni32\ni32.const 42\nreturn\n\n.show\n.quit\n",
			contains: []string{"constant 0 added.", "func(i32) i32"}},
		{
			name: ".reset clears constants",
			// .reset clears constants
			input:    ".const\nfunc() i32\n0000:	i32.const 0x0000002A\n0005:	return\n\n.reset\n.show\n.quit\n",
			contains: []string{"reset.", "(empty)"}},
		{
			name: "empty .const block reports error",
			// empty .const block reports error
			input:    ".const\n\n.quit\n",
			contains: []string{"error:"}},
		{
			name: ".type block declares array type",
			// block .type: single type
			input:    ".type\n[]i32\n\n.show\n.quit\n",
			contains: []string{"type 0 added.", "[]i32"}},
		{
			name: ".type block declares struct type",
			// block .type: struct type
			input:    ".type\nstruct {i32; f64}\n\n.show\n.quit\n",
			contains: []string{"type 0 added.", "struct {i32; f64}"}},
		{
			name: ".type block declares multiple types",
			// block .type: multiple types in one block
			input:    ".type\nstruct {i32; f64}\n[]i32\n\n.show\n.quit\n",
			contains: []string{"type 0 added.", "type 1 added."}},
		{
			name: ".type block accepts index-prefixed type",
			// block .type: accepts program.String() "N:\t" index prefix
			input:    ".type\n0:\tstruct {i32; f64}\n\n.show\n.quit\n",
			contains: []string{"type 0 added.", "struct {i32; f64}"}},
		{
			name: "empty .type block reports error",
			// empty .type block reports error
			input:    ".type\n\n.quit\n",
			contains: []string{"error:"}},
		{
			name: ".reset clears types",
			// .reset clears types
			input:    ".type\n[]i32\n\n.reset\n.show\n.quit\n",
			contains: []string{"reset.", "(empty)"}},
		{
			name: "array.new_default keeps ref alive across steps",
			// array.new_default: KindRef persists across steps
			input:    ".type\n[]i32\n\ni32.const 1\narray.new_default 0\n.quit\n",
			excludes: []string{"error:"}},
		{
			name: "struct.new_default keeps ref alive across steps",
			// struct.new_default: KindRef persists across steps
			input:    ".type\nstruct {i32; f64}\n\nstruct.new_default 0\n.quit\n",
			excludes: []string{"error:"}},
		{
			name: "function constant callable across steps",
			// string constant accessible across steps
			input:    ".const\nfunc() i32\ni32.const 3\nreturn\n\nconst.get 0\ncall\n.quit\n",
			contains: []string{"3"},
			excludes: []string{"error:"}},
		{
			name: "i64 value shows type suffix",
			// i64 value shows type suffix
			input:    "i64.const 42\n.quit\n",
			contains: []string{"42 (i64)"},
			excludes: []string{"error:"}},
		{
			name: "f32 value shows type suffix",
			// f32 value shows type suffix
			input:    "f32.const 1.5\n.quit\n",
			contains: []string{"1.5 (f32)"},
			excludes: []string{"error:"}},
		{
			name: "f64 value shows type suffix",
			// f64 value shows type suffix
			input:    "f64.const 3.14\n.quit\n",
			contains: []string{"3.14 (f64)"},
			excludes: []string{"error:"}},
		{
			name: "array on stack shows element content",
			// array on stack shows element content, not raw heap index
			input:    ".type\n[]i32\n\ni32.const 3\narray.new_default 0\n.quit\n",
			contains: []string{"[]i32{"},
			excludes: []string{"error:"}},
		{
			name: "offset-prefixed absolute branch syntax",
			// offset-prefixed absolute branch syntax works
			input:    "i32.const 0\n0005:\tbr_if @8\n.quit\n",
			excludes: []string{"error:"}},
		{
			name: "absolute branch syntax with @ notation",
			// @-absolute branch syntax: br_if @8 at offset 5, rel=0, condition false → no error
			input:    "i32.const 0\nbr_if @8\n.quit\n",
			excludes: []string{"error:"}},
		{
			name: "absolute branch syntax with hex offset",
			// @-absolute branch syntax: br_if @8 with hex notation
			input:    "i32.const 0\nbr_if @0x0008\n.quit\n",
			excludes: []string{"error:"}},
		{
			name: "relative branch syntax unchanged",
			// relative branch syntax still works unchanged
			input:    "i32.const 0\nbr_if 0x0000\n.quit\n",
			excludes: []string{"error:"}},
		{
			name: "br_table accepts absolute targets",
			// br_table also accepts @-absolute targets
			input:    "i32.const 0\nbr_table 1 @11 @11\nnop\n.quit\n",
			excludes: []string{"error:"}},
		{
			name: "out-of-range absolute branch target errors",
			// out-of-range absolute target reports error
			input:    "br @0x0000\n.quit\n",
			contains: []string{"error:"}},

		// --- debug commands ---
		{
			name: ".debug with empty program",
			// .debug with empty program
			input:    ".debug\n.quit\n",
			contains: []string{"(empty)"}},
		{
			name: ".breaks with no breakpoints set",
			// .breaks with no breakpoints set
			input:    ".breaks\n.quit\n",
			contains: []string{"no breakpoints"}},
		{
			name: ".break sets breakpoint by function index",
			// .break sets a breakpoint, .breaks lists it
			input:    ".break 0\n.breaks\n.quit\n",
			contains: []string{"breakpoint 1", "func=0 ip=0"}},
		{
			name: ".break with fn:ip notation",
			// .break with fn:ip notation
			input:    ".break 0:5\n.breaks\n.quit\n",
			contains: []string{"breakpoint 1", "func=0 ip=5"}},
		{
			name: "breakpoint command errors stay in REPL",
			// breakpoint command errors stay in the REPL
			input:    ".break\n.break bad\n.break bad:5\n.clear\n.clear bad\n.enable\n.disable\n.disable bad\n.enable 99\n.quit\n",
			contains: []string{"usage: .break", "invalid bytecode offset", "invalid function index", "usage: .clear", "invalid breakpoint id", "usage: .enable", "usage: .disable", "breakpoint 99 not found"}},
		{
			name: ".clear removes a breakpoint",
			// .clear removes a breakpoint
			input:    ".break 0\n.clear 1\n.breaks\n.quit\n",
			contains: []string{"no breakpoints"}},
		{
			name: ".clear nonexistent id reports error",
			// .clear nonexistent id reports error
			input:    ".clear 99\n.quit\n",
			contains: []string{"error:"}},
		{
			name: ".disable and .enable toggle breakpoint state",
			// .disable and .enable change state
			input:    ".break 0\n.disable 1\n.breaks\n.enable 1\n.breaks\n.quit\n",
			contains: []string{"disabled", "enabled"}},
		{
			name: ".reset clears breakpoints",
			// .reset clears breakpoints
			input:    ".break 0\n.reset\n.breaks\n.quit\n",
			contains: []string{"reset.", "no breakpoints"}},
		{
			name: ".debug step stops at first instruction",
			// .debug stops at first instruction in step mode
			input:    "i32.const 42\n.debug\nstep\n.quit\n",
			contains: []string{"stopped at", "42"},
			excludes: []string{"error:"}},
		{
			name: ".debug quit exits session cleanly",
			// .debug quit exits session cleanly
			input:    "i32.const 42\n.debug\nquit\n.quit\n",
			contains: []string{"stopped at", "debug session ended"},
			excludes: []string{"error:"}},
		{
			name: ".debug continue stops at breakpoint",
			// .debug with breakpoint hit shows breakpoint info
			input:    "i32.const 42\ni32.const 8\n.break 5\n.debug\ncontinue\nquit\n.quit\n",
			contains: []string{"breakpoint"},
			excludes: []string{"error:"}},
		{
			name:     "debug clear disarms the active breakpoint",
			input:    "i32.const 42\ni32.const 8\n.break 5\n.debug\nclear 1\ncontinue\n.quit\n",
			contains: []string{"breakpoint 1 cleared", "debug> 42 8\n", "bye"},
			excludes: []string{"breakpoint 1 at", "unknown debug command", "error:"}},
		{
			name:     "debug preserves breakpoint ids after deletion and disabling",
			input:    "i32.const 42\ni32.const 8\n.break 0\n.break 0\n.break 5\n.clear 1\n.disable 2\n.debug\ncontinue\nquit\n.quit\n",
			contains: []string{"breakpoint 3 at func=0 ip=0005"},
			excludes: []string{"breakpoint 1 at", "breakpoint 2 at", "error:"}},
		{
			name:     "debug sessions restart at the first instruction",
			input:    "i32.const 42\ni32.const 8\n.debug\nquit\n.debug\nstack\ncontinue\n.quit\n",
			contains: []string{"debug session ended\n> stopped at func=0 ip=0000", "debug> debug> 42 8\n", "bye"},
			excludes: []string{"error:", "unknown debug command"}},
		{
			name: "debug stack command shows values",
			// stack command in debug sub-loop shows values
			input:    "i32.const 42\ni32.const 8\n.debug\nstep\nstack\nquit\n.quit\n",
			contains: []string{"stopped at", "42"},
			excludes: []string{"error:"}},
		{
			name: "debug frames command shows call stack",
			// frames command shows call stack
			input:    "i32.const 42\n.debug\nframes\nquit\n.quit\n",
			contains: []string{"frame[0]"},
			excludes: []string{"error:"}},
		{
			name: "debug continue runs to completion",
			// continue in debug sub-loop runs to completion
			input:    "i32.const 42\n.debug\ncontinue\n.quit\n",
			contains: []string{"stopped at", "42"},
			excludes: []string{"error:"}},
		{
			name: "debug shorthand commands s and c",
			// shorthand: s for step, c for continue, q for quit (two instrs needed so s stops mid-program)
			input:    "i32.const 42\ni32.const 8\n.debug\ns\nc\n.quit\n",
			contains: []string{"stopped at", "42"},
			excludes: []string{"error:"}},
		{
			name: "debug sub-loop handles next, finish, break, clear",
			// debug sub-loop handles next, finish, empty line, break, clear
			input:    "i32.const 42\ni32.const 8\n.debug\n\nnext\nbreak\nbreak bad\nbreak 5\nbreaks\nclear\nclear bad\nclear 99\nclear 1\nfinish\n.quit\n",
			contains: []string{"stopped at", "usage: break", "invalid bytecode offset", "breakpoint 1", "usage: clear", "invalid breakpoint id", "breakpoint 99 not found", "breakpoint 1 cleared"},
			excludes: []string{"panic"}},
		{
			name: "unknown debug command reports error",
			// unknown debug command reports error without crashing
			input:    "i32.const 42\n.debug\nbadcmd\nquit\n.quit\n",
			contains: []string{"unknown debug command"},
			excludes: []string{"error:"}},
		{
			name: "debug globals command with no globals",
			// globals command shows (no globals) when none set
			input:    "i32.const 42\n.debug\nglobals\nquit\n.quit\n",
			contains: []string{"(no globals)"},
			excludes: []string{"error:"}},
		{
			name: "debug locals command at top level",
			// locals command shows (no locals) at top level
			input:    "i32.const 42\n.debug\nlocals\nquit\n.quit\n",
			contains: []string{"(no locals)"},
			excludes: []string{"error:"}},
		{
			name:  "eof exits cleanly",
			input: "i32.const 1\n",
		},
		{
			name:     "stack accumulates bottom to top",
			input:    "i32.const 10\ni32.const 20\n.quit\n",
			contains: []string{"> 10\n> 10 20\n"}},
		{
			name:     "profile ranks tied ips and limits them to ten",
			input:    strings.Repeat("nop\n", 11) + ".profile\n.quit\n",
			contains: []string{"hot ips for func 0 (top 10):", "0000\t1\t9.1%\n0001\t1\t9.1%\n", "0008\t1\t9.1%\n0009\t1\t9.1%\n", "hot opcodes (top 10):"},
			excludes: []string{"0010\t1"}},
		{
			name:     "profile does not mutate history",
			input:    ".type\n[]i32\n\n.const\nfunc() i32\ni32.const 3\nreturn\n\nconst.get 0\ncall\n.profile\nconst.get 0\ncall\n.show\n.quit\n",
			contains: []string{"[]i32", "func() i32", "const.get 0", "3 3"},
			excludes: []string{"error:"}},
		{
			name:     "save then load round-trips through file",
			input:    "i32.const 1\ni32.const 2\ni32.add\n.save $DIR/prog.mvm\n.reset\n.load $DIR/prog.mvm\n.show\n.quit\n",
			contains: []string{"saved $DIR/prog.mvm", "loaded $DIR/prog.mvm", "i32.add"},
			wantFile: map[string]string{"prog.mvm": "i32.add"}},
		{
			name:     "load replaces current state",
			input:    "i32.const 1\ni32.const 2\n.load $DIR/replacement.mvm\n.show\n.quit\n",
			files:    map[string]string{"replacement.mvm": "0000:\ti32.const 0x00000063\n0005:\treturn\n"},
			contains: []string{"loaded $DIR/replacement.mvm", "i32.const 0x00000063"},
			excludes: []string{"i32.const 0x00000001"}},
		{
			name:     "load reports parse errors",
			input:    ".load $DIR/broken.mvm\n.quit\n",
			files:    map[string]string{"broken.mvm": "not-an-instruction xyz\n"},
			contains: []string{"error:"}},
		{
			name:     "load reports missing file",
			input:    ".load $DIR/missing.mvm\n.quit\n",
			contains: []string{"error:"}},
		{
			name:     "save and load require a path",
			input:    ".save\n.load\n.quit\n",
			contains: []string{"usage: .save", "usage: .load"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, data := range tt.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644))
			}
			replace := func(v string) string { return strings.ReplaceAll(v, "$DIR", filepath.ToSlash(dir)) }

			var out bytes.Buffer
			r := cli.NewREPL(strings.NewReader(replace(tt.input)), &out, cli.OS())
			require.NoError(t, r.Run(context.Background()))
			output := out.String()
			for _, s := range tt.contains {
				require.Contains(t, output, replace(s))
			}
			for _, s := range tt.excludes {
				require.NotContains(t, output, replace(s))
			}
			for name, want := range tt.wantFile {
				data, err := os.ReadFile(filepath.Join(dir, name))
				require.NoError(t, err)
				require.Contains(t, string(data), want)
			}
		})
	}
}
