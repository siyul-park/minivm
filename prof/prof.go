package prof

// Func holds execution-frequency data for one function.
type Func struct {
	Count  uint64
	Blocks []uint64
}

// Stats records execution-frequency data during bytecode interpretation.
// It grows automatically to accommodate any function index and instruction pointer.
type Stats struct {
	funcs []funcData
}

type funcData struct {
	count  uint64
	blocks []uint64
}

// New returns an empty Stats.
func New() *Stats {
	return &Stats{}
}

// Record records one execution sample at (funcIdx, ip).
// It automatically grows to accommodate new function indices and IPs.
func (p *Stats) Record(funcIdx, ip int) {
	for len(p.funcs) <= funcIdx {
		p.funcs = append(p.funcs, funcData{})
	}
	if len(p.funcs[funcIdx].blocks) <= ip {
		nb := make([]uint64, ip+1)
		copy(nb, p.funcs[funcIdx].blocks)
		p.funcs[funcIdx].blocks = nb
	}
	p.funcs[funcIdx].count++
	p.funcs[funcIdx].blocks[ip]++
}

// Count returns the aggregate tick-sample count for funcIdx.
// Returns 0 for an unknown index.
func (p *Stats) Count(funcIdx int) uint64 {
	if funcIdx >= len(p.funcs) {
		return 0
	}
	return p.funcs[funcIdx].count
}

// Hits returns the per-IP sample count for funcIdx at ip.
// Returns 0 for unknown indices.
func (p *Stats) Hits(funcIdx, ip int) uint64 {
	if funcIdx >= len(p.funcs) || ip >= len(p.funcs[funcIdx].blocks) {
		return 0
	}
	return p.funcs[funcIdx].blocks[ip]
}

// HitsInRange returns the sum of per-IP hit counts for funcIdx over [start, end).
// Returns 0 for an unknown function index.
func (p *Stats) HitsInRange(funcIdx, start, end int) uint64 {
	if funcIdx >= len(p.funcs) {
		return 0
	}
	blocks := p.funcs[funcIdx].blocks
	var total uint64
	for ip := start; ip < end && ip < len(blocks); ip++ {
		total += blocks[ip]
	}
	return total
}

// Funcs returns an immutable deep copy of all collected function data.
func (p *Stats) Funcs() []Func {
	out := make([]Func, len(p.funcs))
	for i, f := range p.funcs {
		blocks := make([]uint64, len(f.blocks))
		copy(blocks, f.blocks)
		out[i] = Func{Count: f.count, Blocks: blocks}
	}
	return out
}
