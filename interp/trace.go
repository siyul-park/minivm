package interp

import (
	"fmt"
	"sync"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

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

// Tracer is the shared JIT front-end: it samples execution into an aggregate
// profile and records hot linear traces that the trace compiler consumes. One
// Tracer is shared across a pool so a trace recorded by one member compiles
// once and serves all.
type Tracer struct {
	mu        sync.Mutex
	stats     *stats
	precise   [][]func(*Interpreter)
	loops     map[int][]int
	trees     map[anchor]*tree
	blacklist map[anchor]bool
}

// stats records execution-frequency data during bytecode interpretation. It
// grows automatically to accommodate any function index and instruction
// pointer. Each interpreter owns a private stats for contention-free sampling
// on the hot path; the shared Tracer merges them into its aggregate.
type stats struct {
	samples uint64
	funcs   []funcData
	opcodes [256]uint64
	jit     JIT
}

type funcData struct {
	samples uint64
	ips     []uint64
}

type anchor struct {
	addr int
	ip   int
}

type outcome int

type step struct {
	op   instr.Opcode
	seen types.Boxed

	fn     int
	ip     int
	depth  int
	target int
	taken  bool
	callee int
}

type trace struct {
	anchor anchor
	ops    []step
	kind   outcome
}

type tree struct {
	root     *trace
	branches map[int]*trace
	hits     []int64
	exits    map[int]int

	attempts int
}

const (
	linear outcome = iota
	loop
	returned
	aborted
)

const opLimit = 1024

const exitThreshold = 8

const attemptLimit = 8

func NewTracer() *Tracer {
	return &Tracer{
		stats:     newStats(),
		loops:     map[int][]int{},
		trees:     map[anchor]*tree{},
		blacklist: map[anchor]bool{},
	}
}

func newStats() *stats {
	return &stats{}
}

// Profile returns an immutable copy of the shared aggregate profile.
func (r *Tracer) Profile() Snapshot {
	return r.snapshot(nil)
}

// flush merges a member's local samples into the shared aggregate and resets
// the local collector.
func (r *Tracer) flush(local *stats) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stats.Merge(local.Snapshot())
	local.Reset()
}

// addJIT records compilation counters straight into the shared aggregate.
// Compilation is a shared event in a pool, so counters are visible to every
// member immediately rather than waiting for a flush.
func (r *Tracer) addJIT(d JIT) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stats.addJIT(d)
}

// snapshot returns the aggregate merged with local without disturbing either,
// so a caller sees its own unflushed samples on top of the shared total.
func (r *Tracer) snapshot(local *stats) Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	merged := newStats()
	merged.Merge(r.stats.Snapshot())
	if local != nil {
		merged.Merge(local.Snapshot())
	}
	return merged.Snapshot()
}

func (r *Tracer) exit(i *Interpreter, root anchor, ip int) (int64, error) {
	r.mu.Lock()
	tree := r.tree(root)
	id := r.exitIndex(tree, ip)
	tree.hits[id]++
	hits := tree.hits[id]
	if tree.branches[id] != nil {
		r.mu.Unlock()
		return hits, nil
	}
	r.mu.Unlock()

	t, err := r.capture(i, anchor{addr: i.fr.addr, ip: ip})
	if err != nil {
		return hits, err
	}
	r.mu.Lock()
	tree.branches[id] = t
	r.mu.Unlock()
	return hits, nil
}

func (r *Tracer) capture(i *Interpreter, a anchor) (*trace, error) {
	r.mu.Lock()
	if r.blacklist[a] {
		r.mu.Unlock()
		return nil, nil
	}
	tree := r.tree(a)
	if tree.attempts >= attemptLimit {
		r.blacklist[a] = true
		r.mu.Unlock()
		return nil, nil
	}
	tree.attempts++
	r.mu.Unlock()

	if a.addr < 0 || a.addr >= len(i.instrs) || a.ip < 0 || a.ip >= len(i.instrs[a.addr]) {
		r.mu.Lock()
		r.blacklist[a] = true
		r.mu.Unlock()
		return nil, nil
	}

	clone := r.clone(i)
	clone.fr = &clone.frames[i.fp-1]
	clone.fr.ip = a.ip
	// An entry anchor replays the function from a clean frame: drop any operands
	// the sampling safepoint left mid-expression so the trace begins exactly as
	// a fresh call would, with only params and locals live.
	if a.ip == 0 && clone.fr.addr == a.addr {
		if fn, ok := clone.function(a.addr); ok {
			clone.sp = clone.fr.bp + len(fn.LocalKinds())
		}
	}

	t := &trace{anchor: a, kind: linear}
	startFP := clone.fp
	for len(t.ops) < opLimit {
		f := clone.fr
		if f.addr < 0 || f.addr >= len(clone.instrs) || f.ip < 0 || f.ip >= len(clone.instrs[f.addr]) {
			t.kind = aborted
			break
		}

		code := clone.instrs[f.addr]
		op := instr.Opcode(code[f.ip])
		if r.unrecordable(&clone, op) {
			t.kind = aborted
			break
		}

		st := r.op(&clone, op, startFP)
		if op == instr.CALL && r.recursive(&clone, a) {
			r.skipCall(&clone, a.addr)
			st.callee = a.addr
			t.ops = append(t.ops, st)
			continue
		}
		// A tail call back to the entry anchor closes the trace as a native loop
		// back-edge: record it as the entry trace's terminal op without stepping
		// into the reused frame, so it compiles like a loop without tripping the
		// ip-0 loop ban (the trace stays kind=returned).
		if op == instr.RETURN_CALL && a.ip == 0 && r.tailToAnchor(&clone, a) {
			st.callee = a.addr
			t.ops = append(t.ops, st)
			t.kind = returned
			r.store(a, t)
			return t, nil
		}
		// YIELD and RESUME are true suspension points whose suspend/resume bodies
		// a linear trace cannot represent. In the anchor frame, record the op as
		// the trace's terminal and store kind=returned WITHOUT stepping the clone
		// (which would unwind it on YIELD or splice the uncompilable resumed-frame
		// body on RESUME); the JIT lowers this to an unconditional deopt so the
		// threaded handler performs the real suspend/resume. The deopt only
		// preserves the coroutine handle on the outermost (anchor) frame — inlined
		// callee frames are rebuilt without their coro field, so a suspend there
		// would mis-read. Abort rather than miscompile when the op sits in an
		// inlined frame.
		if op == instr.YIELD || op == instr.RESUME {
			if clone.fp != startFP {
				t.kind = aborted
				break
			}
			t.ops = append(t.ops, st)
			t.kind = returned
			r.store(a, t)
			return t, nil
		}
		if err := r.step(&clone, f.addr, f.ip); err != nil {
			t.kind = aborted
			break
		}

		r.finish(&clone, &st, op)
		t.ops = append(t.ops, st)

		switch {
		case op == instr.RETURN && st.depth == 0:
			t.kind = returned
			r.store(a, t)
			return t, nil
		case clone.fr.addr >= 0 && clone.fr.addr < len(clone.instrs) && clone.fr.ip >= len(clone.instrs[clone.fr.addr]):
			r.store(a, t)
			return t, nil
		case clone.fr.addr == a.addr && clone.fr.ip == a.ip:
			t.kind = loop
			r.store(a, t)
			return t, nil
		case clone.fp < startFP:
			t.kind = returned
			r.store(a, t)
			return t, nil
		}
	}
	if len(t.ops) >= opLimit {
		t.kind = aborted
	}
	r.store(a, t)
	return t, nil
}

func (r *Tracer) clone(i *Interpreter) Interpreter {
	out := *i
	out.compiler = nil
	out.hook = nil
	out.threshold = -1

	out.constants = append([]types.Boxed(nil), i.constants...)
	out.globals = append([]types.Boxed(nil), i.globals...)
	out.instrs = append([][]byte(nil), i.instrs...)
	out.code = r.codes(i)
	out.fallbacks = map[anchor]func(*Interpreter){}
	out.journal = append([]uint64(nil), i.journal...)
	out.frames = append([]frame(nil), i.frames...)
	out.stack = append([]types.Boxed(nil), i.stack...)
	out.roots = append([]types.Boxed(nil), i.roots...)
	out.heap = r.heap(i.heap)
	out.free = append([]int(nil), i.free...)
	out.rc = append([]int(nil), i.rc...)
	out.interned = make(map[string]types.Ref, len(i.interned))
	for key, val := range i.interned {
		out.interned[key] = val
	}
	for idx := 0; idx < out.fp; idx++ {
		addr := out.frames[idx].addr
		if addr >= 0 && addr < len(out.code) {
			out.frames[idx].code = out.code[addr]
		}
		out.frames[idx].upvals = append([]types.Boxed(nil), out.frames[idx].upvals...)
	}
	return out
}

func (r *Tracer) heap(heap []types.Value) []types.Value {
	out := make([]types.Value, len(heap))
	for idx, val := range heap {
		switch v := val.(type) {
		case *types.Closure:
			clone := *v
			clone.Upvals = append([]types.Boxed(nil), v.Upvals...)
			out[idx] = &clone
		default:
			out[idx] = val
		}
	}
	return out
}

func (r *Tracer) codes(i *Interpreter) [][]func(*Interpreter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.precise) == len(i.instrs) {
		return r.precise
	}
	r.precise = make([][]func(*Interpreter), len(i.instrs))
	for addr, code := range i.instrs {
		if len(code) == 0 {
			continue
		}
		var locals []types.Kind
		if fn, ok := i.function(addr); ok {
			locals = fn.LocalKinds()
		}
		tc := &threadedCompiler{
			types:     i.types,
			constants: i.constants,
			heap:      i.heap,
			exts:      i.exts,
			precise:   true,
		}
		r.precise[addr] = tc.Compile(code, locals)
	}
	return r.precise
}

func (r *Tracer) op(i *Interpreter, op instr.Opcode, startFP int) step {
	f := i.fr
	st := step{
		op:    op,
		fn:    f.addr,
		ip:    f.ip,
		depth: i.fp - startFP,
	}
	switch op {
	case instr.BR, instr.BR_IF:
		st.target = f.ip + instr.ParseI16(i.instrs[f.addr], f.ip+1) + 3
	case instr.CALL, instr.RETURN_CALL:
		if i.sp > 0 {
			st.seen = i.stack[i.sp-1]
		}
	}
	return st
}

func (r *Tracer) finish(i *Interpreter, st *step, op instr.Opcode) {
	switch op {
	case instr.BR_IF:
		if i.fr.addr == st.fn && i.fr.ip == st.target {
			st.taken = true
		}
	case instr.BR_TABLE:
		st.target = i.fr.ip
	case instr.CALL, instr.RETURN_CALL:
		st.callee = i.fr.addr
	case instr.REF_GET, instr.ARRAY_GET, instr.STRUCT_GET, instr.CORO_VALUE:
		if i.sp > 0 {
			st.seen = i.stack[i.sp-1]
		}
	}
}

func (r *Tracer) recursive(i *Interpreter, a anchor) bool {
	if i.sp == 0 || i.stack[i.sp-1].Kind() != types.KindRef {
		return false
	}
	addr := i.stack[i.sp-1].Ref()
	if addr != a.addr || addr < 0 || addr >= len(i.heap) {
		return false
	}
	_, ok := i.heap[addr].(*types.Function)
	return ok
}

// tailToAnchor reports whether the next RETURN_CALL tail-calls the trace's own
// anchor function with a plain function ref on top of the stack. Such a tail
// call closes the trace as a native loop back-edge rather than stepping into a
// fresh callee body, so the recorder treats it as the entry trace's terminal.
func (r *Tracer) tailToAnchor(i *Interpreter, a anchor) bool {
	if i.sp == 0 || i.stack[i.sp-1].Kind() != types.KindRef {
		return false
	}
	addr := i.stack[i.sp-1].Ref()
	if addr != a.addr || addr < 0 || addr >= len(i.heap) {
		return false
	}
	_, ok := i.heap[addr].(*types.Function)
	return ok
}

func (r *Tracer) skipCall(i *Interpreter, addr int) {
	fn := i.heap[addr].(*types.Function)
	i.sp -= len(fn.Typ.Params) + 1
	for _, typ := range fn.Typ.Returns {
		var val types.Boxed
		switch typ.Kind() {
		case types.KindI32:
			val = types.BoxI32(0)
		case types.KindI64:
			val = types.BoxI64(0)
		case types.KindF32:
			val = types.BoxF32(0)
		case types.KindF64:
			val = types.BoxF64(0)
		default:
			val = types.BoxedNull
		}
		i.stack[i.sp] = val
		i.sp++
	}
	i.fr.ip++
}

func (r *Tracer) step(i *Interpreter, addr, ip int) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("trace aborted: %v", v)
		}
	}()
	i.code[addr][ip](i)
	return nil
}

func (r *Tracer) store(a anchor, t *trace) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tr := r.tree(a)
	tr.root = t
}

func (r *Tracer) tree(a anchor) *tree {
	tr := r.trees[a]
	if tr == nil {
		tr = &tree{
			branches: map[int]*trace{},
			exits:    map[int]int{},
		}
		r.trees[a] = tr
	}
	return tr
}

func (r *Tracer) anchors(addr int) []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]int, 0, len(r.trees))
	for anchor, tree := range r.trees {
		if anchor.addr == addr && tree.root != nil && tree.root.kind != aborted {
			out = append(out, anchor.ip)
		}
	}
	return out
}

// rootAt returns the recorded tree anchored exactly at a, or nil when none is
// recorded or its root aborted. The compiler emits one native per usable root.
func (r *Tracer) rootAt(a anchor) *tree {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.trees[a]
	if t == nil || t.root == nil || t.root.kind == aborted {
		return nil
	}
	return t
}

func (r *Tracer) hasEntry(addr int) bool {
	return r.hasLoop(addr, 0)
}

// hasLoop reports whether a non-aborted trace is already recorded at
// (addr, ip), so the trigger does not re-capture an existing root.
func (r *Tracer) hasLoop(addr, ip int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	tree := r.trees[anchor{addr: addr, ip: ip}]
	return tree != nil && tree.root != nil && tree.root.kind != aborted
}

// headers returns the loop-header IPs of the function at addr: the targets of
// backward branches, where a hot loop re-enters. The scan is static and
// memoized per address.
func (r *Tracer) headers(i *Interpreter, addr int) []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if hs, ok := r.loops[addr]; ok {
		return hs
	}
	var hs []int
	if addr >= 0 && addr < len(i.instrs) {
		code := i.instrs[addr]
		seen := map[int]bool{}
		for ip := 0; ip < len(code); {
			w := instr.Instruction(code[ip:]).Width()
			if w <= 0 {
				break
			}
			switch instr.Opcode(code[ip]) {
			case instr.BR, instr.BR_IF:
				target := ip + instr.ParseI16(code, ip+1) + 3
				if target >= 0 && target < ip && !seen[target] {
					seen[target] = true
					hs = append(hs, target)
				}
			}
			ip += w
		}
	}
	r.loops[addr] = hs
	return hs
}

func (r *Tracer) exitIndex(tree *tree, ip int) int {
	if idx, ok := tree.exits[ip]; ok {
		return idx
	}
	idx := len(tree.hits)
	tree.exits[ip] = idx
	tree.hits = append(tree.hits, 0)
	return idx
}

func (r *Tracer) unrecordable(i *Interpreter, op instr.Opcode) bool {
	if (op == instr.CALL || op == instr.RETURN_CALL) && i.sp > 0 {
		if i.stack[i.sp-1].Kind() != types.KindRef {
			return false
		}
		addr := i.stack[i.sp-1].Ref()
		if addr < 0 || addr >= len(i.heap) {
			return false
		}
		if _, ok := i.heap[addr].(*HostFunction); ok {
			return true
		}
		// A tail call only lowers to a plain function target: the loop back-edge
		// and the in-place activation morph have no slot for closure upvals, so
		// closures deopt to the threaded tail() handler.
		if op == instr.RETURN_CALL {
			_, ok := i.heap[addr].(*types.Function)
			return !ok
		}
		return false
	}
	switch op {
	// YIELD and RESUME suspend or rebuild a frame, so a linear trace cannot
	// span them; capture records them as terminal deopt boundaries instead of
	// aborting, and the JIT lowers each to an unconditional deopt that hands the
	// real suspend/resume back to the threaded handler. CORO_DONE and CORO_VALUE
	// are pure heap reads (handle in, value out) and stay recordable like
	// ARRAY_GET/STRUCT_GET; the JIT lowers them directly.
	case instr.STRING_NEW_UTF32,
		instr.ARRAY_NEW,
		instr.ARRAY_NEW_DEFAULT,
		instr.ARRAY_SET,
		instr.ARRAY_FILL,
		instr.ARRAY_COPY,
		instr.STRUCT_NEW,
		instr.STRUCT_NEW_DEFAULT,
		instr.STRUCT_SET,
		instr.MAP_NEW,
		instr.MAP_NEW_DEFAULT,
		instr.MAP_SET,
		instr.MAP_DELETE,
		instr.MAP_CLEAR,
		instr.MAP_KEYS,
		instr.MAP_ITER,
		instr.REF_NEW,
		instr.REF_SET,
		instr.CLOSURE_NEW:
		return true
	}
	return false
}

// Add records one execution sample at (fn, ip) for op. It grows to accommodate
// new function indices and IPs.
func (p *stats) Add(fn, ip int, op byte) {
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

// Samples returns the sample count for fn, or 0 for an unknown index.
func (p *stats) Samples(fn int) uint64 {
	if fn < 0 || fn >= len(p.funcs) {
		return 0
	}
	return p.funcs[fn].samples
}

// Func returns a copy of the profile data for fn.
func (p *stats) Func(fn int) Func {
	if fn < 0 || fn >= len(p.funcs) {
		return Func{Index: fn}
	}
	return p.funcData(fn, p.samples)
}

// IP returns the profile data for fn at ip.
func (p *stats) IP(fn, ip int) IP {
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
func (p *stats) Range(fn, start, end int) uint64 {
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
func (p *stats) Snapshot() Snapshot {
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
func (p *stats) Merge(s Snapshot) {
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
	p.addJIT(s.JIT)
}

// Reset clears all collected profile data.
func (p *stats) Reset() {
	p.samples = 0
	p.funcs = nil
	clear(p.opcodes[:])
	p.jit = JIT{}
}

// addJIT merges d into the aggregate JIT counters.
func (p *stats) addJIT(d JIT) {
	p.jit.Attempts += d.Attempts
	p.jit.Emits += d.Emits
	p.jit.Links += d.Links
	p.jit.Skips += d.Skips
	p.jit.Errors += d.Errors
	p.jit.Bytes += d.Bytes
}

func (p *stats) funcData(fn int, total uint64) Func {
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

func (t *tree) branchIPs() map[int]*trace {
	out := map[int]*trace{}
	for _, branch := range t.branches {
		if branch != nil {
			out[branch.anchor.ip] = branch
		}
	}
	return out
}

func percent(samples, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(samples) / float64(total) * 100
}
