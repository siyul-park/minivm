package interp

import (
	"errors"
	"maps"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/arm64"
	"github.com/siyul-park/minivm/internal/jit/compile"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/transform"
	"github.com/siyul-park/minivm/types"
)

// native holds per-interpreter native state and shared compiled-code state.
type native struct {
	// ctx is built by n's first entry: its native stack is the largest
	// allocation a JIT makes, and most programs never enter native code.
	ctx *jit.Context
	*shared
	// reader is n's registration with its shared store.
	reader *jit.Reader

	// threshold is the call count that compiles an address's Baseline, and
	// each OSR site's back-edge count: WithThreshold's n, or floor when auto.
	threshold int
	// quota is the work units a safepoint adds to Context.Budget: the tick
	// of an interpreter with fuel or a hook, else budget.
	quota int64
	// auto reports WithThreshold(0): a retire raises the address's thresholds
	// (backoffs), and n stays dormant until the interpreter's heat runs out.
	auto bool
	// backoffs counts each address's retires, up to steps: each raises its
	// automatic call and promotion thresholds 4×.
	backoffs []uint8
	// briefs marks, under the automatic policy, each address whose every run
	// executes fewer than short instructions (span): Go runs it threaded,
	// and only its native callers' served calls count it (replay).
	briefs []bool
	// gates closes the CALL hook per address: a failed Baseline or brief
	// code. It folds both into one load to keep call inlinable.
	gates []bool
	// entries is native-writable: Context.Entries points at it, and every
	// Baseline function prologue increments its own address there, including
	// native-to-native entries.
	entries []int64
	// refutes counts each address's refuted speculations since its last retire.
	refutes []int
	// ledgers weighs each address's native work against its exits' cost
	// across Go entries.
	ledgers []jit.Ledger
	// failed records permanent compile failure per address and tier.
	failed [][2]bool
	// built is the feedback each address's published code was compiled from,
	// per tier: a retire blocks a recompile only when feedback has not moved
	// since.
	built [][2]transform.Module
	// sites indexes every observed OSR site by (address, ip): drain looks a
	// failed OSR unit's site up here to restore its threaded handler.
	sites map[key]*site
	// callees records each dynamic CALL's observed callees, by caller
	// address then ip: the zero Callee means unseen, a mixed Function more
	// than one seen (Type their shared signature, nil when they differ), any
	// other the one callee seen there so far. A caller's own ip slice is
	// allocated lazily, sized to its own code. feedback reads this to
	// snapshot a unit's own sites at submit time.
	callees [][]transform.Callee
	// refuted records, by address then ip, each guard native code has seen
	// fail; allocated lazily like callees. feedback snapshots it.
	refuted [][]bool

	// exact caches unfused threaded code for materialized frames, by address.
	exact [][]func(*Interpreter)

	// depth is how many native activations are suspended under a call the
	// interpreter runs for them (nest): the next entry starts its records
	// above them.
	depth uint64
	// borrows caches transform.Borrows by address for entries above depth
	// 0, whose activation's return keeps its borrowed parameters (it
	// releases them only at depth 1).
	borrows [][]bool

	// compile is captured once so deoptimization does not add a static
	// dependency from generated threaded handlers back to their compiler.
	// Calling i.compile here directly creates the threaded/fusions
	// initialization cycle.
	compile func(fn *types.Function, exact bool) []func(*Interpreter)
}

// hold is one heap operand of a bridge attempt and its count before it.
type hold struct {
	ref, count int
}

// shared contains the pool-shareable native runtime: Store, Queue, and Module.
// refs closes it after the last interpreter releases it.
type shared struct {
	store  *jit.Store
	queue  *compile.Queue
	module transform.Module

	// candidates holds every address, pool-wide, with published Baseline
	// code not yet promoted or permanently blocked. Any native sharing r
	// may nominate or sweep it, not only the one that published the job.
	mu         sync.Mutex
	candidates []int
	// nominated is len(candidates), readable without mu so an empty sweep —
	// every call's common case — takes no lock.
	nominated atomic.Int64

	// calls is the pool-wide CALL count per address, added to by every
	// native sharing r while the address has no published code: a pooled
	// workload's calls compile Baseline after about threshold total calls,
	// not threshold calls to any one interpreter.
	calls []atomic.Int64

	// totals is the pool-wide entry count per OSR/entry site, added to by
	// every native sharing r while the site is unsubmitted; built lazily
	// under mu.
	totals map[key]*atomic.Int64

	refs atomic.Int64
}

// callout computes an allocating operation's new object from its operands,
// args holding the deepest first, from code, the operation's bytecode. It is
// the interpreter's own helper for the operation, which writes no reference
// count of an operand (native code owns and releases each).
type callout func(i *Interpreter, code []byte, args [2]types.Boxed) types.Value

// nativeStack is the native stack size per interpreter.
const nativeStack = 1 << 20

// budget is the work units between safepoints of an unmetered interpreter.
const budget = 1 << 16

// tolerance is the count of refuted speculations under unchanged feedback that
// retires native code.
const tolerance = 8

// graduate is the Baseline entry count that tiers an address to Optimized.
const graduate = 1024

// The automatic policy (WithThreshold(0)). One wake — analysis, the first
// compiles, and their garbage — costs ~0.3–0.6 ms of the interpreter's time,
// so a dormant JIT waits for ~30 ms of threaded work before it can cost a
// program 2 %: dormancy frame entries and taken back edges at ~15 ns. A Run
// spends a 32nd of it.
const (
	dormancy = 1 << 21
	// floor is the automatic call and back-edge threshold before any retire.
	floor = 256
	// steps caps the retires that raise an address's thresholds.
	steps = 4
	// ceiling is the highest automatic threshold: floor raised steps times.
	ceiling = floor << (2 * steps)
	// short is the fewest instructions a run must execute to repay a Go
	// entry: the entry's ~15 ns round trip over the ~0.5 ns native code saves
	// per threaded instruction.
	short = 32
)

// mixed marks a dynamic CALL site (native.callees) that has seen more than
// one callee: it never speculates.
const mixed = -1

const (
	metricCompiles = "vm_jit_compiles_total"
	metricEntries  = "vm_jit_entries_total"
	metricExits    = "vm_jit_exits_total"
)

// bridgeable reports, by opcode, whether an ExitBridge for it runs its
// threaded handler in Go and resumes native code; otherwise the exit
// deoptimizes and threaded code runs the op once. It denies:
//   - a control transfer (the op writes Branch): no next instruction to resume;
//   - ARRAY_NEW: instr.Type declares two operands, not the 1+count it pops,
//     so its SSA arguments do not cover its operands;
//   - MAP_KEYS: it allocates every string or wide i64 key before the result
//     array, so a heap-exhaustion trap between them would leave the keys a
//     failed attempt allocated.
//
// It is a table because every bridge exit reads it.
var bridgeable = func() (out [256]bool) {
	for code := range out {
		op := instr.Opcode(code)
		out[code] = !op.Writes(instr.Branch) && op != instr.ARRAY_NEW && op != instr.MAP_KEYS
	}
	return out
}()

// callouts serve the bridges of allocating operations without a scratch
// frame, operand retains, or a handler run.
var callouts = [256]callout{
	instr.STRING_CONCAT: func(i *Interpreter, _ []byte, args [2]types.Boxed) types.Value {
		return i.concat(args[0], args[1])
	},
	instr.STRING_NEW_UTF32: func(i *Interpreter, _ []byte, args [2]types.Boxed) types.Value {
		return i.text(args[0])
	},
	instr.STRING_ENCODE_UTF32: func(i *Interpreter, _ []byte, args [2]types.Boxed) types.Value {
		return i.runes(args[0])
	},
	instr.ARRAY_NEW_DEFAULT: func(i *Interpreter, code []byte, args [2]types.Boxed) types.Value {
		return i.newArrayDefault(i.types[index(code)].(*types.ArrayType), args[0])
	},
	instr.STRUCT_NEW_DEFAULT: func(i *Interpreter, code []byte, _ [2]types.Boxed) types.Value {
		return i.newStruct(i.types[index(code)].(*types.StructType))
	},
}

// total returns r's pool-wide entry counter for the OSR/entry site k,
// creating it for the first native that observes k.
func (r *shared) total(k key) *atomic.Int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.totals == nil {
		r.totals = map[key]*atomic.Int64{}
	}
	t := r.totals[k]
	if t == nil {
		t = new(atomic.Int64)
		r.totals[k] = t
	}
	return t
}

// nominate adds addr to r's candidates, once.
func (r *shared) nominate(addr int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !slices.Contains(r.candidates, addr) {
		r.candidates = append(r.candidates, addr)
		r.nominated.Add(1)
	}
}

// sweep calls visit on every candidate and keeps only the ones it reports
// live, under one lock for the whole pass.
func (r *shared) sweep(visit func(addr int) (live bool)) {
	if r.nominated.Load() == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	live := r.candidates[:0]
	for _, addr := range r.candidates {
		if visit(addr) {
			live = append(live, addr)
		}
	}
	r.candidates = live
	r.nominated.Store(int64(len(live)))
}

// jitEnabled reports whether opt selects the JIT: WithThreshold(n) with n >=
// 0, on arm64, with a tick above 1: a tick of 1 is exact execution, every
// instruction boundary observable, which native code does not expose.
func jitEnabled(opt option) bool {
	return opt.threshold >= 0 && runtime.GOARCH == "arm64" && opt.tick > 1
}

// newModule builds the program data compile.Unit reads: the interpreter's
// own loaded constants and their real heap addresses, never the live heap
// (L11), so it is built once per program.
func newModule(i *Interpreter) transform.Module {
	objects := transform.Objects{}
	for _, c := range i.constants {
		if c.Kind() != types.KindRef {
			continue
		}
		addr := c.Ref()
		switch v := i.heap[addr].(type) {
		case *types.Function:
			objects[addr] = transform.Object{Function: v}
		case *types.Struct:
			objects[addr] = transform.Object{Struct: v.Typ}
		case types.I64:
			objects[addr] = transform.Object{I64: &v}
		default:
			if at, ok := v.Type().(*types.ArrayType); ok {
				objects[addr] = transform.Object{Array: at}
			}
		}
	}
	return transform.Module{
		Constants: i.constants,
		Globals:   types.Kinds(i.globalTypes),
		Objects:   objects,
		Types:     i.types,
	}
}

// newShared builds a fresh, unshared JIT runtime for i's program: a Store
// sized to i's code, a single-worker compile Queue, and i's constant module.
func newShared(i *Interpreter) *shared {
	r := &shared{
		store:  jit.NewStore(len(i.code)),
		queue:  compile.NewQueue(func() compile.Machine { return arm64.New() }, 1),
		module: newModule(i),
		calls:  make([]atomic.Int64, len(i.code)),
	}
	r.refs.Store(1)
	return r
}

// retain adds one reference to r, for a Pool sharing it with another
// native, and returns r.
func (r *shared) retain() *shared {
	r.refs.Add(1)
	return r
}

// release drops one reference to r, closing its queue — freeing every code
// it finished but no native ever drained — and its store once none remain.
// A native's calls into its shared runtime are synchronous, so no native
// code is ever suspended when the last reference releases.
func (r *shared) release() error {
	if r.refs.Add(-1) > 0 {
		return nil
	}
	var err error
	for _, job := range r.queue.Close() {
		if job.Code != nil {
			err = errors.Join(err, job.Code.Free())
		}
	}
	return errors.Join(err, r.store.Close())
}

// newNative builds i's JIT runtime: live at once for a fixed threshold,
// dormant for the automatic one.
func newNative(i *Interpreter, threshold int) *native {
	// compile is captured here, not in wake: threaded code reaches wake
	// (Interpreter.cool), and a reference from it to i.compile closes the
	// threaded/fusions initialization cycle.
	// A profiler only samples native safepoints: a shorter quota would cost it.
	quota := int64(budget)
	if i.gas >= 0 || i.hook != nil {
		quota = int64(i.tick)
	}
	if threshold == 0 {
		return &native{auto: true, threshold: floor, quota: quota, compile: i.compile}
	}
	n := &native{threshold: threshold, quota: quota, compile: i.compile}
	n.wake(i) // live: only the automatic policy turns a program away
	return n
}

// wake builds n's live state, once, and reports whether n is live. Under the
// automatic policy a program whose code is all brief has nothing a Go entry
// could repay: it stays without a JIT.
func (n *native) wake(i *Interpreter) bool {
	if n.shared != nil {
		return true
	}
	briefs := make([]bool, len(i.code))
	if n.auto {
		spans := map[int]int{}
		briefs[0] = n.span(i, 0, spans) < short
		all := briefs[0]
		for _, c := range i.constants {
			if c.Kind() != types.KindRef {
				continue
			}
			if _, ok := i.heap[c.Ref()].(*types.Function); ok {
				briefs[c.Ref()] = n.span(i, c.Ref(), spans) < short
				all = all && briefs[c.Ref()]
			}
		}
		if all {
			return false
		}
	}
	r := newShared(i)
	n.shared = r
	n.reader = r.store.Attach()
	n.backoffs = make([]uint8, len(i.code))
	n.entries = make([]int64, len(i.code))
	n.refutes = make([]int, len(i.code))
	n.ledgers = make([]jit.Ledger, len(i.code))
	n.failed = make([][2]bool, len(i.code))
	n.built = make([][2]transform.Module, len(i.code))
	n.exact = make([][]func(*Interpreter), len(i.code))
	n.borrows = make([][]bool, len(i.code))
	n.sites = map[key]*site{}
	n.callees = make([][]transform.Callee, len(i.code))
	n.refuted = make([][]bool, len(i.code))
	n.briefs = briefs
	n.gates = slices.Clone(briefs)
	// OSR observes every loop header of every function i compiled at
	// construction, module code (address 0) included; a function bound
	// later (a dynamic closure) is not.
	n.observe(i, 0, i.module)
	for addr, obj := range n.module.Objects {
		if obj.Function != nil {
			n.observe(i, addr, obj.Function)
		}
	}
	return true
}

// span returns how many instructions one run of the code at addr executes,
// its constant callees' included, or short when it may run more: a loop, a
// call through anything but a constant function, a tail call, or recursion.
func (n *native) span(i *Interpreter, addr int, spans map[int]int) int {
	if count, ok := spans[addr]; ok {
		return count
	}
	// A recursive call reaches addr again before its span is known, and
	// every early return below leaves this.
	spans[addr] = short
	code := i.function(addr).Code
	count, callee := 0, -1
	for ip := 0; ip < len(code) && count < short; {
		inst := instr.Instruction(code[ip:])
		switch inst.Opcode() {
		case instr.CALL:
			if callee < 0 {
				return short
			}
			count += n.span(i, callee, spans)
		case instr.RETURN_CALL:
			return short
		case instr.BR, instr.BR_IF, instr.BR_TABLE:
			for _, target := range instr.Targets(code, ip) {
				if target <= ip {
					return short
				}
			}
		}
		callee = -1
		if inst.Opcode() == instr.CONST_GET {
			if c := i.constants[inst.Operand(0)]; c.Kind() == types.KindRef {
				if _, ok := i.heap[c.Ref()].(*types.Function); ok {
					callee = c.Ref()
				}
			}
		}
		count++
		ip += inst.Width()
	}
	count = min(count, short)
	spans[addr] = count
	return count
}

// close releases n's reference to its shared runtime.
func (n *native) close() error {
	if n.shared == nil {
		return nil
	}
	n.reader.Detach()
	return n.shared.release()
}

// join moves n onto r, a Pool's runtime, releasing its own; n has never
// entered native code.
func (n *native) join(r *shared) error {
	n.reader.Detach()
	own := n.shared
	n.shared, n.reader = r, r.store.Attach()
	return own.release()
}

// quiesce marks a quiescent point, where n runs no native activation and
// holds no code it read before, then frees what every reader has passed.
func (n *native) quiesce() {
	if n.shared == nil {
		return
	}
	n.reader.Quiesce()
	_ = n.store.Reclaim()
}

// call is the CALL handler's hook for a *types.Function target at addr,
// generated into pushFrame's function-target path. It reports whether it ran
// the call to completion, in which case the caller must not push a frame.
// release and advance mirror the call site's own releaseTarget and ip
// advance, since native completion must apply them exactly as pushFrame
// would.
func (n *native) call(i *Interpreter, addr int, fn *types.Function, release bool, advance int) bool {
	// Bound after construction (Alloc, Store): never compiled. A dormant n
	// has no addresses. A gated one is never entered from Go (gates).
	if addr >= len(n.gates) || n.gates[addr] {
		return false
	}
	return n.attempt(i, addr, fn, release, advance)
}

// attempt is call's slow path; call stays small enough to inline into the
// threaded CALL handlers. Only a direct call reaches it, which carries no
// upvals: a function with captures runs natively only through its closure.
func (n *native) attempt(i *Interpreter, addr int, fn *types.Function, release bool, advance int) bool {
	if release {
		n.see(i, transform.Callee{Function: addr})
	}
	if len(fn.Captures) > 0 || n.depth >= uint64(len(n.ctx.Records)) {
		return false
	}
	n.drain(i)
	if n.store.Code(addr) == nil {
		n.count(i, addr, fn)
		return false
	}

	code := n.store.Code(addr)
	if code == nil {
		n.count(i, addr, fn)
		return false
	}
	// Entry's SBFX unboxes an i64 register argument inline; a heap-promoted
	// one (slot tagged Ref) would misread. Decline and run threaded instead,
	// counting neither an entry nor a deopt.
	bp := i.sp - len(fn.Typ.Params)
	if release {
		bp--
	}
	for index, k := range code.Arguments {
		if k == types.KindI64 && i.stack[bp+index].Kind() == types.KindRef {
			return false
		}
	}
	retire, fault := n.run(i, addr, fn, code, bp, release, advance)
	if fault != nil {
		panic(fault)
	}
	if retire {
		n.retire(addr, code.Tier)
	}
	return true
}

// count tracks cold calls, pool-wide, and requests Baseline compilation once
// the total across every native sharing addr's code reaches threshold.
func (n *native) count(i *Interpreter, addr int, fn *types.Function) {
	total := n.calls[addr].Add(1)
	if total < int64(n.threshold)<<n.raise(addr) || n.hasFailed(addr, jit.Baseline) {
		return
	}
	n.queue.Submit(compile.Unit{Address: addr, Function: fn, Module: n.feedback(addr), Tier: jit.Baseline})
}

// see records the callee seen at the current dynamic CALL: i.fr's own
// address and ip (the CALL's own, per call's doc). A caller past construction
// (bound dynamically) is skipped; its own ip slice is allocated lazily, sized
// to its own code, on first use. The transition is unset -> callee -> mixed
// once a second, different callee is seen; it never moves back, and a mixed
// site keeps its signature until a callee of another one is seen.
func (n *native) see(i *Interpreter, callee transform.Callee) {
	addr, ip := i.fr.addr, i.fr.ip
	if addr >= len(n.callees) {
		return
	}
	if n.callees[addr] == nil {
		n.callees[addr] = make([]transform.Callee, len(i.code[addr]))
	}
	callee.Type = i.function(callee.Function).Typ
	sites := n.callees[addr]
	switch site := sites[ip]; {
	case site == transform.Callee{}:
		sites[ip] = callee
	case site == callee:
	case site.Type != nil && !site.Type.Equals(callee.Type):
		sites[ip] = transform.Callee{Function: mixed}
	default:
		sites[ip] = transform.Callee{Function: mixed, Type: site.Type}
	}
}

// feedback is addr's compile-time snapshot of n.module: its own dynamic CALL
// sites (see), an unseen one absent, a mixed one without its function, and
// its refuted guard sites (record). The snapshot is never mutated after
// Submit: fresh maps every call.
func (n *native) feedback(addr int) transform.Module {
	m := n.module
	if addr >= len(n.callees) {
		return m
	}
	for ip, callee := range n.callees[addr] {
		switch {
		case callee == transform.Callee{}:
			continue
		case callee.Function == mixed:
			callee = transform.Callee{Type: callee.Type}
		}
		if m.Callees == nil {
			m.Callees = map[int]transform.Callee{}
		}
		m.Callees[ip] = callee
	}
	for ip, ok := range n.refuted[addr] {
		if !ok {
			continue
		}
		if m.Refuted == nil {
			m.Refuted = map[int]bool{}
		}
		m.Refuted[ip] = true
	}
	return m
}

// same reports whether feedback snapshots a and b hold the same callees and
// refuted sites.
func same(a, b transform.Module) bool {
	return maps.Equal(a.Callees, b.Callees) && maps.Equal(a.Refuted, b.Refuted)
}

// moved reports whether addr's feedback changed since its code at tier was
// built: a recompile at that tier has new input.
func (n *native) moved(addr int, tier jit.Tier) bool {
	return !same(n.built[addr][tier-1], n.feedback(addr))
}

// record notes exit's site, a guard that failed, so the next compile of its
// function translates the site generic (transform.Module.Refuted).
func (n *native) record(i *Interpreter, exit jit.Exit) {
	addr, ip := exit.Frame.Address, exit.Frame.IP
	if addr >= len(n.refuted) {
		return
	}
	if n.refuted[addr] == nil {
		n.refuted[addr] = make([]bool, len(i.code[addr]))
	}
	n.refuted[addr][ip] = true
}

// refute counts one refuted speculation against addr's code at tier and
// reports whether it retires: at once when feedback moved since the code was
// built, which a recompile can use, else at the tolerance count.
func (n *native) refute(addr int, tier jit.Tier) bool {
	n.refutes[addr]++
	return n.moved(addr, tier) || n.refutes[addr] >= tolerance
}

// retire unpublishes addr's code at tier and restarts its counters. The tier
// fails only when feedback has not moved since the code was built: a
// recompile would get no new input.
func (n *native) retire(addr int, tier jit.Tier) {
	n.store.Retire(addr)
	if !n.moved(addr, tier) {
		n.markFailed(addr, tier)
	}
	if n.auto && n.backoffs[addr] < steps {
		n.backoffs[addr]++
	}
	n.calls[addr].Store(0)
	n.entries[addr], n.refutes[addr], n.ledgers[addr] = 0, 0, jit.Ledger{}
}

// raise is the shift addr's retires apply to its call and promotion
// thresholds: 4× per retire under the automatic policy, none when fixed.
func (n *native) raise(addr int) uint {
	return 2 * uint(n.backoffs[addr])
}

// hasFailed reports whether addr's compile at tier permanently failed.
func (n *native) hasFailed(addr int, tier jit.Tier) bool {
	return n.failed[addr][tier-1]
}

// markFailed permanently marks addr's compile at tier as failed.
func (n *native) markFailed(addr int, tier jit.Tier) {
	n.failed[addr][tier-1] = true
	if tier == jit.Baseline {
		// A failed Baseline never republishes: nothing is left to count.
		n.gates[addr] = true
	}
}

// drain publishes completed jobs, records permanent compile failures, and
// tiers every Baseline candidate whose prologue count reached graduate.
// A candidate leaves once it is no longer Baseline or Optimized has failed.
func (n *native) drain(i *Interpreter) {
	for _, job := range n.queue.Drain() {
		if job.Err != nil {
			// A failed OSR unit restores its site's threaded handler; failed
			// tracks entry-0 tiering only.
			if job.Unit.OSR {
				k := key{job.Unit.Address, job.Unit.IP}
				if s, ok := n.sites[k]; ok {
					i.code[s.address][s.ip] = s.inner
					delete(n.sites, k)
				}
			} else if same(job.Unit.Module, n.feedback(job.Unit.Address)) {
				// Feedback that moved since this unit's own snapshot gets
				// another try instead of a permanent failure.
				n.markFailed(job.Unit.Address, job.Unit.Tier)
			}
			outcome := "failed"
			if errors.Is(job.Err, compile.ErrUnsupported) {
				outcome = "unsupported"
			}
			metric(i, metricCompiles, prof.Label{Key: "tier", Value: job.Unit.Tier.String()}, prof.Label{Key: "outcome", Value: outcome})
			continue
		}
		n.store.Publish(job.Code)
		if !job.Unit.OSR {
			n.built[job.Unit.Address][job.Code.Tier-1] = job.Unit.Module
		} else if s, ok := n.sites[key{job.Unit.Address, job.Unit.IP}]; ok {
			s.built = job.Unit.Module
		}
		if job.Code.Tier == jit.Baseline {
			n.nominate(job.Unit.Address)
		}
		metric(i, metricCompiles, prof.Label{Key: "tier", Value: job.Unit.Tier.String()}, prof.Label{Key: "outcome", Value: "ok"})
	}
	// Candidates are pool-wide: a native that never drains a Baseline job
	// still promotes it once its own entries reach graduate.
	n.sweep(func(addr int) bool {
		code := n.store.Code(addr)
		if code == nil || code.Tier != jit.Baseline || n.hasFailed(addr, jit.Optimized) {
			return false
		}
		if n.entries[addr] >= graduate<<n.raise(addr) {
			n.queue.Submit(compile.Unit{Address: addr, Function: i.function(addr), Module: n.feedback(addr), Tier: jit.Optimized})
		}
		return true
	})
}

// settle serves exits from trap through safepoints, releases, bridges, and
// calls; mark is Context.Budget at entry, and the entered activation
// materializes as frame start. ok reports that the activation reached
// TrapReturn; retire reports whether the code retires; fault is a panic a
// call raised past its native caller, which the caller re-raises once native
// code has returned.
//
// Each exit costs the code by its class (jit.Class): a trap nothing; a guard
// its refutation (judge); a bridge or call its price, charged to ledger before it is
// served, which retires the code once its work no longer pays.
func (n *native) settle(i *Interpreter, code *jit.Code, trap jit.Trap, ledger *jit.Ledger, mark int64, start int, deopt func(jit.Exit), refute func() bool) (ok, retire bool, fault any) {
	ctx := n.ctx
	for {
		ledger.Spend(mark - ctx.Budget)
		mark = ctx.Budget
		if trap == jit.TrapReturn {
			return true, false, nil
		}

		// At the entered depth no native call has run since entry: skip
		// Find's locked scan.
		entered := code
		if ctx.Depth != n.depth+1 {
			entered = n.store.Find(ctx.PC())
		}
		exit := entered.Exits[ctx.Exit()]
		if i.profiler != nil {
			metric(i, metricExits, prof.Label{Key: "kind", Value: exit.Kind.String()})
		}
		switch exit.Kind {
		case jit.ExitSafepoint:
			if cancelled(i) {
				// Threaded code reports the cancellation at its next safepoint,
				// as an error no guest handler can catch.
				deopt(exit)
				return false, false, nil
			}
			ctx.Budget += n.quota
			if i.metered() {
				if i.hook != nil || !i.burn() {
					// Threaded code runs the tick before its next instruction,
					// over materialized frames: the hook sees and changes the
					// VM threaded code would, and Run returns the tick's error.
					deopt(exit)
					n.rethread(i, start)
					i.due, i.parked, i.fr.ip = true, i.fr.ip, park
					return false, false, nil
				}
				i.sample(exit.Frame.Address, exit.Frame.IP)
			}
			n.sync(i)
			mark = ctx.Budget
			n.drain(i)
			trap = jit.Resume(ctx)
		case jit.ExitRelease:
			// A release cannot deopt (it maps no frame): its charge takes
			// effect at the next bridge or call.
			ledger.Charge(jit.ClassRelease)
			if ref := types.Boxed(ctx.Read(int(ctx.Depth)-1, exit.Word)).Ref(); ref != 0 {
				i.release(ref)
			}
			n.sync(i)
			trap = jit.Resume(ctx)
		case jit.ExitBridge, jit.ExitBox:
			// Charged before serving: a deopt after a bridge would run the op
			// twice.
			var serve callout
			class := jit.ClassBridge
			if exit.Kind == jit.ExitBridge && callouts[exit.Code] != nil {
				serve, class = callouts[exit.Code], jit.ClassCallout
			}
			if !ledger.Charge(class) {
				deopt(exit)
				return false, true, nil
			}
			switch {
			case serve != nil:
				if !n.callout(i, exit, serve) {
					class = jit.ClassTrap
				}
			case exit.Kind == jit.ExitBridge:
				class = n.bridge(i, exit)
			case !n.widen(i, exit):
				class = jit.ClassTrap
			}
			switch class {
			case jit.ClassBridge, jit.ClassCallout:
				n.sync(i)
				trap = jit.Resume(ctx)
				continue
			case jit.ClassTrap:
				deopt(exit)
				return false, false, nil
			}
			deopt(exit)
			return false, n.judge(entered, code, refute), nil
		case jit.ExitCall:
			ref := n.callee(int(ctx.Depth)-1, exit)
			if i.hook != nil {
				// A served call keeps its native callers' frames
				// unmaterialized below the floor, where a hook reads them.
				deopt(exit)
				n.replay(i, exit, ref)
				n.rethread(i, start)
				return false, false, nil
			}
			addr, ok := target(i, exit, ref)
			if !ok || !n.nests(i, exit, addr) {
				deopt(exit)
				n.replay(i, exit, ref)
				return false, n.judge(entered, code, refute), nil
			}
			if !n.pending(addr) && !ledger.Charge(jit.ClassCall) {
				deopt(exit)
				n.replay(i, exit, ref)
				return false, true, nil
			}
			if fault, ok := n.nest(i, exit, ref, start, deopt); !ok {
				return false, false, fault
			}
			n.sync(i)
			mark = ctx.Budget
			trap = jit.Resume(ctx)
		default:
			deopt(exit)
			if exit.Trap {
				return false, false, nil
			}
			n.record(i, exit)
			return false, n.judge(entered, code, refute), nil
		}
	}
}

// rethread points every frame materialized from start at its observed
// threaded code where its ip has a handler, so its loop headers enter native
// code again.
func (n *native) rethread(i *Interpreter, start int) {
	for at := start; at < i.fp; at++ {
		f := &i.frames[at]
		if code := i.code[f.addr]; f.ip < len(code) && code[f.ip] != nil {
			f.code = code
		}
	}
}

// judge weighs a refuted speculation in entered, the code that took the
// exit, and reports whether the run's own code retires. refute judges the
// run's own code; a native callee's code that is still published is judged
// against its own feedback and count, and retired here.
func (n *native) judge(entered, code *jit.Code, refute func() bool) bool {
	if entered == code {
		return refute()
	}
	if !entered.Retired() && n.refute(entered.Address, entered.Tier) {
		n.retire(entered.Address, entered.Tier)
	}
	return false
}

// pending reports whether addr has no code yet but may still compile: a
// served call to it is its callee's warm-up, which costs the caller nothing.
func (n *native) pending(addr int) bool {
	return addr < len(n.failed) && !n.hasFailed(addr, jit.Baseline) && n.store.Code(addr) == nil
}

// run executes one native call whose frame starts at bp and reports whether
// it should retire and the fault settle reports.
func (n *native) run(i *Interpreter, addr int, fn *types.Function, code *jit.Code, bp int, release bool, advance int) (bool, any) {
	returns := len(fn.Typ.Returns)

	n.load(i, bp, 0)
	ctx := n.ctx
	mark := ctx.Budget

	if i.profiler != nil {
		metric(i, metricEntries, prof.Label{Key: "tier", Value: code.Tier.String()})
	}

	// Threaded code pushed every argument owned, but a return above depth 1
	// releases no borrowed parameter: Go releases each once it returns.
	var buf [4]types.Boxed
	owned := buf[:0]
	if n.depth > 0 {
		if n.borrows[addr] == nil {
			n.borrows[addr] = transform.Borrows(fn)
		}
		for p, b := range n.borrows[addr] {
			if b {
				owned = append(owned, i.stack[bp+p])
			}
		}
	}

	if trap := jit.Enter(code.Entry(), ctx); trap != jit.TrapReturn {
		start := i.fp
		ok, retire, fault := n.settle(i, code, trap, &n.ledgers[addr], mark, start,
			// ip advances the entering (pre-rebuild) frame, which rebuild
			// never writes, so it applies before rebuild retargets i.fr.
			func(exit jit.Exit) { i.fr.ip += advance; n.rebuild(i, exit, start, addr, release) },
			func() bool { return n.refute(addr, code.Tier) },
		)
		if !ok {
			return retire, fault
		}
	} else {
		n.ledgers[addr].Spend(mark - ctx.Budget)
	}
	for _, v := range owned {
		i.releaseBox(v)
	}
	boxRegisters(i, code, bp)
	if release {
		i.release(addr)
	}
	i.sp = bp + returns
	i.fr.ip += advance
	return false, nil
}

// widen heap-boxes exit.Word, a wide i64, into Context.Results[0] as
// threaded boxI64 does; a panic (heap exhaustion) declines.
func (n *native) widen(i *Interpreter, exit jit.Exit) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	k := int(n.ctx.Depth) - 1
	v := int64(n.ctx.Read(k, exit.Word))
	n.ctx.Results[0] = uint64(i.boxI64(v))
	return true
}

// boxRegisters boxes each i64 register-convention result at bp, which the
// Go entry stub left as a raw word, as threaded RETURN would: inline or heap.
func boxRegisters(i *Interpreter, code *jit.Code, bp int) {
	for index, k := range code.Registers {
		if k == types.KindI64 {
			i.stack[bp+index] = i.boxI64(int64(i.stack[bp+index]))
		}
	}
}

// bridge runs exit's threaded handler once against its boxed operands and
// reports the exit's class: jit.ClassBridge when native code resumes,
// jit.ClassTrap when the op raised its trap or is a control transfer, and
// jit.ClassGuard when it declined without running. It leaves native
// registers untouched; asm.Resume restores them.
//
// A handler that panics declines: threaded code runs the op again after the
// deopt and reports its trap. Before running it, bridge saves each heap
// operand and its count and retains every ref operand for the handler; on a
// panic it releases each back to its saved count, whatever the handler
// released or overwrote. A declined attempt can leave changed only heap and
// count storage growth, stack slots above the frame, and i.frames[i.fp]:
// bridgeable admits no handler that writes heap contents, allocates, or
// retains anything but its operands before its last panic point. An operand
// that is a host view declines before the handler runs, since its
// conversions are host code.
func (n *native) bridge(i *Interpreter, exit jit.Exit) jit.Class {
	if !bridgeable[exit.Code] {
		// A control transfer is the program leaving on its own; any other
		// denied op deopts unrun.
		if exit.Code.Writes(instr.Branch) {
			return jit.ClassTrap
		}
		return jit.ClassGuard
	}
	if i.fp >= len(i.frames) {
		return jit.ClassGuard
	}

	ctx := n.ctx
	k := int(ctx.Depth) - 1
	m := exit.Frame
	bp := n.frameBase(i, k)
	sp := bp + slots(i.function(m.Address))

	tail := m.Stack[len(m.Stack)-exit.Pops:]
	var buf [8]hold
	holds := buf[:0]
	for j, o := range tail {
		v := fromWord(i, o.Value.Kind, ctx.Read(k, o.Value))
		i.stack[sp+j] = v
		if v.Kind() != types.KindRef {
			continue
		}
		// A boxed wide i64 is fresh: the handler owns its one reference.
		count := 0
		if o.Value.Kind == types.KindRef {
			if hosted(i.heap[v.Ref()]) {
				rollback(i, holds)
				return jit.ClassGuard
			}
			count = i.rc[v.Ref()]
		}
		holds = append(holds, hold{ref: v.Ref(), count: count})
	}
	for j, o := range tail {
		if o.Value.Kind == types.KindRef {
			i.retainBox(i.stack[sp+j])
		}
	}

	savedFr, savedSP := i.fr, i.sp
	f := &i.frames[i.fp]
	*f = frame{addr: m.Address, code: n.exactCode(i, m.Address), bp: bp, ip: m.IP}
	i.fr = f
	i.sp = sp + len(tail)

	ok := exec(i, f)
	if ok {
		for j, kind := range exit.Results {
			ctx.Results[j] = toWord(i, kind, i.stack[i.sp-len(exit.Results)+j])
		}
		// Native code handed an adopted operand's own reference to the op;
		// the handler consumed the retain above instead.
		for _, o := range tail[len(tail)-exit.Adopts:] {
			if o.Value.Kind == types.KindRef {
				i.releaseBox(types.Boxed(ctx.Read(k, o.Value)))
			}
		}
	} else {
		rollback(i, holds)
	}
	i.fr, i.sp = savedFr, savedSP
	if !ok {
		return jit.ClassTrap
	}
	return jit.ClassBridge
}

// callout serves exit, a bridge of an operation callouts serve with serve,
// and reports whether it completed: it allocates serve's object and hands
// native code its one reference through Context.Results. A panic (heap
// exhaustion, or the operation's own fault) happens before anything is
// published and leaves every operand's reference count as it was; threaded code runs
// the operation again and reports it.
func (n *native) callout(i *Interpreter, exit jit.Exit, serve callout) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	ctx := n.ctx
	k := int(ctx.Depth) - 1
	m := exit.Frame
	var args [2]types.Boxed
	for j, o := range m.Stack[len(m.Stack)-exit.Pops:] {
		args[j] = fromWord(i, o.Value.Kind, ctx.Read(k, o.Value))
	}
	v := serve(i, i.function(m.Address).Code[m.IP:], args)
	ctx.Results[0] = toWord(i, exit.Results[0], types.BoxRef(i.alloc(v)))
	return true
}

// index is the two-byte type index the operation at code[0] carries, read
// as the threader reads it.
func index(code []byte) int {
	return int(*(*uint16)(unsafe.Pointer(&code[1])))
}

// rollback releases each held operand back down to its saved count.
func rollback(i *Interpreter, holds []hold) {
	for _, h := range holds {
		for i.rc[h.ref] > h.count {
			i.release(h.ref)
		}
	}
}

// exec runs f's own instruction and reports whether it completed; a panic is
// recovered and reported as false.
func exec(i *Interpreter, f *frame) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	f.code[f.ip](i)
	return true
}

// hosted reports whether v is a host view: its operations run a Registry's
// conversions, host code a declined attempt would run a second time.
func hosted(v types.Value) bool {
	switch v.(type) {
	case *HostStruct, *HostArray, *HostMap:
		return true
	default:
		return false
	}
}

// rebuild materializes every native activation of the current run, from
// depth upward, as frames from start and positions the interpreter at the
// innermost one. The run's first activation was entered through ref, and
// release reports whether it owns that reference.
func (n *native) rebuild(i *Interpreter, exit jit.Exit, start, ref int, release bool) {
	ctx := n.ctx
	floor, depth := int(n.depth), int(ctx.Depth)

	maps := make([]jit.Frame, depth-floor)
	refs := make([]int, depth-floor)
	owns := make([]bool, depth-floor)
	// lent[j] are the parameter slots activation floor+j was entered with
	// but does not own; the first is never lent one, since threaded code
	// pushed its arguments owned.
	lent := make([][]int, depth-floor)
	maps[depth-floor-1] = exit.Frame
	refs[0], owns[0] = ref, release
	for k := depth - 2; k >= floor; k-- {
		code := n.store.Find(ctx.Records[k+1].PC)
		e := code.Exits[ctx.Records[k].Exit]
		maps[k-floor] = e.Frame
		refs[k-floor+1] = n.callee(k, e)
		owns[k-floor+1] = e.Owned
		lent[k-floor+1] = e.Lent
	}

	for j := range maps {
		n.frame(i, start+j, floor+j, maps[j], refs[j], owns[j])
	}
	for j := 1; j < len(maps); j++ {
		bp := i.frames[start+j].bp
		for _, p := range lent[j] {
			i.retainBox(i.stack[bp+p])
		}
	}
	i.fp = start + len(maps)
	i.fr = &i.frames[i.fp-1]
	ctx.Abandon()
}

// replay pushes exit's callee over its arguments at i.fr's CALL, which the
// interpreter then runs: it retains each lent argument and a borrowed ref,
// since the CALL adopts both. A closure callee is counted and recorded here,
// since the threaded closure CALL has no native hook; so is brief code, whose
// hook is gated.
func (n *native) replay(i *Interpreter, exit jit.Exit, ref int) {
	for _, p := range exit.Lent {
		i.retainBox(i.stack[i.sp+p])
	}
	i.sp += exit.Args
	boxed := types.BoxRef(ref)
	if !exit.Owned {
		i.retainBox(boxed)
	}
	i.stack[i.sp] = boxed
	i.sp++
	// CALL is one byte (interp.go's handler walk relies on the same fact),
	// so its own ip is the map's IP, recorded past it, minus one.
	i.fr.ip--
	if ref > 0 && ref < len(i.heap) {
		switch callee := i.heap[ref].(type) {
		case *types.Closure:
			addr := int(callee.Fn)
			n.see(i, transform.Callee{Function: addr, Closure: true})
			if n.store.Code(addr) == nil {
				n.count(i, addr, i.function(addr))
			}
		case *types.Function:
			if ref < len(n.briefs) && n.briefs[ref] && n.store.Code(ref) == nil {
				n.count(i, ref, callee)
			}
		}
	}
}

// target resolves exit's callee, the value ref, to the address of the
// function it runs. A generic call, which names none, is served only for a
// function or closure that takes the call's arguments and returns the kinds
// it reads back; any other callee ok reports false and the caller deoptimizes
// to run its own CALL.
func target(i *Interpreter, exit jit.Exit, ref int) (addr int, ok bool) {
	if exit.Callee != 0 {
		return exit.Callee, true
	}
	if ref <= 0 || ref >= len(i.heap) {
		return 0, false
	}
	var typ *types.FunctionType
	switch callee := i.heap[ref].(type) {
	case *types.Function:
		addr, typ = ref, callee.Typ
	case *types.Closure:
		addr, typ = int(callee.Fn), callee.Typ
	default:
		return 0, false
	}
	if len(typ.Params) != exit.Args || len(typ.Returns) != len(exit.Returns) {
		return 0, false
	}
	for j, t := range typ.Returns {
		if t.Kind() != exit.Returns[j] {
			return 0, false
		}
	}
	return addr, true
}

// nests reports whether exit's call to the function at addr can run while its
// caller stays suspended: not a coroutine, whose CALL returns a handle instead
// of the results native code expects, and with room to push the callee.
func (n *native) nests(i *Interpreter, exit jit.Exit, addr int) bool {
	if addr < len(i.coros) && i.coros[addr] {
		return false
	}
	k := int(n.ctx.Depth) - 1
	bp := n.frameBase(i, k)
	return bp+slots(i.function(exit.Frame.Address))+len(exit.Frame.Stack)+exit.Args < len(i.stack)
}

// nest runs exit's call in the interpreter while its native caller stays
// suspended, and reports whether native code resumes. A frame standing in
// for the caller runs only its CALL; the native activations below it keep
// their frame slots from start, which no handler search or unwinding crosses
// (Interpreter.floor), and native entries meanwhile start their records
// above them (depth). On return the results go where the call's exit map
// reads them and the suspended state comes back. Anything else leaving the
// callee materializes the caller under the callee's live frames: a
// cancellation continues threaded, a THROW whose search the floor stopped
// runs again over the materialized frames, and any other panic is fault.
func (n *native) nest(i *Interpreter, exit jit.Exit, ref, start int, deopt func(jit.Exit)) (fault any, ok bool) {
	ctx := n.ctx
	k := int(ctx.Depth) - 1
	m := exit.Frame
	at := start + k - int(n.depth)
	bp := n.frameBase(i, k)

	state, limit, depth, floor := ctx.State, ctx.Limit, n.depth, i.floor
	fr, fp, sp, saved := i.fr, i.fp, i.sp, i.frames[at]

	code := n.exactCode(i, m.Address)
	i.frames[at] = frame{addr: m.Address, code: code[:m.IP], bp: bp, ip: m.IP, returns: m.Returns}
	i.fr, i.fp = &i.frames[at], at+1
	args := bp + slots(i.function(m.Address)) + len(m.Stack)
	i.sp = args
	n.replay(i, exit, ref)
	for _, p := range exit.Kept {
		i.retainBox(i.stack[args+p])
	}
	n.depth, i.floor = uint64(k+1), at+1
	fault, err := dispatch(i)
	n.depth, i.floor = depth, floor

	if err == nil && fault == nil {
		for j, kind := range exit.Results {
			ctx.Results[j] = toWord(i, kind, i.stack[i.sp-len(exit.Results)+j])
		}
		i.fr, i.fp, i.sp, i.frames[at] = fr, fp, sp, saved
		ctx.State, ctx.Limit, ctx.Depth = state, limit, uint64(k+1)
		return nil, true
	}

	// The caller never resumes to release what it kept.
	for _, p := range exit.Kept {
		i.releaseBox(i.stack[args+p])
	}
	// deopt materializes the caller from the state it was suspended in;
	// the callee's frames above it stay as they are.
	ip, live := i.frames[at].ip, [...]int{i.fp, i.sp}
	inner := i.fr
	ctx.State, ctx.Depth = state, uint64(k+1)
	i.fr, i.fp, i.sp, i.frames[at] = fr, fp, sp, saved
	deopt(exit)
	i.frames[at].ip = ip
	i.fr, i.fp, i.sp = inner, live[0], live[1]
	if _, ok := fault.(escape); ok {
		// THROW popped its exception and stopped at the floor before any
		// other effect: pushing it back runs THROW again over every frame.
		i.sp++
		fault = nil
	}
	return fault, false
}

// dispatch runs the threaded loop for nest until its stand-in frame's CALL
// completes, reporting a cancellation as err and any panic no handler above
// the floor caught as fault.
func dispatch(i *Interpreter) (fault any, err error) {
	defer func() {
		fault = recover()
	}()
	for caught := true; caught; {
		caught, err = i.dispatch()
	}
	return nil, err
}

// callee is the reference activation k's ExitCall e calls through: the
// closure it maps, or else its function.
func (n *native) callee(k int, e jit.Exit) int {
	if e.Target == nil {
		return e.Callee
	}
	return types.Boxed(n.ctx.Read(k, *e.Target)).Ref()
}

// frame materializes activation k from m as frame at, entered through ref.
// release reports whether k owns ref: the run's first activation follows the
// entering call site, and every other one follows the call site's own Owned
// decision in the caller's compiled code.
func (n *native) frame(i *Interpreter, at, k int, m jit.Frame, ref int, release bool) {
	ctx := n.ctx
	f := &i.frames[at]
	f.addr = m.Address
	f.code = n.exactCode(i, m.Address)
	f.ref = ref
	f.release = release
	f.bp = n.frameBase(i, k)
	f.returns = m.Returns
	f.ip = m.IP
	f.upvals = i.upvals(ref, m.Address)
	f.coro = 0

	sp := f.bp + slots(i.function(m.Address))
	for j, o := range m.Stack {
		boxed := fromWord(i, o.Value.Kind, ctx.Read(k, o.Value))
		// A boxed wide i64 is fresh and already owned; only a ref borrows.
		if !o.Owned && o.Value.Kind == types.KindRef {
			i.retainBox(boxed)
		}
		i.stack[sp+j] = boxed
	}
	sp += len(m.Stack)

	for _, l := range m.Locals {
		boxed := fromWord(i, l.Value.Kind, ctx.Read(k, l.Value))
		addr := f.bp + l.Index
		i.releaseBox(i.stack[addr])
		i.stack[addr] = boxed
	}

	if k == int(ctx.Depth)-1 {
		if sp > len(i.stack) {
			panic(ErrStackOverflow)
		}
		i.sp = sp
	}
}

// exactCode returns addr's threaded code compiled exact, building and caching
// it the first time a deoptimization at addr needs it.
func (n *native) exactCode(i *Interpreter, addr int) []func(*Interpreter) {
	if code := n.exact[addr]; code != nil {
		return code
	}
	code := n.compile(i.function(addr), true)
	n.exact[addr] = code
	return code
}

// fromWord converts a native word to the interpreter's Boxed representation.
// Wide i64 values use the normal heap-promotion path.
func fromWord(i *Interpreter, kind types.Kind, word uint64) types.Boxed {
	if kind == types.KindI64 {
		return i.boxI64(int64(word))
	}
	return types.BoxWord(kind, word)
}

// toWord converts v, a value a handler pushed, to the native word of kind,
// consuming a heap-boxed i64's reference; fromWord is its inverse.
func toWord(i *Interpreter, kind types.Kind, v types.Boxed) uint64 {
	if kind == types.KindI64 {
		return uint64(i.unboxI64(v))
	}
	return v.Word()
}

func metric(i *Interpreter, name string, labels ...prof.Label) {
	if i.profiler == nil {
		return
	}
	i.samples.AddMetric(name, 1, labels...)
}

// cancelled reports whether i's active Run context is done, without
// blocking; ExitSafepoint is the only point native code polls it.
func cancelled(i *Interpreter) bool {
	if i.done == nil {
		return false
	}
	select {
	case <-i.done:
		return true
	default:
		return false
	}
}

// base is the address of s's backing array. The heap's is jit.SizeofValue
// bytes (an interface word pair) per address, read-only to native code except
// for the non-pointer element and field words a guarded exec op writes in place.
func base[T any](s []T) uintptr {
	return uintptr(unsafe.Pointer(unsafe.SliceData(s)))
}

// end is the address one past the last word of the operand stack s.
func end(s []types.Boxed) uintptr {
	return base(s) + uintptr(len(s))*unsafe.Sizeof(types.Boxed(0))
}

// frameBase is the stack index where native activation k's frame starts.
func (n *native) frameBase(i *Interpreter, k int) int {
	return int((n.ctx.Records[k].FB - base(i.stack)) / unsafe.Sizeof(types.Boxed(0)))
}

// load points the native context at i for an entry whose frame starts at stack
// index bp. spare is how many more nested activations the entry may start: an
// OSR entry replaces the running frame instead of pushing one.
func (n *native) load(i *Interpreter, bp, spare int) {
	if n.ctx == nil {
		ctx, err := jit.NewContext(nativeStack)
		if err != nil {
			panic(err)
		}
		ctx.Budget = n.quota
		n.ctx = ctx
	}
	ctx := n.ctx
	n.sync(i)
	ctx.Globals = base(i.globals)
	ctx.Natives = n.store.Natives()
	ctx.Entries = base(n.entries)
	ctx.Top = end(i.stack)
	ctx.FB = base(i.stack[bp:])
	ctx.Limit = uint64(min(len(ctx.Records), int(n.depth)+len(i.frames)-i.fp+spare))
	ctx.Depth = n.depth
}

// sync points the native context at i's heap and counts, which a bridge or a
// call may have moved.
func (n *native) sync(i *Interpreter) {
	n.ctx.Heap = base(i.heap)
	n.ctx.RC = base(i.rc)
}

// slots is fn's parameter and local count, without Declared's allocation.
func slots(fn *types.Function) int {
	if fn.Typ == nil {
		return len(fn.Locals)
	}
	return len(fn.Typ.Params) + len(fn.Locals)
}
