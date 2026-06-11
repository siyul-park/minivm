package prof

// Snapshot is an immutable copy of collected profile data.
type Snapshot struct {
	Samples uint64
	Funcs   []Func
	Opcodes []Opcode
	JIT     JIT
}

// Func holds sample data for one function.
type Func struct {
	Index   int
	Samples uint64
	Percent float64
	IPs     []IP
}

// IP holds sample data for one instruction offset.
type IP struct {
	Offset  int
	Samples uint64
	Percent float64
}

// Opcode holds sample data for one opcode byte.
type Opcode struct {
	Code    byte
	Samples uint64
	Percent float64
}

// JIT holds aggregate JIT compilation counters.
type JIT struct {
	Attempts uint64
	Emits    uint64
	Links    uint64
	Skips    uint64
	Errors   uint64
	Bytes    uint64
}

// Stats records execution-frequency data during bytecode interpretation.
// It grows automatically to accommodate any function index and instruction pointer.
type Stats struct {
	samples uint64
	funcs   []funcData
	opcodes [256]uint64
	jit     JIT
}

type funcData struct {
	samples uint64
	ips     []uint64
}

// New returns an empty Stats.
func New() *Stats {
	return &Stats{}
}

// Add records one execution sample at (fn, ip) for op.
// It automatically grows to accommodate new function indices and IPs.
func (p *Stats) Add(fn, ip int, op byte) {
	if fn < 0 || ip < 0 {
		return
	}
	for len(p.funcs) <= fn {
		p.funcs = append(p.funcs, funcData{})
	}
	if len(p.funcs[fn].ips) <= ip {
		ips := make([]uint64, ip+1)
		copy(ips, p.funcs[fn].ips)
		p.funcs[fn].ips = ips
	}
	p.samples++
	p.funcs[fn].samples++
	p.funcs[fn].ips[ip]++
	p.opcodes[op]++
}

// Samples returns the sample count for fn.
// Returns 0 for an unknown function index.
func (p *Stats) Samples(fn int) uint64 {
	if fn < 0 || fn >= len(p.funcs) {
		return 0
	}
	return p.funcs[fn].samples
}

// Func returns a copy of the profile data for fn.
func (p *Stats) Func(fn int) Func {
	if fn < 0 || fn >= len(p.funcs) {
		return Func{Index: fn}
	}
	return p.funcData(fn, p.samples)
}

// IP returns the profile data for fn at ip.
func (p *Stats) IP(fn, ip int) IP {
	if fn < 0 || fn >= len(p.funcs) || ip < 0 || ip >= len(p.funcs[fn].ips) {
		return IP{Offset: ip}
	}
	return IP{
		Offset:  ip,
		Samples: p.funcs[fn].ips[ip],
		Percent: percent(p.funcs[fn].ips[ip], p.funcs[fn].samples),
	}
}

// Range returns the sum of samples for fn over [start, end).
// Returns 0 for an unknown function index.
func (p *Stats) Range(fn, start, end int) uint64 {
	if fn < 0 || fn >= len(p.funcs) || start >= end {
		return 0
	}
	if start < 0 {
		start = 0
	}
	ips := p.funcs[fn].ips
	var total uint64
	for ip := start; ip < end && ip < len(ips); ip++ {
		total += ips[ip]
	}
	return total
}

// Snapshot returns an immutable deep copy of collected profile data.
func (p *Stats) Snapshot() Snapshot {
	funcs := make([]Func, len(p.funcs))
	for i := range p.funcs {
		funcs[i] = p.funcData(i, p.samples)
	}

	opcodes := make([]Opcode, 0, len(p.opcodes))
	for code, samples := range p.opcodes {
		if samples == 0 {
			continue
		}
		opcodes = append(opcodes, Opcode{
			Code:    byte(code),
			Samples: samples,
			Percent: percent(samples, p.samples),
		})
	}

	return Snapshot{
		Samples: p.samples,
		Funcs:   funcs,
		Opcodes: opcodes,
		JIT:     p.jit,
	}
}

// Merge adds snapshot data into p.
func (p *Stats) Merge(s Snapshot) {
	for _, fn := range s.Funcs {
		if fn.Index < 0 || fn.Samples == 0 {
			continue
		}
		for len(p.funcs) <= fn.Index {
			p.funcs = append(p.funcs, funcData{})
		}
		p.samples += fn.Samples
		p.funcs[fn.Index].samples += fn.Samples
		for _, ip := range fn.IPs {
			if ip.Offset < 0 || ip.Samples == 0 {
				continue
			}
			if len(p.funcs[fn.Index].ips) <= ip.Offset {
				ips := make([]uint64, ip.Offset+1)
				copy(ips, p.funcs[fn.Index].ips)
				p.funcs[fn.Index].ips = ips
			}
			p.funcs[fn.Index].ips[ip.Offset] += ip.Samples
		}
	}
	for _, op := range s.Opcodes {
		p.opcodes[op.Code] += op.Samples
	}
	p.JITAdd(s.JIT)
}

// Reset clears all collected profile data.
func (p *Stats) Reset() {
	p.samples = 0
	p.funcs = nil
	clear(p.opcodes[:])
	p.jit = JIT{}
}

// JITAdd merges d into the aggregate JIT counters.
func (p *Stats) JITAdd(d JIT) {
	p.jit.Attempts += d.Attempts
	p.jit.Emits += d.Emits
	p.jit.Links += d.Links
	p.jit.Skips += d.Skips
	p.jit.Errors += d.Errors
	p.jit.Bytes += d.Bytes
}

func (p *Stats) funcData(fn int, total uint64) Func {
	f := p.funcs[fn]
	ips := make([]IP, 0, len(f.ips))
	for offset, samples := range f.ips {
		if samples == 0 {
			continue
		}
		ips = append(ips, IP{
			Offset:  offset,
			Samples: samples,
			Percent: percent(samples, f.samples),
		})
	}
	return Func{
		Index:   fn,
		Samples: f.samples,
		Percent: percent(f.samples, total),
		IPs:     ips,
	}
}

func percent(samples, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(samples) / float64(total) * 100
}
