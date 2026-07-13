package cli

import (
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/siyul-park/minivm/prof"
)

type profileReport struct {
	total     uint64
	functions []functionProfile
	opcodes   []opcodeSample
	jit       jitProfile
}

type functionProfile struct {
	fn      int
	samples uint64
	ips     []ipSample
}

type ipSample struct {
	offset  int
	samples uint64
}

type opcodeSample struct {
	name    string
	samples uint64
}

type jitProfile struct {
	summary jitSummary
	entries []jitEntry
	exits   []jitExit
	misses  []jitMiss
}

type jitSummary struct {
	captures uint64
	compiles uint64
	emits    uint64
	bytes    uint64
	entries  uint64
	exits    uint64
}

type entryKey struct {
	fn       int
	ip       int
	kind     string
	frontend string
}

type jitEntry struct {
	entryKey
	status  string
	samples uint64
	emits   uint64
	bytes   uint64
	entries uint64
}

type exitKey struct {
	entryKey
	reason string
	opcode string
}

type jitExit struct {
	exitKey
	count   uint64
	entries uint64
}

type compileKey struct {
	fn       int
	ip       int
	trigger  string
	frontend string
	outcome  string
	reason   string
}

type captureKey struct {
	fn      int
	ip      int
	outcome string
	reason  string
}

type jitMiss struct {
	stage    string
	fn       int
	ip       int
	trigger  string
	frontend string
	outcome  string
	reason   string
	count    uint64
}

const profileLimit = 10

func (p jitProfile) empty() bool {
	return p.summary == (jitSummary{}) && len(p.entries) == 0 && len(p.exits) == 0 && len(p.misses) == 0
}

// printProfile renders a normalized, ranked view of profiler metrics.
func printProfile(out io.Writer, metrics []prof.Metric) {
	report := newProfileReport(metrics)

	fmt.Fprintf(out, "profile samples: %d\n", report.total)
	if len(report.functions) > 0 {
		fmt.Fprintln(out, "hot functions:")
		fmt.Fprintln(out, "func\tsamples\t%")
		for _, function := range report.functions {
			fmt.Fprintf(out, "%d\t%d\t%s\n", function.fn, function.samples, formatPercent(function.samples, report.total))
		}
	}

	hasIPs := false
	for _, function := range report.functions {
		hasIPs = hasIPs || len(function.ips) > 0
	}
	if hasIPs {
		fmt.Fprintln(out, "hot ips:")
		for _, function := range report.functions {
			if len(function.ips) == 0 {
				continue
			}
			fmt.Fprintf(out, "func %d ips:\n", function.fn)
			fmt.Fprintln(out, "ip\tsamples\t%")
			for _, ip := range function.ips {
				fmt.Fprintf(out, "%04d\t%d\t%s\n", ip.offset, ip.samples, formatPercent(ip.samples, function.samples))
			}
		}
	}

	if len(report.opcodes) > 0 {
		fmt.Fprintln(out, "hot opcodes:")
		fmt.Fprintln(out, "opcode\tsamples\t%")
		for _, opcode := range report.opcodes {
			fmt.Fprintf(out, "%s\t%d\t%s\n", opcode.name, opcode.samples, formatPercent(opcode.samples, report.total))
		}
	}

	if report.jit.empty() {
		return
	}
	fmt.Fprintln(out, "jit summary:")
	fmt.Fprintln(out, "captures\tcompiles\temits\tbytes\tentries\texits")
	fmt.Fprintf(out, "%d\t%d\t%d\t%d\t%d\t%d\n",
		report.jit.summary.captures,
		report.jit.summary.compiles,
		report.jit.summary.emits,
		report.jit.summary.bytes,
		report.jit.summary.entries,
		report.jit.summary.exits,
	)

	fmt.Fprintln(out, "jit entries:")
	fmt.Fprintln(out, "func\tip\tkind\tfrontend\tstatus\tsamples\temits\tbytes\tentries")
	for _, entry := range report.jit.entries {
		fmt.Fprintf(out, "%d\t%04d\t%s\t%s\t%s\t%d\t%d\t%d\t%d\n",
			entry.fn, entry.ip, entry.kind, entry.frontend, entry.status,
			entry.samples, entry.emits, entry.bytes, entry.entries,
		)
	}

	fmt.Fprintln(out, "jit exit reasons:")
	fmt.Fprintln(out, "func\tip\tkind\tfrontend\treason\topcode\texits\t%")
	for _, exit := range report.jit.exits {
		fmt.Fprintf(out, "%d\t%04d\t%s\t%s\t%s\t%s\t%d\t%s\n",
			exit.fn, exit.ip, exit.kind, exit.frontend, exit.reason, exit.opcode,
			exit.count, formatPercent(exit.count, exit.entries),
		)
	}

	fmt.Fprintln(out, "jit misses:")
	fmt.Fprintln(out, "stage\tfunc\tip\ttrigger\tfrontend\toutcome\treason\tcount")
	for _, miss := range report.jit.misses {
		fmt.Fprintf(out, "%s\t%d\t%04d\t%s\t%s\t%s\t%s\t%d\n",
			miss.stage, miss.fn, miss.ip, miss.trigger, miss.frontend,
			miss.outcome, miss.reason, miss.count,
		)
	}
}

func newProfileReport(metrics []prof.Metric) profileReport {
	functions := map[int]uint64{}
	ips := map[[2]int]uint64{}
	opcodes := map[string]uint64{}
	entries := map[entryKey]*jitEntry{}
	exits := map[exitKey]uint64{}
	compiles := map[compileKey]uint64{}
	captures := map[captureKey]uint64{}
	var report profileReport

	for _, metric := range metrics {
		value := uint64(metric.Value)
		switch metric.Name {
		case "vm_samples_total":
			report.total += value
		case "vm_func_samples_total":
			functions[metricLabelInt(metric, "func")] += value
		case "vm_func_ip_samples_total":
			key := [2]int{metricLabelInt(metric, "func"), metricLabelInt(metric, "ip")}
			ips[key] += value
		case "vm_opcode_samples_total":
			opcodes[metricLabel(metric, "opcode")] += value
		case "vm_jit_trace_captures_total":
			key := captureKey{
				fn: metricLabelInt(metric, "func"), ip: metricLabelInt(metric, "ip"),
				outcome: metricLabel(metric, "outcome"), reason: metricLabel(metric, "reason"),
			}
			captures[key] += value
		case "vm_jit_compiles_total":
			key := compileKey{
				fn: metricLabelInt(metric, "func"), ip: metricLabelInt(metric, "ip"),
				trigger: metricLabel(metric, "trigger"), frontend: metricLabel(metric, "frontend"),
				outcome: metricLabel(metric, "outcome"), reason: metricLabel(metric, "reason"),
			}
			compiles[key] += value
		case "vm_jit_entry_emits_total", "vm_jit_entry_bytes_total", "vm_jit_native_entries_total":
			key := entryKey{
				fn: metricLabelInt(metric, "func"), ip: metricLabelInt(metric, "ip"),
				kind: metricLabel(metric, "kind"), frontend: metricLabel(metric, "frontend"),
			}
			entry := entries[key]
			if entry == nil {
				entry = &jitEntry{entryKey: key}
				entries[key] = entry
			}
			switch metric.Name {
			case "vm_jit_entry_emits_total":
				entry.emits += value
			case "vm_jit_entry_bytes_total":
				entry.bytes += value
			case "vm_jit_native_entries_total":
				entry.entries += value
			}
		case "vm_jit_native_exits_total":
			key := exitKey{
				entryKey: entryKey{
					fn: metricLabelInt(metric, "func"), ip: metricLabelInt(metric, "ip"),
					kind: metricLabel(metric, "kind"), frontend: metricLabel(metric, "frontend"),
				},
				reason: metricLabel(metric, "reason"), opcode: metricLabel(metric, "opcode"),
			}
			exits[key] += value
		}
	}

	report.functions = rankedFunctions(functions, ips)
	report.opcodes = rankedOpcodes(opcodes)
	report.jit = normalizeJIT(entries, exits, compiles, captures, ips)
	return report
}

func rankedFunctions(functions map[int]uint64, ips map[[2]int]uint64) []functionProfile {
	report := make([]functionProfile, 0, len(functions))
	for fn, samples := range functions {
		function := functionProfile{fn: fn, samples: samples}
		for key, count := range ips {
			if key[0] == fn {
				function.ips = append(function.ips, ipSample{offset: key[1], samples: count})
			}
		}
		sort.Slice(function.ips, func(i, j int) bool {
			return function.ips[i].samples > function.ips[j].samples ||
				function.ips[i].samples == function.ips[j].samples && function.ips[i].offset < function.ips[j].offset
		})
		function.ips = limit(function.ips)
		report = append(report, function)
	}
	sort.Slice(report, func(i, j int) bool {
		return report[i].samples > report[j].samples ||
			report[i].samples == report[j].samples && report[i].fn < report[j].fn
	})
	return limit(report)
}

func rankedOpcodes(opcodes map[string]uint64) []opcodeSample {
	report := make([]opcodeSample, 0, len(opcodes))
	for name, samples := range opcodes {
		report = append(report, opcodeSample{name: name, samples: samples})
	}
	sort.Slice(report, func(i, j int) bool {
		return report[i].samples > report[j].samples ||
			report[i].samples == report[j].samples && report[i].name < report[j].name
	})
	return limit(report)
}

func normalizeJIT(
	entryRows map[entryKey]*jitEntry,
	exitRows map[exitKey]uint64,
	compileRows map[compileKey]uint64,
	captureRows map[captureKey]uint64,
	ips map[[2]int]uint64,
) jitProfile {
	var report jitProfile
	covered := map[[2]int]bool{}
	for _, entry := range entryRows {
		entry.samples = ips[[2]int{entry.fn, entry.ip}]
		if entry.entries > 0 {
			entry.status = "used"
		} else {
			entry.status = "emitted-unused"
		}
		report.summary.emits += entry.emits
		report.summary.bytes += entry.bytes
		report.summary.entries += entry.entries
		report.entries = append(report.entries, *entry)
		covered[[2]int{entry.fn, entry.ip}] = true
	}
	for key := range compileRows {
		covered[[2]int{key.fn, key.ip}] = true
	}
	for key, samples := range ips {
		if !covered[key] {
			report.entries = append(report.entries, jitEntry{
				entryKey: entryKey{fn: key[0], ip: key[1], kind: "none", frontend: "interpreted"},
				status:   "not-attempted", samples: samples,
			})
		}
	}
	for _, count := range captureRows {
		report.summary.captures += count
	}
	for key, count := range compileRows {
		report.summary.compiles += count
		matched := false
		for entry := range entryRows {
			if entry.fn == key.fn && entry.ip == key.ip && entry.frontend == key.frontend {
				matched = true
				break
			}
		}
		if !matched {
			report.entries = append(report.entries, jitEntry{
				entryKey: entryKey{fn: key.fn, ip: key.ip, kind: "none", frontend: key.frontend},
				status:   "compile-" + key.outcome,
				samples:  ips[[2]int{key.fn, key.ip}],
			})
		}
		if key.outcome != "emitted" || key.reason != "none" {
			report.misses = append(report.misses, jitMiss{
				stage: "compile", fn: key.fn, ip: key.ip, trigger: key.trigger,
				frontend: key.frontend, outcome: key.outcome, reason: key.reason, count: count,
			})
		}
	}
	for key, count := range captureRows {
		if key.outcome != "published" || key.reason != "none" {
			report.misses = append(report.misses, jitMiss{
				stage: "capture", fn: key.fn, ip: key.ip, trigger: "none",
				frontend: "none", outcome: key.outcome, reason: key.reason, count: count,
			})
		}
	}
	for key, count := range exitRows {
		entries := uint64(0)
		if entry := entryRows[key.entryKey]; entry != nil {
			entries = entry.entries
		}
		report.summary.exits += count
		report.exits = append(report.exits, jitExit{exitKey: key, count: count, entries: entries})
	}

	sort.Slice(report.entries, func(i, j int) bool {
		a, b := report.entries[i], report.entries[j]
		return a.entries > b.entries ||
			a.entries == b.entries && (a.emits > b.emits ||
				a.emits == b.emits && (entryLess(a.entryKey, b.entryKey) ||
					a.entryKey == b.entryKey && a.status < b.status))
	})
	sort.Slice(report.exits, func(i, j int) bool {
		a, b := report.exits[i], report.exits[j]
		return a.count > b.count || a.count == b.count && exitLess(a.exitKey, b.exitKey)
	})
	sort.Slice(report.misses, func(i, j int) bool {
		a, b := report.misses[i], report.misses[j]
		return a.count > b.count || a.count == b.count && missLess(a, b)
	})
	report.entries = limit(report.entries)
	report.exits = limit(report.exits)
	report.misses = limit(report.misses)
	return report
}

func entryLess(a, b entryKey) bool {
	return a.fn < b.fn || a.fn == b.fn && (a.ip < b.ip ||
		a.ip == b.ip && (a.kind < b.kind || a.kind == b.kind && a.frontend < b.frontend))
}

func exitLess(a, b exitKey) bool {
	return entryLess(a.entryKey, b.entryKey) || a.entryKey == b.entryKey &&
		(a.reason < b.reason || a.reason == b.reason && a.opcode < b.opcode)
}

func missLess(a, b jitMiss) bool {
	return a.stage < b.stage || a.stage == b.stage &&
		(a.fn < b.fn || a.fn == b.fn &&
			(a.ip < b.ip || a.ip == b.ip &&
				(a.trigger < b.trigger || a.trigger == b.trigger &&
					(a.frontend < b.frontend || a.frontend == b.frontend &&
						(a.outcome < b.outcome || a.outcome == b.outcome && a.reason < b.reason)))))
}

func metricLabel(metric prof.Metric, key string) string {
	for _, label := range metric.Labels {
		if label.Key == key {
			return label.Value
		}
	}
	return ""
}

func metricLabelInt(metric prof.Metric, key string) int {
	value, _ := strconv.Atoi(metricLabel(metric, key))
	return value
}

func formatPercent(value, total uint64) string {
	if total == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", float64(value)/float64(total)*100)
}

func limit[T any](values []T) []T {
	if len(values) > profileLimit {
		return values[:profileLimit]
	}
	return values
}
