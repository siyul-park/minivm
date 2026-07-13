package cli

import (
	"fmt"

	"github.com/siyul-park/minivm/prof"
)

// printProfile renders normalized, ranked profiler metrics.
func (r *REPL) printProfile(metrics []prof.Metric) {
	p := newProfileReport(metrics)
	out := r.out
	fmt.Fprintf(out, "profile samples: %d\n", p.total)
	if len(p.functions) > 0 {
		fmt.Fprintln(out, "hot functions (top 10):")
		fmt.Fprintln(out, "func\tsamples\ttotal%\tnative-entries\tnative-exits\texit%")
		for _, function := range p.functions {
			fmt.Fprintf(out, "%d\t%d\t%s\t%d\t%d\t%s\n",
				function.fn, function.samples, formatPercent(function.samples, p.total),
				function.nativeEntries, function.nativeExits, formatPercent(function.nativeExits, function.nativeEntries),
			)
		}
	}

	for _, function := range p.functions {
		if len(function.ips) == 0 {
			continue
		}
		fmt.Fprintf(out, "hot ips for func %d (top 10):\n", function.fn)
		fmt.Fprintln(out, "ip\tsamples\tfunc%\tnative-kind\temits\tentries\texits")
		for _, ip := range function.ips {
			fmt.Fprintf(out, "%04d\t%d\t%s\t%s\t%d\t%d\t%d\n",
				ip.offset, ip.samples, formatPercent(ip.samples, function.samples),
				ip.nativeKind, ip.emits, ip.entries, ip.exits,
			)
		}
	}

	if len(p.opcodes) > 0 {
		fmt.Fprintln(out, "hot opcodes (top 10):")
		fmt.Fprintln(out, "opcode\tsamples\ttotal%")
		for _, opcode := range p.opcodes {
			fmt.Fprintf(out, "%s\t%d\t%s\n", opcode.name, opcode.samples, formatPercent(opcode.samples, p.total))
		}
	}

	if p.jit.empty() {
		return
	}
	fmt.Fprintln(out, "jit summary:")
	fmt.Fprintln(out, "attempts\temits\terrors\tbytes\tnative-entries\tnative-exits\tnative-yields")
	fmt.Fprintf(out, "%d\t%d\t%d\t%d\t%d\t%d\t%d\n",
		p.jit.summary.attempts,
		p.jit.summary.emits,
		p.jit.summary.errors,
		p.jit.summary.bytes,
		p.jit.summary.entries,
		p.jit.summary.exits,
		p.jit.summary.yields,
	)

	fmt.Fprintln(out, "jit entries:")
	fmt.Fprintln(out, "func\tip\tkind\tfrontend\temits\tbytes\tentries\texits\texit%")
	for _, entry := range p.jit.entries {
		fmt.Fprintf(out, "%d\t%04d\t%s\t%s\t%d\t%d\t%d\t%d\t%s\n",
			entry.fn, entry.ip, entry.kind, entry.frontend,
			entry.emits, entry.bytes, entry.entries, entry.exits, formatPercent(entry.exits, entry.entries),
		)
	}

	fmt.Fprintln(out, "jit exit reasons:")
	fmt.Fprintln(out, "func\tip\treason\topcode\tcount\tentry%")
	for _, exit := range p.jit.exits {
		fmt.Fprintf(out, "%d\t%04d\t%s\t%s\t%d\t%s\n",
			exit.fn, exit.ip, exit.reason, exit.opcode,
			exit.count, formatPercent(exit.count, exit.entries),
		)
	}

	fmt.Fprintln(out, "jit misses:")
	fmt.Fprintln(out, "func\tip\tphase\treason\tcount")
	for _, miss := range p.jit.misses {
		fmt.Fprintf(out, "%d\t%04d\t%s\t%s\t%d\n", miss.fn, miss.ip, miss.phase, miss.reason, miss.count)
	}
}

func formatPercent(value, total uint64) string {
	if total == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", float64(value)/float64(total)*100)
}
