package cli

import (
	"sort"
	"strconv"

	"github.com/siyul-park/minivm/prof"
)

type profile struct {
	total           uint64
	functionSamples map[int]uint64
	ipSamples       map[anchor]uint64
	opcodeSamples   map[string]uint64
}

type anchor struct{ fn, ip int }

type report struct {
	total     uint64
	functions []functionRow
	opcodes   []opcodeRow
}

type functionRow struct {
	fn      int
	samples uint64
	ips     []ipRow
}

type ipRow struct {
	offset  int
	samples uint64
}

type opcodeRow struct {
	name    string
	samples uint64
}

const profileLimit = 10

func (p profile) report() report {
	return report{
		total:     p.total,
		functions: p.functions(),
		opcodes:   p.opcodes(),
	}
}

func (p profile) functions() []functionRow {
	points := map[int][]ipRow{}
	for key, samples := range p.ipSamples {
		points[key.fn] = append(points[key.fn], ipRow{offset: key.ip, samples: samples})
	}
	rows := make([]functionRow, 0, len(p.functionSamples))
	for fn, samples := range p.functionSamples {
		row := functionRow{fn: fn, samples: samples, ips: points[fn]}
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

func collect(metrics []prof.Metric) profile {
	p := profile{
		functionSamples: map[int]uint64{},
		ipSamples:       map[anchor]uint64{},
		opcodeSamples:   map[string]uint64{},
	}
	for _, metric := range metrics {
		value := uint64(metric.Value)
		switch metric.Name {
		case "vm_samples_total":
			p.total += value
		case "vm_func_samples_total":
			p.functionSamples[metricInt(metric, "func")] += value
		case "vm_func_ip_samples_total":
			p.ipSamples[metricAnchor(metric)] += value
		case "vm_opcode_samples_total":
			p.opcodeSamples[metricLabel(metric, "opcode")] += value
		}
	}
	return p
}

func metricAnchor(metric prof.Metric) anchor {
	return anchor{fn: metricInt(metric, "func"), ip: metricInt(metric, "ip")}
}

func metricInt(metric prof.Metric, key string) int {
	value, _ := strconv.Atoi(metricLabel(metric, key))
	return value
}

func metricLabel(metric prof.Metric, key string) string {
	for _, label := range metric.Labels {
		if label.Key == key {
			return label.Value
		}
	}
	return ""
}

func limit[T any](values []T) []T {
	if len(values) > profileLimit {
		return values[:profileLimit]
	}
	return values
}
