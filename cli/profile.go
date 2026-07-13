package cli

import (
	"sort"
	"strconv"

	"github.com/siyul-park/minivm/prof"
)

type profile struct {
	total           uint64
	functionSamples map[int]uint64
	pointSamples    map[anchor]uint64
	opcodeSamples   map[string]uint64
	jitCounts       jitCounts
	entryCounts     map[entry]entryCounts
	exitCounts      map[exit]uint64
	compileCounts   map[compile]uint64
	captureCounts   map[capture]uint64
}

type anchor struct{ fn, ip int }

type entry struct {
	anchor
	kind     string
	frontend string
}

type entryCounts struct{ emits, bytes, entries uint64 }

type exit struct {
	entry
	reason string
	opcode string
}

type compile struct {
	anchor
	trigger  string
	frontend string
	outcome  string
	reason   string
}

type capture struct {
	anchor
	outcome string
	reason  string
}

type jitCounts struct {
	attempts uint64
	emits    uint64
	errors   uint64
	bytes    uint64
	yields   uint64
}

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
	summary jitSummary
	entries []entryRow
	exits   []exitRow
	misses  []missRow
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

type entryRow struct {
	fn       int
	ip       int
	kind     string
	frontend string
	emits    uint64
	bytes    uint64
	entries  uint64
	exits    uint64
}

type exitRow struct {
	fn      int
	ip      int
	reason  string
	opcode  string
	count   uint64
	entries uint64
}

type missRow struct {
	fn     int
	ip     int
	phase  string
	reason string
	count  uint64
}

type anchorStats struct {
	kind    string
	emits   uint64
	entries uint64
	exits   uint64
}

const profileLimit = 10

func (p profile) report() report {
	stats := p.stats()
	return report{
		total:     p.total,
		functions: p.functions(stats),
		opcodes:   p.opcodes(),
		jit:       p.jit(stats),
	}
}

func (p jitReport) empty() bool {
	return p.summary == (jitSummary{}) && len(p.entries) == 0 && len(p.exits) == 0 && len(p.misses) == 0
}

func (p profile) functions(stats map[anchor]anchorStats) []functionRow {
	byFunction := map[int]anchorStats{}
	for key, value := range stats {
		function := byFunction[key.fn]
		function.entries += value.entries
		function.exits += value.exits
		byFunction[key.fn] = function
	}

	points := map[int][]pointRow{}
	for key, samples := range p.pointSamples {
		value := stats[key]
		kind := value.kind
		if kind == "" {
			kind = "none"
		}
		points[key.fn] = append(points[key.fn], pointRow{
			offset: key.ip, samples: samples, nativeKind: kind,
			emits: value.emits, entries: value.entries, exits: value.exits,
		})
	}

	rows := make([]functionRow, 0, len(p.functionSamples))
	for fn, samples := range p.functionSamples {
		value := byFunction[fn]
		row := functionRow{
			fn: fn, samples: samples, nativeEntries: value.entries, nativeExits: value.exits,
			ips: points[fn],
		}
		sort.Slice(row.ips, func(i, j int) bool {
			return row.ips[i].samples > row.ips[j].samples ||
				row.ips[i].samples == row.ips[j].samples && row.ips[i].offset < row.ips[j].offset
		})
		row.ips = limit(row.ips)
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].samples > rows[j].samples ||
			rows[i].samples == rows[j].samples && rows[i].fn < rows[j].fn
	})
	return limit(rows)
}

func (p profile) opcodes() []opcodeRow {
	rows := make([]opcodeRow, 0, len(p.opcodeSamples))
	for name, samples := range p.opcodeSamples {
		rows = append(rows, opcodeRow{name: name, samples: samples})
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].samples > rows[j].samples ||
			rows[i].samples == rows[j].samples && rows[i].name < rows[j].name
	})
	return limit(rows)
}

func (p profile) jit(stats map[anchor]anchorStats) jitReport {
	return jitReport{
		summary: p.summary(),
		entries: p.entries(),
		exits:   p.exits(stats),
		misses:  p.misses(),
	}
}

func (p profile) summary() jitSummary {
	summary := jitSummary{
		attempts: p.jitCounts.attempts,
		emits:    p.jitCounts.emits,
		errors:   p.jitCounts.errors,
		bytes:    p.jitCounts.bytes,
		yields:   p.jitCounts.yields,
	}
	for _, counts := range p.entryCounts {
		summary.entries += counts.entries
	}
	for _, count := range p.exitCounts {
		summary.exits += count
	}
	return summary
}

func (p profile) entries() []entryRow {
	rows := map[entry]*entryRow{}
	covered := map[anchor]bool{}
	for key, counts := range p.entryCounts {
		rows[key] = &entryRow{
			fn: key.fn, ip: key.ip, kind: key.kind, frontend: key.frontend,
			emits: counts.emits, bytes: counts.bytes, entries: counts.entries,
		}
		covered[key.anchor] = true
	}
	for key, count := range p.exitCounts {
		if row := rows[key.entry]; row != nil {
			row.exits += count
		}
	}
	for key := range p.compileCounts {
		covered[key.anchor] = true
		if key.outcome != "emitted" || key.reason != "none" {
			entry := entry{anchor: key.anchor, kind: "none", frontend: key.frontend}
			if rows[entry] == nil {
				rows[entry] = &entryRow{fn: entry.fn, ip: entry.ip, kind: entry.kind, frontend: entry.frontend}
			}
		}
	}
	for key := range p.pointSamples {
		if !covered[key] {
			entry := entry{anchor: key, kind: "none", frontend: "interpreted"}
			rows[entry] = &entryRow{fn: entry.fn, ip: entry.ip, kind: entry.kind, frontend: entry.frontend}
		}
	}

	result := make([]entryRow, 0, len(rows))
	for _, row := range rows {
		result = append(result, *row)
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		return a.entries > b.entries || a.entries == b.entries &&
			(a.emits > b.emits || a.emits == b.emits &&
				(a.fn < b.fn || a.fn == b.fn && (a.ip < b.ip || a.ip == b.ip &&
					(a.kind < b.kind || a.kind == b.kind && a.frontend < b.frontend))))
	})
	return limit(result)
}

func (p profile) exits(stats map[anchor]anchorStats) []exitRow {
	groups := map[struct {
		anchor
		reason string
		opcode string
	}]uint64{}
	for key, count := range p.exitCounts {
		group := struct {
			anchor
			reason string
			opcode string
		}{anchor: key.anchor, reason: key.reason, opcode: key.opcode}
		groups[group] += count
	}

	rows := make([]exitRow, 0, len(groups))
	for key, count := range groups {
		rows = append(rows, exitRow{
			fn: key.fn, ip: key.ip, reason: key.reason, opcode: key.opcode,
			count: count, entries: stats[key.anchor].entries,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		return a.count > b.count || a.count == b.count &&
			(a.fn < b.fn || a.fn == b.fn && (a.ip < b.ip || a.ip == b.ip &&
				(a.reason < b.reason || a.reason == b.reason && a.opcode < b.opcode)))
	})
	return limit(rows)
}

func (p profile) misses() []missRow {
	counts := map[struct {
		anchor
		phase  string
		reason string
	}]uint64{}
	for key, count := range p.compileCounts {
		if key.outcome != "emitted" || key.reason != "none" {
			miss := struct {
				anchor
				phase  string
				reason string
			}{anchor: key.anchor, phase: "compile-" + key.trigger, reason: key.reason}
			counts[miss] += count
		}
	}
	for key, count := range p.captureCounts {
		if key.outcome == "rejected" {
			miss := struct {
				anchor
				phase  string
				reason string
			}{anchor: key.anchor, phase: "capture", reason: key.reason}
			counts[miss] += count
		}
	}

	rows := make([]missRow, 0, len(counts))
	for key, count := range counts {
		rows = append(rows, missRow{fn: key.fn, ip: key.ip, phase: key.phase, reason: key.reason, count: count})
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		return a.count > b.count || a.count == b.count &&
			(a.fn < b.fn || a.fn == b.fn && (a.ip < b.ip || a.ip == b.ip &&
				(a.phase < b.phase || a.phase == b.phase && a.reason < b.reason)))
	})
	return limit(rows)
}

func (p profile) stats() map[anchor]anchorStats {
	result := map[anchor]anchorStats{}
	for key, counts := range p.entryCounts {
		stats := result[key.anchor]
		stats.kind = mergeKind(stats.kind, key.kind)
		stats.emits += counts.emits
		stats.entries += counts.entries
		result[key.anchor] = stats
	}
	for key, count := range p.exitCounts {
		stats := result[key.anchor]
		stats.kind = mergeKind(stats.kind, key.kind)
		stats.exits += count
		result[key.anchor] = stats
	}
	return result
}

func collect(metrics []prof.Metric) profile {
	p := profile{
		functionSamples: map[int]uint64{},
		pointSamples:    map[anchor]uint64{},
		opcodeSamples:   map[string]uint64{},
		entryCounts:     map[entry]entryCounts{},
		exitCounts:      map[exit]uint64{},
		compileCounts:   map[compile]uint64{},
		captureCounts:   map[capture]uint64{},
	}

	for _, metric := range metrics {
		value := uint64(metric.Value)
		switch metric.Name {
		case "vm_samples_total":
			p.total += value
		case "vm_func_samples_total":
			p.functionSamples[metricInt(metric, "func")] += value
		case "vm_func_ip_samples_total":
			p.pointSamples[metricAnchor(metric)] += value
		case "vm_opcode_samples_total":
			p.opcodeSamples[metricLabel(metric, "opcode")] += value
		case "vm_jit_attempts_total":
			p.jitCounts.attempts += value
		case "vm_jit_emits_total":
			p.jitCounts.emits += value
		case "vm_jit_errors_total":
			p.jitCounts.errors += value
		case "vm_jit_bytes_total":
			p.jitCounts.bytes += value
		case "vm_jit_trace_captures_total":
			key := capture{
				anchor:  metricAnchor(metric),
				outcome: metricLabel(metric, "outcome"),
				reason:  metricLabel(metric, "reason"),
			}
			p.captureCounts[key] += value
		case "vm_jit_compiles_total":
			key := compile{
				anchor:   metricAnchor(metric),
				trigger:  metricLabel(metric, "trigger"),
				frontend: metricLabel(metric, "frontend"),
				outcome:  metricLabel(metric, "outcome"),
				reason:   metricLabel(metric, "reason"),
			}
			p.compileCounts[key] += value
		case "vm_jit_entry_emits_total", "vm_jit_entry_bytes_total", "vm_jit_native_entries_total":
			key := metricEntry(metric)
			counts := p.entryCounts[key]
			switch metric.Name {
			case "vm_jit_entry_emits_total":
				counts.emits += value
			case "vm_jit_entry_bytes_total":
				counts.bytes += value
			case "vm_jit_native_entries_total":
				counts.entries += value
			}
			p.entryCounts[key] = counts
		case "vm_jit_native_exits_total":
			key := exit{
				entry:  metricEntry(metric),
				reason: metricLabel(metric, "reason"),
				opcode: metricLabel(metric, "opcode"),
			}
			p.exitCounts[key] += value
		case "vm_jit_native_yields_total":
			p.jitCounts.yields += value
		}
	}
	return p
}

func metricAnchor(metric prof.Metric) anchor {
	return anchor{fn: metricInt(metric, "func"), ip: metricInt(metric, "ip")}
}

func metricEntry(metric prof.Metric) entry {
	return entry{
		anchor:   metricAnchor(metric),
		kind:     metricLabel(metric, "kind"),
		frontend: metricLabel(metric, "frontend"),
	}
}

func metricLabel(metric prof.Metric, key string) string {
	for _, label := range metric.Labels {
		if label.Key == key {
			return label.Value
		}
	}
	return ""
}

func metricInt(metric prof.Metric, key string) int {
	// Metrics come from the internal collector; missing or malformed labels use
	// the zero value, matching the collector's default function and IP IDs.
	value, _ := strconv.Atoi(metricLabel(metric, key))
	return value
}

func mergeKind(current, next string) string {
	if current == "" || current == next {
		return next
	}
	return "mixed"
}

func limit[T any](values []T) []T {
	if len(values) > profileLimit {
		return values[:profileLimit]
	}
	return values
}
