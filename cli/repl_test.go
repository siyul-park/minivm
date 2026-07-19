package cli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestNewREPL(t *testing.T) {
	out := bytes.NewBuffer(nil)
	repl := NewREPL(strings.NewReader(""), out, nil)
	require.NotNil(t, repl)
	require.NoError(t, repl.Run(context.Background()))
	require.Contains(t, out.String(), "MiniVM Assembly REPL")
}

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
			excludes: []string{"profile samples:"},
		},
		{
			input: "i32.const 7\ndrop\n.profile\n.quit\n",
			contains: []string{
				"profile samples: 2",
				"hot functions (top 10):",
				"func\tsamples\ttotal%\tnative-entries\tnative-exits\texit%",
				"0\t2\t100.0%\t0\t0\t-",
				"hot ips for func 0 (top 10):",
				"ip\tsamples\tfunc%\tnative-kind\temits\tentries\texits",
				"0000\t1\t50.0%\tnone\t0\t0\t0",
				"0005\t1\t50.0%\tnone\t0\t0\t0",
				"hot opcodes (top 10):",
				"opcode\tsamples\ttotal%",
				"i32.const\t1\t50.0%",
				"drop\t1\t50.0%",
				"jit summary:",
				"attempts\temits\terrors\tbytes\tnative-entries\tnative-exits\tnative-yields",
				"0\t0\t0\t0\t0\t0\t0",
				"jit entries:",
				"func\tip\tkind\tfrontend\temits\tbytes\tentries\texits\texit%",
				"0\t0000\tnone\tinterpreted\t0\t0\t0\t0\t-",
				"0\t0005\tnone\tinterpreted\t0\t0\t0\t0\t-",
				"jit exit reasons:",
				"func\tip\treason\topcode\tcount\tentry%",
				"jit misses:",
				"func\tip\tphase\treason\tcount",
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
			// breakpoint command errors stay in the REPL
			input:    ".break\n.break bad\n.break bad:5\n.clear\n.clear bad\n.enable\n.disable\n.disable bad\n.enable 99\n.quit\n",
			contains: []string{"usage: .break", "invalid bytecode offset", "invalid function index", "usage: .clear", "invalid breakpoint id", "usage: .enable", "usage: .disable", "breakpoint 99 not found"},
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
			// debug sub-loop handles next, finish, empty line, break, clear
			input:    "i32.const 42\ni32.const 8\n.debug\n\nnext\nbreak\nbreak bad\nbreak 5\nbreaks\nclear\nclear bad\nclear 99\nclear 1\nfinish\n.quit\n",
			contains: []string{"stopped at", "usage: break", "invalid bytecode offset", "breakpoint 1", "usage: clear", "invalid breakpoint id", "breakpoint 99 not found", "breakpoint 1 cleared"},
			excludes: []string{"panic"},
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

	t.Run("profile ranks tied ips and limits them to ten", func(t *testing.T) {
		var out bytes.Buffer
		input := strings.Repeat("nop\n", 11) + ".profile\n.quit\n"
		r := NewREPL(strings.NewReader(input), &out, nil)
		require.NoError(t, r.Run(context.Background()))

		output := out.String()
		start := strings.Index(output, "hot ips for func 0 (top 10):")
		end := strings.Index(output, "hot opcodes (top 10):")
		require.NotEqual(t, -1, start)
		require.Greater(t, end, start)
		section := output[start:end]
		require.Contains(t, section, "0000\t1")
		require.Contains(t, section, "0009\t1")
		require.Less(t, strings.Index(section, "0000\t1"), strings.Index(section, "0009\t1"))
		require.NotContains(t, section, "0010\t1")
	})

	t.Run("profile excludes published partial captures from misses", func(t *testing.T) {
		metrics := []prof.Metric{
			{Name: "vm_jit_trace_captures_total", Labels: []prof.Label{
				{Key: "func", Value: "0"}, {Key: "ip", Value: "0"},
				{Key: "outcome", Value: "partial"}, {Key: "reason", Value: "op-limit"},
			}, Value: 2},
			{Name: "vm_jit_trace_captures_total", Labels: []prof.Label{
				{Key: "func", Value: "0"}, {Key: "ip", Value: "0"},
				{Key: "outcome", Value: "rejected"}, {Key: "reason", Value: "unsupported-op"},
			}, Value: 3},
		}
		var out bytes.Buffer
		r := NewREPL(strings.NewReader(""), &out, nil)
		r.showProfile(metrics)

		require.Contains(t, out.String(), "0\t0000\tcapture\tunsupported-op\t3")
		require.NotContains(t, out.String(), "capture\top-limit")
	})

	t.Run("profile renders the lifecycle output contract", func(t *testing.T) {
		// One real .profile run cannot deterministically produce compile misses,
		// unused emissions, yields, and more than ten rows together. Exercise the
		// protected human-readable contract at its owning REPL renderer boundary.
		metrics := []prof.Metric{
			{Name: "vm_jit_attempts_total", Value: 4},
			{Name: "vm_jit_emits_total", Value: 9},
			{Name: "vm_jit_bytes_total", Value: 204},
			{Name: "vm_jit_native_entries_total", Labels: []prof.Label{
				{Key: "func", Value: "1"}, {Key: "ip", Value: "1"},
				{Key: "kind", Value: "start"}, {Key: "frontend", Value: "static"},
			}, Value: 4},
			{Name: "vm_jit_native_entries_total", Labels: []prof.Label{
				{Key: "func", Value: "3"}, {Key: "ip", Value: "3"},
				{Key: "kind", Value: "call"}, {Key: "frontend", Value: "trace"},
			}, Value: 4},
			{Name: "vm_jit_native_yields_total", Labels: []prof.Label{
				{Key: "func", Value: "1"}, {Key: "ip", Value: "1"},
				{Key: "kind", Value: "start"}, {Key: "frontend", Value: "static"},
			}, Value: 99},
			{Name: "vm_jit_native_exits_total", Labels: []prof.Label{
				{Key: "func", Value: "1"}, {Key: "ip", Value: "1"},
				{Key: "kind", Value: "start"}, {Key: "frontend", Value: "static"},
				{Key: "reason", Value: "guard-value"}, {Key: "opcode", Value: "i32.div_s"},
			}, Value: 2},
			{Name: "vm_jit_native_exits_total", Labels: []prof.Label{
				{Key: "func", Value: "3"}, {Key: "ip", Value: "3"},
				{Key: "kind", Value: "call"}, {Key: "frontend", Value: "trace"},
				{Key: "reason", Value: "cold-branch"}, {Key: "opcode", Value: "br_if"},
			}, Value: 2},
			{Name: "vm_jit_compiles_total", Labels: []prof.Label{
				{Key: "func", Value: "4"}, {Key: "ip", Value: "4"},
				{Key: "trigger", Value: "hot"}, {Key: "frontend", Value: "static"},
				{Key: "outcome", Value: "empty"}, {Key: "reason", Value: "no-plan"},
			}, Value: 3},
			{Name: "vm_jit_compiles_total", Labels: []prof.Label{
				{Key: "func", Value: "4"}, {Key: "ip", Value: "4"},
				{Key: "trigger", Value: "side-exit"}, {Key: "frontend", Value: "static"},
				{Key: "outcome", Value: "empty"}, {Key: "reason", Value: "no-plan"},
			}, Value: 2},
			{Name: "vm_jit_compiles_total", Labels: []prof.Label{
				{Key: "func", Value: "40"}, {Key: "ip", Value: "40"},
				{Key: "trigger", Value: "hot"}, {Key: "frontend", Value: "static"},
				{Key: "outcome", Value: "empty"}, {Key: "reason", Value: "no-plan"},
			}, Value: 1},
		}
		for _, row := range []struct {
			fn       string
			ip       string
			kind     string
			frontend string
			bytes    float64
		}{
			{fn: "1", ip: "1", kind: "start", frontend: "static", bytes: 64},
			{fn: "2", ip: "2", kind: "loop", frontend: "trace", bytes: 32},
			{fn: "3", ip: "3", kind: "call", frontend: "trace", bytes: 48},
			{fn: "30", ip: "30", kind: "loop", frontend: "trace", bytes: 10},
			{fn: "31", ip: "31", kind: "loop", frontend: "trace", bytes: 10},
			{fn: "32", ip: "32", kind: "loop", frontend: "trace", bytes: 10},
			{fn: "33", ip: "33", kind: "loop", frontend: "trace", bytes: 10},
			{fn: "34", ip: "34", kind: "loop", frontend: "trace", bytes: 10},
			{fn: "35", ip: "35", kind: "loop", frontend: "trace", bytes: 10},
		} {
			labels := []prof.Label{
				{Key: "func", Value: row.fn}, {Key: "ip", Value: row.ip},
				{Key: "kind", Value: row.kind}, {Key: "frontend", Value: row.frontend},
			}
			metrics = append(metrics,
				prof.Metric{Name: "vm_jit_entry_emits_total", Labels: labels, Value: 1},
				prof.Metric{Name: "vm_jit_entry_bytes_total", Labels: labels, Value: row.bytes},
			)
		}
		for fn := 50; fn < 60; fn++ {
			metrics = append(metrics, prof.Metric{Name: "vm_jit_native_exits_total", Labels: []prof.Label{
				{Key: "func", Value: fmt.Sprint(fn)}, {Key: "ip", Value: "0"},
				{Key: "kind", Value: "loop"}, {Key: "frontend", Value: "trace"},
				{Key: "reason", Value: "trace-cut"}, {Key: "opcode", Value: "none"},
			}, Value: 1})
		}
		for fn := 10; fn < 20; fn++ {
			metrics = append(metrics, prof.Metric{Name: "vm_jit_trace_captures_total", Labels: []prof.Label{
				{Key: "func", Value: fmt.Sprint(fn)}, {Key: "ip", Value: "0"},
				{Key: "outcome", Value: "rejected"}, {Key: "reason", Value: "unsupported-op"},
			}, Value: 1})
		}

		var out bytes.Buffer
		r := NewREPL(strings.NewReader(""), &out, nil)
		r.showProfile(metrics)
		output := out.String()
		require.Contains(t, output, "4\t9\t0\t204\t8\t14\t99")

		entriesStart := strings.Index(output, "jit entries:")
		exitsStart := strings.Index(output, "jit exit reasons:")
		missesStart := strings.Index(output, "jit misses:")
		require.NotEqual(t, -1, entriesStart)
		require.Greater(t, exitsStart, entriesStart)
		require.Greater(t, missesStart, exitsStart)
		entries := output[entriesStart:exitsStart]
		exits := output[exitsStart:missesStart]
		misses := output[missesStart:]

		require.Contains(t, entries, "1\t0001\tstart\tstatic\t1\t64\t4\t2\t50.0%")
		require.Contains(t, entries, "3\t0003\tcall\ttrace\t1\t48\t4\t2\t50.0%")
		require.Less(t, strings.Index(entries, "1\t0001"), strings.Index(entries, "3\t0003"))
		require.Contains(t, entries, "2\t0002\tloop\ttrace\t1\t32\t0\t0\t-")
		require.Contains(t, entries, "4\t0004\tnone\tstatic\t0\t0\t0\t0\t-")
		require.NotContains(t, entries, "40\t0040")

		require.Contains(t, exits, "1\t0001\tguard-value\ti32.div_s\t2\t50.0%")
		require.Contains(t, exits, "3\t0003\tcold-branch\tbr_if\t2\t50.0%")
		require.Less(t, strings.Index(exits, "1\t0001"), strings.Index(exits, "3\t0003"))
		require.Contains(t, exits, "57\t0000\ttrace-cut\tnone\t1\t-")
		require.NotContains(t, exits, "59\t0000")

		require.Contains(t, misses, "4\t0004\tcompile-hot\tno-plan\t3")
		require.Contains(t, misses, "4\t0004\tcompile-side-exit\tno-plan\t2")
		require.Contains(t, misses, "10\t0000\tcapture\tunsupported-op\t1")
		require.Contains(t, misses, "17\t0000\tcapture\tunsupported-op\t1")
		require.Less(t, strings.Index(misses, "10\t0000"), strings.Index(misses, "17\t0000"))
		require.NotContains(t, misses, "18\t0000")
	})

	t.Run("profile does not mutate history", func(t *testing.T) {
		var out bytes.Buffer
		r := NewREPL(strings.NewReader(".type\n[]i32\n\n.const\nfunc() i32\ni32.const 3\nreturn\n\nconst.get 0\ncall\n.profile\nconst.get 0\ncall\n.show\n.quit\n"), &out, nil)
		require.NoError(t, r.Run(context.Background()))
		output := out.String()
		require.Contains(t, output, "[]i32")
		require.Contains(t, output, "func() i32")
		require.Contains(t, output, "const.get 0")
		require.Contains(t, output, "3 3")
		require.NotContains(t, output, "error:")
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
		path := filepath.Join(t.TempDir(), "prog.mvm")

		var out1 bytes.Buffer
		r1 := NewREPL(
			strings.NewReader("i32.const 1\ni32.const 2\ni32.add\n.save "+path+"\n.quit\n"),
			&out1,
			OS(),
		)
		require.NoError(t, r1.Run(context.Background()))
		require.Contains(t, out1.String(), "saved "+path)
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Contains(t, string(data), "i32.add")

		var out2 bytes.Buffer
		r2 := NewREPL(
			strings.NewReader(".load "+path+"\n.show\n.quit\n"),
			&out2,
			OS(),
		)
		require.NoError(t, r2.Run(context.Background()))
		require.Contains(t, out2.String(), "loaded "+path)
		require.Contains(t, out2.String(), "i32.add")
		require.Equal(t, 3, len(r2.instrs))
	})

	t.Run("save rejects host-only constants", func(t *testing.T) {
		values := []struct {
			value types.Value
			name  string
		}{
			{value: interp.NewHostFunction(&types.FunctionType{}, nil), name: "host function"},
			{value: &interp.HostObject{}, name: "host object"},
		}
		for _, tt := range values {
			path := filepath.Join(t.TempDir(), "host.mvm")
			var out bytes.Buffer
			r := NewREPL(strings.NewReader(".save "+path+"\n.quit\n"), &out, OS())
			r.constants = []types.Value{tt.value}

			require.NoError(t, r.Run(context.Background()))
			require.Contains(t, out.String(), "error: cannot save: constant 0 is a "+tt.name)
			_, err := os.Stat(path)
			require.ErrorIs(t, err, os.ErrNotExist)
		}
	})

	t.Run("load replaces current state", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "replacement.mvm")
		require.NoError(t, os.WriteFile(path, []byte("0000:\ti32.const 0x00000063\n0005:\treturn\n"), 0o644))

		var out bytes.Buffer
		r := NewREPL(
			strings.NewReader("i32.const 1\ni32.const 2\n.load "+path+"\n.show\n.quit\n"),
			&out,
			OS(),
		)
		require.NoError(t, r.Run(context.Background()))
		output := out.String()
		require.Contains(t, output, "loaded "+path)
		require.Contains(t, output, "i32.const 0x00000063")
		require.NotContains(t, output, "i32.const 0x00000001")
		require.Equal(t, 2, len(r.instrs))
	})

	t.Run("load reports parse errors", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "broken.mvm")
		require.NoError(t, os.WriteFile(path, []byte("not-an-instruction xyz\n"), 0o644))

		var out bytes.Buffer
		r := NewREPL(strings.NewReader(".load "+path+"\n.quit\n"), &out, OS())
		require.NoError(t, r.Run(context.Background()))
		require.Contains(t, out.String(), "error:")
	})

	t.Run("load reports missing file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.mvm")
		var out bytes.Buffer
		r := NewREPL(strings.NewReader(".load "+path+"\n.quit\n"), &out, OS())
		require.NoError(t, r.Run(context.Background()))
		require.Contains(t, out.String(), "error:")
	})

	t.Run("save and load require a path", func(t *testing.T) {
		var out bytes.Buffer
		r := NewREPL(strings.NewReader(".save\n.load\n.quit\n"), &out, OS())
		require.NoError(t, r.Run(context.Background()))
		require.Contains(t, out.String(), "usage: .save")
		require.Contains(t, out.String(), "usage: .load")
	})
}
