package cli

import (
	"sort"
)

type report struct {
	total     uint64
	functions []functionRow
	opcodes   []opcodeRow
	jit       jitReport
}

type functionRow struct {
	fn            int
	samples       uint64
	nativeEntries uint64
	nativeExits   uint64
	ips           []pointRow
}

type pointRow struct {
	offset     int
	samples    uint64
	nativeKind string
	emits      uint64
	entries    uint64
	exits      uint64
}

type opcodeRow struct {
	name    string
	samples uint64
}

type jitReport struct {
	summary jitTotals
	entries []entryRow
	exits   []exitRow
	misses  []missRow
}

type jitTotals struct {
	attempts uint64
	emits    uint64
	errors   uint64
	bytes    uint64
	entries  uint64
	exits    uint64
	yields   uint64
}

type entryRow struct {
	entry
	emits   uint64
	bytes   uint64
	entries uint64
	exits   uint64
}

type exitReason struct {
	anchor
	reason string
	opcode string
}

type exitRow struct {
	exitReason
	count   uint64
	entries uint64
}

type miss struct {
	anchor
	phase  string
	reason string
}

type missRow struct {
	miss
	count uint64
}

type native struct {
	kind    string
	emits   uint64
	entries uint64
	exits   uint64
}

const profileLimit = 10

func (p profile) report() report {
	anchors := p.native()
	return report{
		total:     p.total,
		functions: p.functionRows(anchors),
		opcodes:   p.opcodeRows(),
		jit:       p.jitRows(anchors),
	}
}

func (p jitReport) empty() bool {
	return p.summary == (jitTotals{}) && len(p.entries) == 0 && len(p.exits) == 0 && len(p.misses) == 0
}

func (a entry) less(b entry) bool {
	return a.fn < b.fn || a.fn == b.fn && (a.ip < b.ip ||
		a.ip == b.ip && (a.kind < b.kind || a.kind == b.kind && a.frontend < b.frontend))
}

func (a missRow) less(b missRow) bool {
	return a.fn < b.fn || a.fn == b.fn && (a.ip < b.ip || a.ip == b.ip &&
		(a.phase < b.phase || a.phase == b.phase && a.reason < b.reason))
}

func (p profile) functionRows(anchors map[anchor]native) []functionRow {
	functions, ips := p.functions, p.points
	nativeByFunction := map[int]native{}
	for key, stats := range anchors {
		function := nativeByFunction[key.fn]
		function.entries += stats.entries
		function.exits += stats.exits
		nativeByFunction[key.fn] = function
	}
	ipsByFunction := map[int][]pointRow{}
	for key, count := range ips {
		stats := anchors[key]
		kind := stats.kind
		if kind == "" {
			kind = "none"
		}
		ipsByFunction[key.fn] = append(ipsByFunction[key.fn], pointRow{
			offset: key.ip, samples: count, nativeKind: kind,
			emits: stats.emits, entries: stats.entries, exits: stats.exits,
		})
	}
	report := make([]functionRow, 0, len(functions))
	for fn, samples := range functions {
		stats := nativeByFunction[fn]
		function := functionRow{
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

func (p profile) opcodeRows() []opcodeRow {
	opcodes := p.opcodes
	report := make([]opcodeRow, 0, len(opcodes))
	for name, samples := range opcodes {
		report = append(report, opcodeRow{name: name, samples: samples})
	}
	sort.Slice(report, func(i, j int) bool {
		return report[i].samples > report[j].samples ||
			report[i].samples == report[j].samples && report[i].name < report[j].name
	})
	return limit(report)
}

func (p profile) jitRows(anchors map[anchor]native) jitReport {
	summary, entryRows, exitRows := p.jit, p.entries, p.exits
	compileRows, captureRows, ips := p.compiles, p.captures, p.points
	report := jitReport{summary: summary}
	rows := map[entry]*entryRow{}
	covered := map[anchor]bool{}
	for key, stats := range entryRows {
		row := entryRow{entry: key, emits: stats.emits, bytes: stats.bytes, entries: stats.entries}
		rows[key] = &row
		report.summary.entries += stats.entries
		covered[key.anchor] = true
	}
	for key, count := range exitRows {
		report.summary.exits += count
		if entry := rows[key.entry]; entry != nil {
			entry.exits += count
		}
	}
	for key := range compileRows {
		covered[key.anchor] = true
	}
	for key := range ips {
		if !covered[key] {
			entry := entry{anchor: key, kind: "none", frontend: "interpreted"}
			rows[entry] = &entryRow{entry: entry}
		}
	}
	misses := map[miss]uint64{}
	for key, count := range compileRows {
		if key.outcome != "emitted" || key.reason != "none" {
			entry := entry{anchor: key.anchor, kind: "none", frontend: key.frontend}
			if rows[entry] == nil {
				rows[entry] = &entryRow{entry: entry}
			}
			misses[miss{anchor: key.anchor, phase: "compile-" + key.trigger, reason: key.reason}] += count
		}
	}
	for key, count := range captureRows {
		if key.outcome == "rejected" {
			misses[miss{anchor: key.anchor, phase: "capture", reason: key.reason}] += count
		}
	}
	for _, entry := range rows {
		report.entries = append(report.entries, *entry)
	}
	exitReasons := map[exitReason]*exitRow{}
	for key, count := range exitRows {
		group := exitReason{anchor: key.anchor, reason: key.reason, opcode: key.opcode}
		exit := exitReasons[group]
		if exit == nil {
			exit = &exitRow{exitReason: group}
			exitReasons[group] = exit
		}
		exit.count += count
	}
	for _, exit := range exitReasons {
		exit.entries = anchors[exit.anchor].entries
		report.exits = append(report.exits, *exit)
	}
	for key, count := range misses {
		report.misses = append(report.misses, missRow{miss: key, count: count})
	}

	sort.Slice(report.entries, func(i, j int) bool {
		a, b := report.entries[i], report.entries[j]
		return a.entries > b.entries || a.entries == b.entries &&
			(a.emits > b.emits || a.emits == b.emits && a.entry.less(b.entry))
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

func (p profile) native() map[anchor]native {
	entries, exits := p.entries, p.exits
	result := map[anchor]native{}
	for key, entry := range entries {
		anchor := key.anchor
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
		anchor := key.anchor
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

func limit[T any](values []T) []T {
	if len(values) > profileLimit {
		return values[:profileLimit]
	}
	return values
}
