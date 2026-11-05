package cli

import (
	"sort"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/prof"
)

// report is the ranked view of a profile the REPL prints.
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

// rank orders c's functions, their instruction pointers, and opcodes by
// samples, ties by index or name, keeping the top profileLimit of each.
func rank(c *prof.Collector) report {
	r := report{total: c.Total()}
	for _, fn := range c.Functions() {
		row := functionRow{fn: fn, samples: c.Samples(fn)}
		for _, ip := range c.IPs(fn) {
			row.ips = append(row.ips, ipRow{offset: ip, samples: c.IP(fn, ip)})
		}
		sort.Slice(row.ips, func(i, j int) bool {
			return row.ips[i].samples > row.ips[j].samples ||
				row.ips[i].samples == row.ips[j].samples && row.ips[i].offset < row.ips[j].offset
		})
		row.ips = limit(row.ips)
		r.functions = append(r.functions, row)
	}
	sort.Slice(r.functions, func(i, j int) bool {
		return r.functions[i].samples > r.functions[j].samples ||
			r.functions[i].samples == r.functions[j].samples && r.functions[i].fn < r.functions[j].fn
	})
	r.functions = limit(r.functions)

	for code := range 256 {
		if n := c.Opcode(byte(code)); n > 0 {
			r.opcodes = append(r.opcodes, opcodeRow{name: instr.TypeOf(instr.Opcode(code)).Mnemonic, samples: n})
		}
	}
	sort.Slice(r.opcodes, func(i, j int) bool {
		return r.opcodes[i].samples > r.opcodes[j].samples ||
			r.opcodes[i].samples == r.opcodes[j].samples && r.opcodes[i].name < r.opcodes[j].name
	})
	r.opcodes = limit(r.opcodes)
	return r
}

func limit[T any](values []T) []T {
	if len(values) > profileLimit {
		return values[:profileLimit]
	}
	return values
}
