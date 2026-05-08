package prof

// Func holds execution-frequency data for one function.
type Func struct {
	Samples uint64
	Blocks  []uint64
}

// Profile records execution-frequency data during bytecode interpretation.
// It grows automatically to accommodate any function index and instruction pointer.
type Profile struct {
	funcs []funcData
}

type funcData struct {
	samples uint64
	blocks  []uint64
}

// New returns an empty Profile.
func New() *Profile {
	return &Profile{}
}

// Record records one execution sample at (funcIdx, ip).
// It automatically grows to accommodate new function indices and IPs.
func (p *Profile) Record(funcIdx, ip int) {
	for len(p.funcs) <= funcIdx {
		p.funcs = append(p.funcs, funcData{})
	}
	if len(p.funcs[funcIdx].blocks) <= ip {
		nb := make([]uint64, ip+1)
		copy(nb, p.funcs[funcIdx].blocks)
		p.funcs[funcIdx].blocks = nb
	}
	p.funcs[funcIdx].samples++
	p.funcs[funcIdx].blocks[ip]++
}

// Samples returns the aggregate tick-sample count for funcIdx.
// Returns 0 for an unknown index.
func (p *Profile) Samples(funcIdx int) uint64 {
	if funcIdx >= len(p.funcs) {
		return 0
	}
	return p.funcs[funcIdx].samples
}

// Hits returns the per-IP sample count for funcIdx at ip.
// Returns 0 for unknown indices.
func (p *Profile) Hits(funcIdx, ip int) uint64 {
	if funcIdx >= len(p.funcs) || ip >= len(p.funcs[funcIdx].blocks) {
		return 0
	}
	return p.funcs[funcIdx].blocks[ip]
}

// HitsInRange returns the sum of per-IP hit counts for funcIdx over [start, end).
// Returns 0 for an unknown function index.
func (p *Profile) HitsInRange(funcIdx, start, end int) uint64 {
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
func (p *Profile) Funcs() []Func {
	out := make([]Func, len(p.funcs))
	for i, f := range p.funcs {
		blocks := make([]uint64, len(f.blocks))
		copy(blocks, f.blocks)
		out[i] = Func{Samples: f.samples, Blocks: blocks}
	}
	return out
}
