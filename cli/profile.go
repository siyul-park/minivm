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
	fn            int
	samples       uint64
	nativeEntries uint64
	nativeExits   uint64
	ips           []ipSample
}

type ipSample struct {
	offset     int
	samples    uint64
	nativeKind string
	emits      uint64
	entries    uint64
	exits      uint64
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
	attempts uint64
	emits    uint64
	errors   uint64
	bytes    uint64
	entries  uint64
	exits    uint64
	yields   uint64
}

type entryKey struct {
	fn       int
	ip       int
	kind     string
	frontend string
}

type jitEntry struct {
	entryKey
	emits   uint64
	bytes   uint64
	entries uint64
	exits   uint64
}

type exitKey struct {
	entryKey
	reason string
	opcode string
}

type exitReasonKey struct {
	fn     int
	ip     int
	reason string
	opcode string
}

type jitExit struct {
	exitReasonKey
	count   uint64
	entries uint64
}

type compileKey struct {
	fn       int
	ip       int
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

type missKey struct {
	fn     int
	ip     int
	phase  string
	reason string
}

type jitMiss struct {
	missKey
	count uint64
}

type nativeAnchor struct {
	kind    string
	emits   uint64
	entries uint64
	exits   uint64
}

const profileLimit = 10

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
		case "vm_jit_attempts_total":
			report.jit.summary.attempts += value
		case "vm_jit_emits_total":
			report.jit.summary.emits += value
		case "vm_jit_errors_total":
			report.jit.summary.errors += value
		case "vm_jit_bytes_total":
			report.jit.summary.bytes += value
		case "vm_jit_trace_captures_total":
			key := captureKey{
				fn: metricLabelInt(metric, "func"), ip: metricLabelInt(metric, "ip"),
				outcome: metricLabel(metric, "outcome"), reason: metricLabel(metric, "reason"),
			}
			captures[key] += value
		case "vm_jit_compiles_total":
			key := compileKey{
				fn: metricLabelInt(metric, "func"), ip: metricLabelInt(metric, "ip"),
				frontend: metricLabel(metric, "frontend"),
				outcome:  metricLabel(metric, "outcome"), reason: metricLabel(metric, "reason"),
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
		case "vm_jit_native_yields_total":
			report.jit.summary.yields += value
		}
	}

	native := aggregateNative(entries, exits)
	report.functions = rankedFunctions(functions, ips, native)
	report.opcodes = rankedOpcodes(opcodes)
	report.jit = normalizeJIT(report.jit.summary, entries, exits, compiles, captures, ips, native)
	return report
}

// print renders the normalized, ranked profile.
func (p profileReport) print(out io.Writer) {
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

func (p jitProfile) empty() bool {
	return p.summary == (jitSummary{}) && len(p.entries) == 0 && len(p.exits) == 0 && len(p.misses) == 0
}

func rankedFunctions(functions map[int]uint64, ips map[[2]int]uint64, native map[[2]int]nativeAnchor) []functionProfile {
	nativeByFunction := map[int]nativeAnchor{}
	for key, stats := range native {
		function := nativeByFunction[key[0]]
		function.entries += stats.entries
		function.exits += stats.exits
		nativeByFunction[key[0]] = function
	}
	ipsByFunction := map[int][]ipSample{}
	for key, count := range ips {
		stats := native[key]
		kind := stats.kind
		if kind == "" {
			kind = "none"
		}
		ipsByFunction[key[0]] = append(ipsByFunction[key[0]], ipSample{
			offset: key[1], samples: count, nativeKind: kind,
			emits: stats.emits, entries: stats.entries, exits: stats.exits,
		})
	}
	report := make([]functionProfile, 0, len(functions))
	for fn, samples := range functions {
		stats := nativeByFunction[fn]
		function := functionProfile{
			fn: fn, samples: samples, nativeEntries: stats.entries, nativeExits: stats.exits,
			ips: ipsByFunction[fn],
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
	summary jitSummary,
	entryRows map[entryKey]*jitEntry,
	exitRows map[exitKey]uint64,
	compileRows map[compileKey]uint64,
	captureRows map[captureKey]uint64,
	ips map[[2]int]uint64,
	anchors map[[2]int]nativeAnchor,
) jitProfile {
	report := jitProfile{summary: summary}
	rows := map[entryKey]*jitEntry{}
	covered := map[[2]int]bool{}
	for key, entry := range entryRows {
		copy := *entry
		rows[key] = &copy
		report.summary.entries += entry.entries
		covered[[2]int{entry.fn, entry.ip}] = true
	}
	for key, count := range exitRows {
		report.summary.exits += count
		if entry := rows[key.entryKey]; entry != nil {
			entry.exits += count
		}
	}
	for key := range compileRows {
		covered[[2]int{key.fn, key.ip}] = true
	}
	for key := range ips {
		if !covered[key] {
			entryKey := entryKey{fn: key[0], ip: key[1], kind: "none", frontend: "interpreted"}
			rows[entryKey] = &jitEntry{entryKey: entryKey}
		}
	}
	misses := map[missKey]uint64{}
	for key, count := range compileRows {
		if key.outcome != "emitted" || key.reason != "none" {
			entryKey := entryKey{fn: key.fn, ip: key.ip, kind: "none", frontend: key.frontend}
			if rows[entryKey] == nil {
				rows[entryKey] = &jitEntry{entryKey: entryKey}
			}
			misses[missKey{fn: key.fn, ip: key.ip, phase: "compile", reason: key.reason}] += count
		}
	}
	for key, count := range captureRows {
		if key.outcome != "published" || key.reason != "none" {
			misses[missKey{fn: key.fn, ip: key.ip, phase: "capture", reason: key.reason}] += count
		}
	}
	for _, entry := range rows {
		report.entries = append(report.entries, *entry)
	}
	exitReasons := map[exitReasonKey]*jitExit{}
	for key, count := range exitRows {
		group := exitReasonKey{fn: key.fn, ip: key.ip, reason: key.reason, opcode: key.opcode}
		exit := exitReasons[group]
		if exit == nil {
			exit = &jitExit{exitReasonKey: group}
			exitReasons[group] = exit
		}
		exit.count += count
	}
	for _, exit := range exitReasons {
		exit.entries = anchors[[2]int{exit.fn, exit.ip}].entries
		report.exits = append(report.exits, *exit)
	}
	for key, count := range misses {
		report.misses = append(report.misses, jitMiss{missKey: key, count: count})
	}

	sort.Slice(report.entries, func(i, j int) bool {
		a, b := report.entries[i], report.entries[j]
		return a.entries > b.entries || a.entries == b.entries &&
			(a.emits > b.emits || a.emits == b.emits && entryLess(a.entryKey, b.entryKey))
	})
	sort.Slice(report.exits, func(i, j int) bool {
		a, b := report.exits[i], report.exits[j]
		return a.count > b.count || a.count == b.count &&
			(a.fn < b.fn || a.fn == b.fn && (a.ip < b.ip || a.ip == b.ip &&
				(a.reason < b.reason || a.reason == b.reason && a.opcode < b.opcode)))
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

func missLess(a, b jitMiss) bool {
	return a.fn < b.fn || a.fn == b.fn && (a.ip < b.ip || a.ip == b.ip &&
		(a.phase < b.phase || a.phase == b.phase && a.reason < b.reason))
}

func aggregateNative(entries map[entryKey]*jitEntry, exits map[exitKey]uint64) map[[2]int]nativeAnchor {
	result := map[[2]int]nativeAnchor{}
	for key, entry := range entries {
		anchor := [2]int{key.fn, key.ip}
		stats := result[anchor]
		if stats.kind == "" {
			stats.kind = key.kind
		} else if stats.kind != key.kind {
			stats.kind = "mixed"
		}
		stats.emits += entry.emits
		stats.entries += entry.entries
		result[anchor] = stats
	}
	for key, count := range exits {
		anchor := [2]int{key.fn, key.ip}
		stats := result[anchor]
		if stats.kind == "" {
			stats.kind = key.kind
		} else if stats.kind != key.kind {
			stats.kind = "mixed"
		}
		stats.exits += count
		result[anchor] = stats
	}
	return result
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
