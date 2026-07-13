package cli

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/siyul-park/minivm/prof"
	"github.com/stretchr/testify/require"
)

func TestPrintProfile(t *testing.T) {
	t.Run("normalizes and ranks lifecycle rows", func(t *testing.T) {
		metrics := []prof.Metric{
			{Name: "vm_samples_total", Value: 11},
			{Name: "vm_func_samples_total", Labels: []prof.Label{{Key: "func", Value: "2"}}, Value: 5},
			{Name: "vm_func_samples_total", Labels: []prof.Label{{Key: "func", Value: "1"}}, Value: 5},
			{Name: "vm_func_samples_total", Labels: []prof.Label{{Key: "func", Value: "3"}}, Value: 1},
			{Name: "vm_func_ip_samples_total", Labels: []prof.Label{{Key: "func", Value: "1"}, {Key: "ip", Value: "9"}}, Value: 2},
			{Name: "vm_func_ip_samples_total", Labels: []prof.Label{{Key: "func", Value: "1"}, {Key: "ip", Value: "4"}}, Value: 2},
			{Name: "vm_func_ip_samples_total", Labels: []prof.Label{{Key: "func", Value: "3"}, {Key: "ip", Value: "0"}}, Value: 1},
			{Name: "vm_opcode_samples_total", Labels: []prof.Label{{Key: "opcode", Value: "z.op"}}, Value: 5},
			{Name: "vm_opcode_samples_total", Labels: []prof.Label{{Key: "opcode", Value: "a.op"}}, Value: 5},
			{Name: "vm_jit_trace_captures_total", Labels: []prof.Label{{Key: "func", Value: "4"}, {Key: "ip", Value: "7"}, {Key: "outcome", Value: "partial"}, {Key: "reason", Value: "op-limit"}}, Value: 2},
			{Name: "vm_jit_trace_captures_total", Labels: []prof.Label{{Key: "func", Value: "4"}, {Key: "ip", Value: "7"}, {Key: "outcome", Value: "partial"}, {Key: "reason", Value: "op-limit"}}, Value: 1},
			{Name: "vm_jit_trace_captures_total", Labels: []prof.Label{{Key: "func", Value: "1"}, {Key: "ip", Value: "4"}, {Key: "outcome", Value: "published"}, {Key: "reason", Value: "none"}}, Value: 1},
			{Name: "vm_jit_compiles_total", Labels: []prof.Label{{Key: "func", Value: "4"}, {Key: "ip", Value: "7"}, {Key: "trigger", Value: "hot"}, {Key: "frontend", Value: "static"}, {Key: "outcome", Value: "empty"}, {Key: "reason", Value: "no-plan"}}, Value: 2},
			{Name: "vm_jit_entry_emits_total", Labels: []prof.Label{{Key: "func", Value: "5"}, {Key: "ip", Value: "8"}, {Key: "kind", Value: "loop"}, {Key: "frontend", Value: "trace"}}, Value: 1},
			{Name: "vm_jit_entry_bytes_total", Labels: []prof.Label{{Key: "func", Value: "5"}, {Key: "ip", Value: "8"}, {Key: "kind", Value: "loop"}, {Key: "frontend", Value: "trace"}}, Value: 64},
			{Name: "vm_jit_native_entries_total", Labels: []prof.Label{{Key: "func", Value: "1"}, {Key: "ip", Value: "4"}, {Key: "kind", Value: "start"}, {Key: "frontend", Value: "static"}}, Value: 3},
			{Name: "vm_jit_native_entries_total", Labels: []prof.Label{{Key: "func", Value: "1"}, {Key: "ip", Value: "4"}, {Key: "kind", Value: "start"}, {Key: "frontend", Value: "static"}}, Value: 1},
			{Name: "vm_jit_native_exits_total", Labels: []prof.Label{{Key: "func", Value: "1"}, {Key: "ip", Value: "4"}, {Key: "kind", Value: "start"}, {Key: "frontend", Value: "static"}, {Key: "reason", Value: "guard-value"}, {Key: "opcode", Value: "i32.div_s"}}, Value: 2},
			{Name: "vm_jit_native_yields_total", Labels: []prof.Label{{Key: "func", Value: "1"}, {Key: "ip", Value: "4"}, {Key: "kind", Value: "start"}, {Key: "frontend", Value: "static"}}, Value: 99},
		}

		var out bytes.Buffer
		printProfile(&out, metrics)

		require.Equal(t, strings.ReplaceAll(`profile samples: 11
hot functions:
func\tsamples\t%
1\t5\t45.5%
2\t5\t45.5%
3\t1\t9.1%
hot ips:
func 1 ips:
ip\tsamples\t%
0004\t2\t40.0%
0009\t2\t40.0%
func 3 ips:
ip\tsamples\t%
0000\t1\t100.0%
hot opcodes:
opcode\tsamples\t%
a.op\t5\t45.5%
z.op\t5\t45.5%
jit summary:
captures\tcompiles\temits\tbytes\tentries\texits
4\t2\t1\t64\t4\t2
jit entries:
func\tip\tkind\tfrontend\tstatus\tsamples\temits\tbytes\tentries
1\t0004\tstart\tstatic\tused\t2\t0\t0\t4
5\t0008\tloop\ttrace\temitted-unused\t0\t1\t64\t0
1\t0009\tnone\tinterpreted\tnot-attempted\t2\t0\t0\t0
3\t0000\tnone\tinterpreted\tnot-attempted\t1\t0\t0\t0
4\t0007\tnone\tstatic\tcompile-empty\t0\t0\t0\t0
jit exit reasons:
func\tip\tkind\tfrontend\treason\topcode\texits\t%
1\t0004\tstart\tstatic\tguard-value\ti32.div_s\t2\t50.0%
jit misses:
stage\tfunc\tip\ttrigger\tfrontend\toutcome\treason\tcount
capture\t4\t0007\tnone\tnone\tpartial\top-limit\t3
compile\t4\t0007\thot\tstatic\tempty\tno-plan\t2
`, `\t`, "\t"), out.String())
	})

	t.Run("keeps compile empty and zero denominator exits visible", func(t *testing.T) {
		metrics := []prof.Metric{
			{Name: "vm_samples_total", Value: 1},
			{Name: "vm_func_samples_total", Labels: []prof.Label{{Key: "func", Value: "4"}}, Value: 1},
			{Name: "vm_func_ip_samples_total", Labels: []prof.Label{{Key: "func", Value: "4"}, {Key: "ip", Value: "7"}}, Value: 1},
			{Name: "vm_jit_compiles_total", Labels: []prof.Label{{Key: "func", Value: "4"}, {Key: "ip", Value: "7"}, {Key: "trigger", Value: "hot"}, {Key: "frontend", Value: "static"}, {Key: "outcome", Value: "empty"}, {Key: "reason", Value: "no-plan"}}, Value: 1},
			{Name: "vm_jit_native_exits_total", Labels: []prof.Label{{Key: "func", Value: "8"}, {Key: "ip", Value: "3"}, {Key: "kind", Value: "loop"}, {Key: "frontend", Value: "trace"}, {Key: "reason", Value: "trace-cut"}, {Key: "opcode", Value: "none"}}, Value: 2},
			{Name: "vm_jit_native_yields_total", Labels: []prof.Label{{Key: "func", Value: "8"}, {Key: "ip", Value: "3"}, {Key: "kind", Value: "loop"}, {Key: "frontend", Value: "trace"}}, Value: 99},
		}

		var out bytes.Buffer
		printProfile(&out, metrics)
		output := out.String()

		require.Contains(t, output, "4\t0007\tnone\tstatic\tcompile-empty\t1\t0\t0\t0")
		require.Contains(t, output, "8\t0003\tloop\ttrace\ttrace-cut\tnone\t2\t-")
		require.NotContains(t, output, "99")
	})

	t.Run("limits every ranked collection to ten", func(t *testing.T) {
		var metrics []prof.Metric
		for index := 0; index < 11; index++ {
			value := float64(index + 1)
			fn := fmt.Sprint(index)
			ip := fmt.Sprint(index)
			metrics = append(metrics,
				prof.Metric{Name: "vm_func_samples_total", Labels: []prof.Label{{Key: "func", Value: fn}}, Value: value},
				prof.Metric{Name: "vm_func_ip_samples_total", Labels: []prof.Label{{Key: "func", Value: "10"}, {Key: "ip", Value: ip}}, Value: value},
				prof.Metric{Name: "vm_opcode_samples_total", Labels: []prof.Label{{Key: "opcode", Value: fmt.Sprintf("op%02d", index)}}, Value: value},
				prof.Metric{Name: "vm_jit_native_entries_total", Labels: []prof.Label{{Key: "func", Value: fn}, {Key: "ip", Value: "0"}, {Key: "kind", Value: "start"}, {Key: "frontend", Value: "static"}}, Value: value},
				prof.Metric{Name: "vm_jit_native_exits_total", Labels: []prof.Label{{Key: "func", Value: fn}, {Key: "ip", Value: "0"}, {Key: "kind", Value: "start"}, {Key: "frontend", Value: "static"}, {Key: "reason", Value: "guard-value"}, {Key: "opcode", Value: "i32.div_s"}}, Value: value},
				prof.Metric{Name: "vm_jit_compiles_total", Labels: []prof.Label{{Key: "func", Value: fn}, {Key: "ip", Value: "0"}, {Key: "trigger", Value: "hot"}, {Key: "frontend", Value: "trace"}, {Key: "outcome", Value: "empty"}, {Key: "reason", Value: "no-plan"}}, Value: value},
			)
		}

		report := newProfileReport(metrics)

		require.Len(t, report.functions, 10)
		require.Equal(t, 10, report.functions[0].fn)
		require.Equal(t, 1, report.functions[9].fn)
		require.Len(t, report.functions[0].ips, 10)
		require.Equal(t, 10, report.functions[0].ips[0].offset)
		require.Equal(t, 1, report.functions[0].ips[9].offset)
		require.Len(t, report.opcodes, 10)
		require.Equal(t, "op10", report.opcodes[0].name)
		require.Equal(t, "op01", report.opcodes[9].name)
		require.Len(t, report.jit.entries, 10)
		require.Equal(t, 10, report.jit.entries[0].fn)
		require.Equal(t, 1, report.jit.entries[9].fn)
		require.Len(t, report.jit.exits, 10)
		require.Equal(t, 10, report.jit.exits[0].fn)
		require.Equal(t, 1, report.jit.exits[9].fn)
		require.Len(t, report.jit.misses, 10)
		require.Equal(t, 10, report.jit.misses[0].fn)
		require.Equal(t, 1, report.jit.misses[9].fn)
	})
}
