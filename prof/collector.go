package prof

import (
	"strconv"

	"github.com/siyul-park/minivm/instr"
)

// Collector records execution samples and named metrics.
type Collector struct {
	total   uint64
	funcs   []function
	ops     [256]uint64
	metrics []Metric
}

type function struct {
	count uint64
	ips   []uint64
}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Metrics() []Metric {
	out := []Metric{{Name: "vm_samples_total", Value: float64(c.total)}}
	for fn := range c.funcs {
		fd := c.funcs[fn]
		if fd.count == 0 {
			continue
		}
		fnLabel := Label{Key: "func", Value: strconv.Itoa(fn)}
		out = append(out, Metric{
			Name:   "vm_func_samples_total",
			Labels: []Label{fnLabel},
			Value:  float64(fd.count),
		})
		for offset, n := range fd.ips {
			if n == 0 {
				continue
			}
			out = append(out, Metric{
				Name:   "vm_func_ip_samples_total",
				Labels: []Label{fnLabel, {Key: "ip", Value: strconv.Itoa(offset)}},
				Value:  float64(n),
			})
		}
	}
	for code, n := range c.ops {
		if n == 0 {
			continue
		}
		out = append(out, Metric{
			Name:   "vm_opcode_samples_total",
			Labels: []Label{{Key: "opcode", Value: c.opcodeLabel(byte(code))}},
			Value:  float64(n),
		})
	}
	out = append(out, c.metrics...)
	return out
}

func (c *Collector) Metric(name string, labels ...Label) (float64, bool) {
	for _, m := range c.Metrics() {
		if m.Name == name && sameLabels(m.Labels, labels) {
			return m.Value, true
		}
	}
	return 0, false
}

func (c *Collector) Value(name string, labels ...Label) float64 {
	v, _ := c.Metric(name, labels...)
	return v
}

// Add records one execution sample at (fn, ip) for op.
func (c *Collector) Add(fn, ip int, op byte) {
	if fn < 0 || ip < 0 {
		return
	}
	for len(c.funcs) <= fn {
		c.funcs = append(c.funcs, function{})
	}
	if len(c.funcs[fn].ips) <= ip {
		ips := make([]uint64, ip+1)
		copy(ips, c.funcs[fn].ips)
		c.funcs[fn].ips = ips
	}
	c.total++
	c.funcs[fn].count++
	c.funcs[fn].ips[ip]++
	c.ops[op]++
}

func (c *Collector) AddMetric(name string, value float64, labels ...Label) {
	for i := range c.metrics {
		if c.metrics[i].Name == name && sameLabels(c.metrics[i].Labels, labels) {
			c.metrics[i].Value += value
			return
		}
	}
	c.metrics = append(c.metrics, Metric{
		Name:   name,
		Labels: append([]Label(nil), labels...),
		Value:  value,
	})
}

func (c *Collector) Total() uint64 {
	return c.total
}

func (c *Collector) Samples(fn int) uint64 {
	if fn < 0 || fn >= len(c.funcs) {
		return 0
	}
	return c.funcs[fn].count
}

func (c *Collector) IPs(fn int) []int {
	if fn < 0 || fn >= len(c.funcs) {
		return nil
	}
	var ips []int
	for offset, n := range c.funcs[fn].ips {
		if n > 0 {
			ips = append(ips, offset)
		}
	}
	return ips
}

func (c *Collector) IP(fn, ip int) uint64 {
	if fn < 0 || fn >= len(c.funcs) || ip < 0 || ip >= len(c.funcs[fn].ips) {
		return 0
	}
	return c.funcs[fn].ips[ip]
}

func (c *Collector) Opcode(code byte) uint64 {
	return c.ops[code]
}

func (c *Collector) merge(o *Collector) {
	for fn := range o.funcs {
		fd := o.funcs[fn]
		if fd.count == 0 {
			continue
		}
		for len(c.funcs) <= fn {
			c.funcs = append(c.funcs, function{})
		}
		c.total += fd.count
		c.funcs[fn].count += fd.count
		for offset, n := range fd.ips {
			if n == 0 {
				continue
			}
			if len(c.funcs[fn].ips) <= offset {
				ips := make([]uint64, offset+1)
				copy(ips, c.funcs[fn].ips)
				c.funcs[fn].ips = ips
			}
			c.funcs[fn].ips[offset] += n
		}
	}
	for code, n := range o.ops {
		c.ops[code] += n
	}
	for _, m := range o.metrics {
		c.AddMetric(m.Name, m.Value, m.Labels...)
	}
}

func (c *Collector) reset() {
	c.total = 0
	c.funcs = nil
	clear(c.ops[:])
	c.metrics = nil
}

func (c *Collector) opcodeLabel(code byte) string {
	if typ := instr.TypeOf(instr.Opcode(code)); typ.Mnemonic != "" {
		return typ.Mnemonic
	}
	return "0x" + strconv.FormatInt(int64(code), 16)
}
