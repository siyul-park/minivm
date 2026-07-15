package prof

import (
	"strconv"

	"github.com/siyul-park/minivm/instr"
)

// Collector records execution samples and named metrics.
type Collector struct {
	total   uint64
	funcs   []samples
	ops     [256]uint64
	metrics []Metric
	jit     jitMetrics
}

type samples struct {
	count uint64
	ips   []uint64
}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) Value(name string, labels ...Label) float64 {
	v, _ := c.Metric(name, labels...)
	return v
}

func (c *Collector) Metric(name string, labels ...Label) (float64, bool) {
	for _, m := range c.Metrics() {
		if m.Name == name && sameLabels(m.Labels, labels) {
			return m.Value, true
		}
	}
	return 0, false
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
	out = c.jit.appendMetrics(out, c)
	return out
}

// Add records one execution sample at (fn, ip) for op.
func (c *Collector) Add(fn, ip int, op byte) {
	if fn < 0 || ip < 0 {
		return
	}
	c.grow(fn, ip)
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
		c.grow(fn, len(fd.ips)-1)
		c.total += fd.count
		c.funcs[fn].count += fd.count
		for offset, n := range fd.ips {
			if n == 0 {
				continue
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
	c.jit.merge(&o.jit)
}

// grow ensures index fn and index ip within c.funcs[fn].ips are addressable,
// growing each slice's capacity geometrically (doubling) while sizing its
// length to exactly what's needed, so callers never scan padded trailing
// zeros the way a length-doubling grow would leave behind.
func (c *Collector) grow(fn, ip int) {
	if need := fn + 1; len(c.funcs) < need {
		if cap(c.funcs) >= need {
			c.funcs = c.funcs[:need]
		} else {
			funcs := make([]samples, need, max(need, 2*cap(c.funcs)))
			copy(funcs, c.funcs)
			c.funcs = funcs
		}
	}
	if need := ip + 1; len(c.funcs[fn].ips) < need {
		if cap(c.funcs[fn].ips) >= need {
			c.funcs[fn].ips = c.funcs[fn].ips[:need]
		} else {
			ips := make([]uint64, need, max(need, 2*cap(c.funcs[fn].ips)))
			copy(ips, c.funcs[fn].ips)
			c.funcs[fn].ips = ips
		}
	}
}

// reset clears every recorded sample while keeping the backing arrays c.funcs
// and each function's ips grew to. A Pool flushes (and so resets) its local
// collector on every Put, so nil-ing these out here would defeat the
// geometric growth in grow and force a fresh allocation on the next borrow.
func (c *Collector) reset() {
	c.total = 0
	for i := range c.funcs {
		c.funcs[i].count = 0
		clear(c.funcs[i].ips)
	}
	clear(c.ops[:])
	c.metrics = c.metrics[:0]
	c.jit.reset()
}

func (c *Collector) opcodeLabel(code byte) string {
	if typ := instr.TypeOf(instr.Opcode(code)); typ.Mnemonic != "" {
		return typ.Mnemonic
	}
	return "0x" + strconv.FormatInt(int64(code), 16)
}
