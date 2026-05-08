package prof

// FuncProfile is an immutable snapshot of one function's execution data.
type FuncProfile struct {
	Calls  uint64
	Blocks []uint64
}

// Profile is an immutable snapshot of execution-frequency data for a program.
type Profile struct {
	Funcs []FuncProfile
}

// Recorder collects execution-frequency samples during bytecode interpretation.
// It grows automatically to accommodate any function index and instruction pointer.
type Recorder struct {
	funcs []funcData
}

type funcData struct {
	calls  uint64
	blocks []uint64
}

// New returns an empty Recorder.
func New() *Recorder {
	return &Recorder{}
}

// Record records one execution sample at (funcIdx, ip).
// It automatically grows to accommodate new function indices and IPs.
func (r *Recorder) Record(funcIdx, ip int) {
	for len(r.funcs) <= funcIdx {
		r.funcs = append(r.funcs, funcData{})
	}
	if len(r.funcs[funcIdx].blocks) <= ip {
		nb := make([]uint64, ip+1)
		copy(nb, r.funcs[funcIdx].blocks)
		r.funcs[funcIdx].blocks = nb
	}
	r.funcs[funcIdx].calls++
	r.funcs[funcIdx].blocks[ip]++
}

// Calls returns the aggregate sample count for funcIdx.
// Returns 0 for an unknown index.
func (r *Recorder) Calls(funcIdx int) uint64 {
	if funcIdx >= len(r.funcs) {
		return 0
	}
	return r.funcs[funcIdx].calls
}

// Hits returns the per-IP sample count for funcIdx at ip.
// Returns 0 for unknown indices.
func (r *Recorder) Hits(funcIdx, ip int) uint64 {
	if funcIdx >= len(r.funcs) || ip >= len(r.funcs[funcIdx].blocks) {
		return 0
	}
	return r.funcs[funcIdx].blocks[ip]
}

// Snapshot returns an immutable deep copy of all collected data.
func (r *Recorder) Snapshot() Profile {
	p := Profile{Funcs: make([]FuncProfile, len(r.funcs))}
	for i, f := range r.funcs {
		blocks := make([]uint64, len(f.blocks))
		copy(blocks, f.blocks)
		p.Funcs[i] = FuncProfile{Calls: f.calls, Blocks: blocks}
	}
	return p
}
