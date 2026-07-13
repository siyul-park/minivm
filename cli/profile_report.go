package cli

import (
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

type anchorKey struct {
	fn int
	ip int
}

type entryKey struct {
	anchorKey
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
	anchorKey
	reason string
	opcode string
}

type jitExit struct {
	exitReasonKey
	count   uint64
	entries uint64
}

type compileKey struct {
	anchorKey
	trigger  string
	frontend string
	outcome  string
	reason   string
}

type captureKey struct {
	anchorKey
	outcome string
	reason  string
}

type missKey struct {
	anchorKey
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
	ips := map[anchorKey]uint64{}
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
			key := anchorKey{fn: metricLabelInt(metric, "func"), ip: metricLabelInt(metric, "ip")}
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
				anchorKey: anchorKey{fn: metricLabelInt(metric, "func"), ip: metricLabelInt(metric, "ip")},
				outcome:   metricLabel(metric, "outcome"), reason: metricLabel(metric, "reason"),
			}
			captures[key] += value
		case "vm_jit_compiles_total":
			key := compileKey{
				anchorKey: anchorKey{fn: metricLabelInt(metric, "func"), ip: metricLabelInt(metric, "ip")},
				trigger:   metricLabel(metric, "trigger"), frontend: metricLabel(metric, "frontend"),
				outcome: metricLabel(metric, "outcome"), reason: metricLabel(metric, "reason"),
			}
			compiles[key] += value
		case "vm_jit_entry_emits_total", "vm_jit_entry_bytes_total", "vm_jit_native_entries_total":
			key := entryKey{
				anchorKey: anchorKey{fn: metricLabelInt(metric, "func"), ip: metricLabelInt(metric, "ip")},
				kind:      metricLabel(metric, "kind"), frontend: metricLabel(metric, "frontend"),
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
					anchorKey: anchorKey{fn: metricLabelInt(metric, "func"), ip: metricLabelInt(metric, "ip")},
					kind:      metricLabel(metric, "kind"), frontend: metricLabel(metric, "frontend"),
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

func (p jitProfile) empty() bool {
	return p.summary == (jitSummary{}) && len(p.entries) == 0 && len(p.exits) == 0 && len(p.misses) == 0
}

func (a entryKey) less(b entryKey) bool {
	return a.fn < b.fn || a.fn == b.fn && (a.ip < b.ip ||
		a.ip == b.ip && (a.kind < b.kind || a.kind == b.kind && a.frontend < b.frontend))
}

func (a jitMiss) less(b jitMiss) bool {
	return a.fn < b.fn || a.fn == b.fn && (a.ip < b.ip || a.ip == b.ip &&
		(a.phase < b.phase || a.phase == b.phase && a.reason < b.reason))
}

func rankedFunctions(functions map[int]uint64, ips map[anchorKey]uint64, native map[anchorKey]nativeAnchor) []functionProfile {
	nativeByFunction := map[int]nativeAnchor{}
	for key, stats := range native {
		function := nativeByFunction[key.fn]
		function.entries += stats.entries
		function.exits += stats.exits
		nativeByFunction[key.fn] = function
	}
	ipsByFunction := map[int][]ipSample{}
	for key, count := range ips {
		stats := native[key]
		kind := stats.kind
		if kind == "" {
			kind = "none"
		}
		ipsByFunction[key.fn] = append(ipsByFunction[key.fn], ipSample{
			offset: key.ip, samples: count, nativeKind: kind,
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
	ips map[anchorKey]uint64,
	anchors map[anchorKey]nativeAnchor,
) jitProfile {
	report := jitProfile{summary: summary}
	rows := map[entryKey]*jitEntry{}
	covered := map[anchorKey]bool{}
	for key, entry := range entryRows {
		copy := *entry
		rows[key] = &copy
		report.summary.entries += entry.entries
		covered[entry.anchorKey] = true
	}
	for key, count := range exitRows {
		report.summary.exits += count
		if entry := rows[key.entryKey]; entry != nil {
			entry.exits += count
		}
	}
	for key := range compileRows {
		covered[key.anchorKey] = true
	}
	for key := range ips {
		if !covered[key] {
			entryKey := entryKey{anchorKey: key, kind: "none", frontend: "interpreted"}
			rows[entryKey] = &jitEntry{entryKey: entryKey}
		}
	}
	misses := map[missKey]uint64{}
	for key, count := range compileRows {
		if key.outcome != "emitted" || key.reason != "none" {
			entryKey := entryKey{anchorKey: key.anchorKey, kind: "none", frontend: key.frontend}
			if rows[entryKey] == nil {
				rows[entryKey] = &jitEntry{entryKey: entryKey}
			}
			misses[missKey{anchorKey: key.anchorKey, phase: "compile-" + key.trigger, reason: key.reason}] += count
		}
	}
	for key, count := range captureRows {
		if key.outcome == "rejected" {
			misses[missKey{anchorKey: key.anchorKey, phase: "capture", reason: key.reason}] += count
		}
	}
	for _, entry := range rows {
		report.entries = append(report.entries, *entry)
	}
	exitReasons := map[exitReasonKey]*jitExit{}
	for key, count := range exitRows {
		group := exitReasonKey{anchorKey: key.anchorKey, reason: key.reason, opcode: key.opcode}
		exit := exitReasons[group]
		if exit == nil {
			exit = &jitExit{exitReasonKey: group}
			exitReasons[group] = exit
		}
		exit.count += count
	}
	for _, exit := range exitReasons {
		exit.entries = anchors[exit.anchorKey].entries
		report.exits = append(report.exits, *exit)
	}
	for key, count := range misses {
		report.misses = append(report.misses, jitMiss{missKey: key, count: count})
	}

	sort.Slice(report.entries, func(i, j int) bool {
		a, b := report.entries[i], report.entries[j]
		return a.entries > b.entries || a.entries == b.entries &&
			(a.emits > b.emits || a.emits == b.emits && a.entryKey.less(b.entryKey))
	})
	sort.Slice(report.exits, func(i, j int) bool {
		a, b := report.exits[i], report.exits[j]
		return a.count > b.count || a.count == b.count &&
			(a.fn < b.fn || a.fn == b.fn && (a.ip < b.ip || a.ip == b.ip &&
				(a.reason < b.reason || a.reason == b.reason && a.opcode < b.opcode)))
	})
	sort.Slice(report.misses, func(i, j int) bool {
		a, b := report.misses[i], report.misses[j]
		return a.count > b.count || a.count == b.count && a.less(b)
	})
	report.entries = limit(report.entries)
	report.exits = limit(report.exits)
	report.misses = limit(report.misses)
	return report
}

func aggregateNative(entries map[entryKey]*jitEntry, exits map[exitKey]uint64) map[anchorKey]nativeAnchor {
	result := map[anchorKey]nativeAnchor{}
	for key, entry := range entries {
		anchor := key.anchorKey
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
		anchor := key.anchorKey
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

func limit[T any](values []T) []T {
	if len(values) > profileLimit {
		return values[:profileLimit]
	}
	return values
}
