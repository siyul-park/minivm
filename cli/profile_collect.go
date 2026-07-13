package cli

import (
	"strconv"

	"github.com/siyul-park/minivm/prof"
)

type profile struct {
	total     uint64
	functions map[int]uint64
	points    map[anchor]uint64
	opcodes   map[string]uint64
	jit       jitTotals
	entries   map[entry]entryStats
	exits     map[exit]uint64
	compiles  map[compile]uint64
	captures  map[capture]uint64
}

type anchor struct{ fn, ip int }

type entry struct {
	anchor
	kind     string
	frontend string
}

type entryStats struct{ emits, bytes, entries uint64 }

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

func collect(metrics []prof.Metric) profile {
	p := profile{
		functions: map[int]uint64{},
		points:    map[anchor]uint64{},
		opcodes:   map[string]uint64{},
		entries:   map[entry]entryStats{},
		exits:     map[exit]uint64{},
		compiles:  map[compile]uint64{},
		captures:  map[capture]uint64{},
	}

	for _, metric := range metrics {
		value := uint64(metric.Value)
		switch metric.Name {
		case "vm_samples_total":
			p.total += value
		case "vm_func_samples_total":
			p.functions[metricLabelInt(metric, "func")] += value
		case "vm_func_ip_samples_total":
			key := anchor{fn: metricLabelInt(metric, "func"), ip: metricLabelInt(metric, "ip")}
			p.points[key] += value
		case "vm_opcode_samples_total":
			p.opcodes[metricLabel(metric, "opcode")] += value
		case "vm_jit_attempts_total":
			p.jit.attempts += value
		case "vm_jit_emits_total":
			p.jit.emits += value
		case "vm_jit_errors_total":
			p.jit.errors += value
		case "vm_jit_bytes_total":
			p.jit.bytes += value
		case "vm_jit_trace_captures_total":
			key := capture{
				anchor:  anchor{fn: metricLabelInt(metric, "func"), ip: metricLabelInt(metric, "ip")},
				outcome: metricLabel(metric, "outcome"), reason: metricLabel(metric, "reason"),
			}
			p.captures[key] += value
		case "vm_jit_compiles_total":
			key := compile{
				anchor:  anchor{fn: metricLabelInt(metric, "func"), ip: metricLabelInt(metric, "ip")},
				trigger: metricLabel(metric, "trigger"), frontend: metricLabel(metric, "frontend"),
				outcome: metricLabel(metric, "outcome"), reason: metricLabel(metric, "reason"),
			}
			p.compiles[key] += value
		case "vm_jit_entry_emits_total", "vm_jit_entry_bytes_total", "vm_jit_native_entries_total":
			key := entry{
				anchor: anchor{fn: metricLabelInt(metric, "func"), ip: metricLabelInt(metric, "ip")},
				kind:   metricLabel(metric, "kind"), frontend: metricLabel(metric, "frontend"),
			}
			stats := p.entries[key]
			switch metric.Name {
			case "vm_jit_entry_emits_total":
				stats.emits += value
			case "vm_jit_entry_bytes_total":
				stats.bytes += value
			case "vm_jit_native_entries_total":
				stats.entries += value
			}
			p.entries[key] = stats
		case "vm_jit_native_exits_total":
			key := exit{
				entry: entry{
					anchor: anchor{fn: metricLabelInt(metric, "func"), ip: metricLabelInt(metric, "ip")},
					kind:   metricLabel(metric, "kind"), frontend: metricLabel(metric, "frontend"),
				},
				reason: metricLabel(metric, "reason"), opcode: metricLabel(metric, "opcode"),
			}
			p.exits[key] += value
		case "vm_jit_native_yields_total":
			p.jit.yields += value
		}
	}

	return p
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
