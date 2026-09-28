package interp

import (
	"errors"
	"maps"
	"math"
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
	ctx *jit.Context
	*shared

	threshold int
	// entries is native-writable: Context.Entries points at it, and every
	// Baseline function prologue increments its own address there, including
	// native-to-native entries.
	entries []int64
	deopts  []int
	// bridges carries each address's unamortized bridge count and native work
	// across entries.
	bridges []bridge
	// failed records permanent compile failure per address and tier.
	failed [][2]bool
	// sites indexes every observed OSR site by (address, ip): drain looks a
	// failed OSR unit's site up here to restore its threaded handler.
	sites map[key]*site
	// callees records each dynamic CALL's one observed callee, by caller
	// address then ip: the zero Callee means unseen, a mixed Function more
	// than one seen, any other the one callee seen there so far. A caller's
	// own ip slice is allocated lazily, sized to its own code. feedback reads
	// this to snapshot a unit's own single-callee sites at submit time.
	callees [][]transform.Callee

	// exact caches unfused threaded code for materialized frames, by address.
	exact [][]func(*Interpreter)

	// compile is captured once so deoptimization does not add a static
	// dependency from generated threaded handlers back to their compiler.
	// Calling i.compile here directly creates the threaded/fusions
	// initialization cycle.
	compile func(fn *types.Function, exact bool) []func(*Interpreter)
}

// nativeStack is the native stack size per interpreter.
const nativeStack = 1 << 20

// budget is the back-edge count between safepoints.
const budget = 1 << 16

// refute is the deopt threshold that retires native code.
const refute = 8

// resume is the consecutive unamortized-bridge limit before a site retires.
// It uses the same retry count as refute: native work must pay for the Go round trip.
const resume = refute

// amortize is the native work (back edges, calls, returns) since the
// previous bridge that makes the next one count as paid for rather than
// against resume.
const amortize = 4

// graduate is the Baseline entry count that tiers an address to Optimized.
const graduate = 1024

// bridge carries unamortized work for one CALL address or OSR site across entries.
type bridge struct {
	count int
	work  int64
	mark  int64
}

func (b *bridge) spend(ctx *jit.Context) {
	b.work += b.mark - ctx.Budget
	b.mark = ctx.Budget
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
	// count is len(candidates), readable without mu so an empty sweep —
	// every call's common case — takes no lock.
	count atomic.Int64

	// calls is the pool-wide CALL count per address, added to by every
	// native sharing r while the address has no published code: a pooled
	// workload's calls compile Baseline after about threshold total calls,
	// not threshold calls to any one interpreter.
	calls []atomic.Int64

	// sites is the pool-wide entry count per OSR/entry site, added to by
	// every native sharing r while the site is unsubmitted; built lazily
	// under mu.
	sites map[key]*atomic.Int64

	refs atomic.Int64
}

// total returns r's pool-wide entry counter for the OSR/entry site k,
// creating it for the first native that observes k.
func (r *shared) total(k key) *atomic.Int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sites == nil {
		r.sites = map[key]*atomic.Int64{}
	}
	t := r.sites[k]
	if t == nil {
		t = new(atomic.Int64)
		r.sites[k] = t
	}
	return t
}

// nominate adds addr to r's candidates, once.
func (r *shared) nominate(addr int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !slices.Contains(r.candidates, addr) {
		r.candidates = append(r.candidates, addr)
		r.count.Add(1)
	}
}

// sweep calls visit on every candidate and keeps only the ones it reports
// live, under one lock for the whole pass.
func (r *shared) sweep(visit func(addr int) (live bool)) {
	if r.count.Load() == 0 {
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
	r.count.Store(int64(len(live)))
}

// mixed marks a dynamic CALL site (native.callees) that has seen more than
// one callee: it never speculates.
const mixed = -1

const (
	metricCompiles = "vm_jit_compiles_total"
	metricEntries  = "vm_jit_entries_total"
	metricExits    = "vm_jit_exits_total"
)

// jitEnabled reports whether opt selects the JIT: WithThreshold(n) with n >=
// 0, on arm64, without WithHook or WithFuel (their per-tick semantics need
// interpreter frames).
func jitEnabled(opt option) bool {
	return opt.threshold >= 0 && runtime.GOARCH == "arm64" && opt.hook == nil && opt.fuel == 0
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

func newNative(i *Interpreter, threshold int) *native {
	ctx, err := jit.NewContext(nativeStack)
	if err != nil {
		panic(err)
	}
	n := &native{
		ctx:       ctx,
		shared:    newShared(i),
		threshold: threshold,
		entries:   make([]int64, len(i.code)),
		deopts:    make([]int, len(i.code)),
		bridges:   make([]bridge, len(i.code)),
		failed:    make([][2]bool, len(i.code)),
		exact:     make([][]func(*Interpreter), len(i.code)),
		sites:     map[key]*site{},
		callees:   make([][]transform.Callee, len(i.code)),
		compile:   i.compile,
	}
	// OSR observes every loop header of every function i compiled at
	// construction, module code (address 0) included; a function bound
	// later (a dynamic closure) is not.
	n.observe(i, 0, i.module)
	for addr, obj := range n.module.Objects {
		if obj.Function != nil {
			n.observe(i, addr, obj.Function)
		}
	}
	return n
}

// close releases n's reference to its shared runtime.
func (n *native) close() error {
	return n.shared.release()
}

// call is the CALL handler's hook for a *types.Function target at addr,
// generated into pushFrame's function-target path. It reports whether it ran
// the call to completion, in which case the caller must not push a frame.
// release and advance mirror the call site's own releaseTarget and ip
// advance, since native completion must apply them exactly as pushFrame
// would.
func (n *native) call(i *Interpreter, addr int, fn *types.Function, release bool, advance int) bool {
	// Bound after construction (Alloc, Store): never compiled. A failed
	// Baseline never republishes: nothing is left to count.
	if addr >= len(n.failed) || n.failed[addr][jit.Baseline-1] {
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
	if len(fn.Captures) > 0 {
		return false
	}
	n.drain(i)
	if n.store.Code(addr) == nil {
		n.count(i, addr, fn)
		return false
	}

	n.store.Enter()
	// Re-read after Enter; the code may have been retired between lookups.
	code := n.store.Code(addr)
	if code == nil {
		n.store.Leave()
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
			n.store.Leave()
			return false
		}
	}
	retire := n.run(i, addr, fn, code, bp, release, advance)
	n.store.Leave()
	if retire {
		n.store.Retire(addr)
		// The unchanged input has no new feedback for a recompile at this tier.
		n.markFailed(addr, code.Tier)
		// Counters restart; the failed tier stays blocked.
		n.calls[addr].Store(0)
		n.entries[addr], n.deopts[addr], n.bridges[addr] = 0, 0, bridge{}
	}
	_ = n.store.Reclaim()
	return true
}

// count tracks cold calls, pool-wide, and requests Baseline compilation once
// the total across every native sharing addr's code reaches threshold.
func (n *native) count(i *Interpreter, addr int, fn *types.Function) {
	total := n.calls[addr].Add(1)
	if total < int64(n.threshold) || n.hasFailed(addr, jit.Baseline) {
		return
	}
	n.queue.Submit(compile.Unit{Address: addr, Function: fn, Module: n.feedback(addr), Tier: jit.Baseline})
}

// see records the one callee seen at the current dynamic CALL: i.fr's own
// address and ip (the CALL's own, per call's doc). A caller past construction
// (bound dynamically) is skipped; its own ip slice is allocated lazily, sized
// to its own code, on first use. The transition is unset -> callee -> mixed
// once a second, different callee is seen; it never moves back.
func (n *native) see(i *Interpreter, callee transform.Callee) {
	addr, ip := i.fr.addr, i.fr.ip
	if addr >= len(n.callees) {
		return
	}
	if n.callees[addr] == nil {
		n.callees[addr] = make([]transform.Callee, len(i.code[addr]))
	}
	sites := n.callees[addr]
	switch sites[ip] {
	case transform.Callee{}:
		sites[ip] = callee
	case callee, transform.Callee{Function: mixed}:
	default:
		sites[ip] = transform.Callee{Function: mixed}
	}
}

// feedback is addr's compile-time snapshot of n.module: its own single-callee
// dynamic CALL sites (see), an unseen or mixed one absent. The snapshot is
// never mutated after Submit: a fresh map every call.
func (n *native) feedback(addr int) transform.Module {
	m := n.module
	if addr >= len(n.callees) || n.callees[addr] == nil {
		return m
	}
	callees := map[int]transform.Callee{}
	for ip, callee := range n.callees[addr] {
		if callee.Function > 0 {
			callees[ip] = callee
		}
	}
	if len(callees) > 0 {
		m.Callees = callees
	}
	return m
}

// refute counts deopts; refute retires the code at its current tier.
func (n *native) refute(addr int) bool {
	n.deopts[addr]++
	return n.deopts[addr] >= refute
}

// pending reports whether addr has no published code yet but may still get
// Baseline code.
func (n *native) pending(addr int) bool {
	return addr < len(n.failed) && !n.hasFailed(addr, jit.Baseline) && n.store.Code(addr) == nil
}

// hasFailed reports whether addr's compile at tier permanently failed.
func (n *native) hasFailed(addr int, tier jit.Tier) bool {
	return n.failed[addr][tier-1]
}

// markFailed permanently marks addr's compile at tier as failed.
func (n *native) markFailed(addr int, tier jit.Tier) {
	n.failed[addr][tier-1] = true
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
				k := key{job.Unit.Address, job.Unit.Entry}
				if s, ok := n.sites[k]; ok {
					i.code[s.address][s.ip] = s.inner
					delete(n.sites, k)
				}
			} else if maps.Equal(job.Unit.Module.Callees, n.feedback(job.Unit.Address).Callees) {
				// Feedback that moved since this unit's own snapshot gets
				// another try instead of a permanent failure.
				n.markFailed(job.Unit.Address, job.Unit.Tier)
			}
			outcome := "failed"
			if errors.Is(job.Err, compile.ErrUnsupported) {
				outcome = "unsupported"
			}
			n.metric(i, metricCompiles, prof.Label{Key: "tier", Value: job.Unit.Tier.String()}, prof.Label{Key: "outcome", Value: outcome})
			continue
		}
		n.store.Publish(job.Code)
		if job.Code.Tier == jit.Baseline {
			n.nominate(job.Unit.Address)
		}
		n.metric(i, metricCompiles, prof.Label{Key: "tier", Value: job.Unit.Tier.String()}, prof.Label{Key: "outcome", Value: "ok"})
	}
	// Candidates are pool-wide: a native that never drains a Baseline job
	// still promotes it once its own entries reach graduate.
	n.sweep(func(addr int) bool {
		code := n.store.Code(addr)
		if code == nil || code.Tier != jit.Baseline || n.hasFailed(addr, jit.Optimized) {
			return false
		}
		if n.entries[addr] >= graduate {
			n.queue.Submit(compile.Unit{Address: addr, Function: i.function(addr), Module: n.feedback(addr), Tier: jit.Optimized})
		}
		return true
	})
}

// settle serves exits from trap through safepoints, releases, and bridges.
// ok reports that the activation reached TrapReturn; retire reports whether
// the code retires.
func (n *native) settle(i *Interpreter, code *jit.Code, trap jit.Trap, account *bridge, deopt func(jit.Exit), refute func() bool) (ok, retire bool) {
	ctx := n.ctx
	account.mark = budget
	for {
		if trap == jit.TrapReturn {
			account.spend(ctx)
			if account.work >= amortize {
				account.count = 0
				account.work = 0
			}
			return true, false
		}

		// At Depth 1 no native call has run since entry: skip Find's locked scan.
		entered := code
		if ctx.Depth != 1 {
			entered = n.store.Find(ctx.PC())
		}
		exit := entered.Exits[ctx.Exit()]
		if i.profiler != nil {
			n.metric(i, metricExits, prof.Label{Key: "kind", Value: exit.Kind.String()})
		}
		switch exit.Kind {
		case jit.ExitSafepoint:
			account.spend(ctx)
			if cancelled(i) {
				// Threaded code reports the cancellation at its next safepoint,
				// as an error no guest handler can catch.
				deopt(exit)
				return false, refute()
			}
			ctx.Heap = heapBase(i.heap)
			ctx.RC = rcBase(i.rc)
			ctx.Budget = budget
			account.mark = ctx.Budget
			n.drain(i)
			trap = jit.Resume(ctx)
		case jit.ExitRelease:
			if ref := types.Boxed(ctx.Read(int(ctx.Depth)-1, exit.Word)).Ref(); ref != 0 {
				i.release(ref)
			}
			ctx.Heap = heapBase(i.heap)
			ctx.RC = rcBase(i.rc)
			trap = jit.Resume(ctx)
		case jit.ExitBridge, jit.ExitBox:
			account.spend(ctx)
			// Checked before serving: a deopt after a bridge would run the op twice.
			served := false
			if account.count < resume {
				if exit.Kind == jit.ExitBox {
					served = n.widen(i, exit)
				} else {
					served = bridgeable[exit.Code] && n.bridge(i, exit)
				}
			}
			if served {
				if account.work >= amortize {
					account.count = 0
					n.bridges[exit.Frame.Address] = bridge{}
				} else {
					account.count++
				}
				account.work = 0
				account.mark = ctx.Budget
				ctx.Heap = heapBase(i.heap)
				ctx.RC = rcBase(i.rc)
				trap = jit.Resume(ctx)
				continue
			}
			deopt(exit)
			if account.count >= resume {
				return false, true
			}
			return false, refute()
		case jit.ExitCall:
			deopt(exit)
			// A callee whose Baseline is still pending is no refuted
			// speculation and no unpaid bridge.
			if n.pending(exit.Callee) {
				account.count = 0
				return false, false
			}
			return false, refute()
		default:
			deopt(exit)
			return false, refute()
		}
	}
}

// run executes one native call whose frame starts at bp and reports whether
// it should retire.
func (n *native) run(i *Interpreter, addr int, fn *types.Function, code *jit.Code, bp int, release bool, advance int) bool {
	returns := len(fn.Typ.Returns)

	ctx := n.ctx
	ctx.Heap = heapBase(i.heap)
	ctx.Globals = base(i.globals)
	ctx.RC = rcBase(i.rc)
	ctx.Natives = n.store.Natives()
	ctx.Entries = entry(n.entries)
	ctx.Top = end(i.stack)
	ctx.FB = base(i.stack[bp:])
	ctx.Limit = uint64(min(len(ctx.Records), len(i.frames)-i.fp))
	ctx.Budget = budget
	ctx.Depth = 0

	if i.profiler != nil {
		n.metric(i, metricEntries, prof.Label{Key: "tier", Value: code.Tier.String()})
	}

	if trap := jit.Enter(code.Entry(), ctx); trap != jit.TrapReturn {
		ok, retire := n.settle(i, code, trap, &n.bridges[addr],
			// ip advances the entering (pre-rebuild) frame, which rebuild
			// never writes, so it applies before rebuild retargets i.fr.
			func(exit jit.Exit) { i.fr.ip += advance; n.rebuild(i, exit, i.fp, addr, release) },
			func() bool { return n.refute(addr) },
		)
		if !ok {
			return retire
		}
	}
	boxRegisters(i, code, bp)
	if release {
		i.release(addr)
	}
	i.sp = bp + returns
	i.fr.ip += advance
	return false
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

// bridge runs exit's threaded handler once against its boxed operands and
// reports whether native code resumes. It leaves native registers untouched;
// asm.Resume restores them.
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
func (n *native) bridge(i *Interpreter, exit jit.Exit) bool {
	if i.fp >= len(i.frames) {
		return false
	}

	ctx := n.ctx
	k := int(ctx.Depth) - 1
	m := exit.Frame
	bp := int((ctx.Records[k].FB - base(i.stack)) / unsafe.Sizeof(types.Boxed(0)))
	sp := bp + slots(i.function(m.Address))

	tail := m.Stack[len(m.Stack)-exit.Pops:]
	var buf [8]hold
	holds := buf[:0]
	for j, o := range tail {
		v := n.box(i, o.Value.Kind, ctx.Read(k, o.Value))
		i.stack[sp+j] = v
		if v.Kind() != types.KindRef {
			continue
		}
		// A boxed wide i64 is fresh: the handler owns its one reference.
		count := 0
		if o.Value.Kind == types.KindRef {
			if hosted(i.heap[v.Ref()]) {
				restore(i, holds)
				return false
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
			ctx.Results[j] = n.unbox(i, kind, i.stack[i.sp-len(exit.Results)+j])
		}
		// Native code handed an adopted operand's own reference to the op;
		// the handler consumed the retain above instead.
		for _, o := range tail[len(tail)-exit.Adopts:] {
			if o.Value.Kind == types.KindRef {
				i.releaseBox(types.Boxed(ctx.Read(k, o.Value)))
			}
		}
	} else {
		restore(i, holds)
	}
	i.fr, i.sp = savedFr, savedSP
	return ok
}

// restore releases each held operand back down to its saved count.
func restore(i *Interpreter, holds []hold) {
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

// rebuild materializes every native activation as a frame from start,
// replays an ExitCall's callee, and positions the interpreter at the
// innermost one. Activation 0 was entered through ref, and release reports
// whether it owns that reference. A replayed closure callee is counted and
// recorded here, since the threaded closure CALL has no native hook.
func (n *native) rebuild(i *Interpreter, exit jit.Exit, start, ref int, release bool) {
	ctx := n.ctx
	depth := int(ctx.Depth)

	maps := make([]jit.Frame, depth)
	refs := make([]int, depth)
	owns := make([]bool, depth)
	// lent[k] are the parameter slots activation k was entered with but does
	// not own; the outermost activation is never lent one, since threaded
	// code pushed its arguments owned.
	lent := make([][]int, depth)
	maps[depth-1] = exit.Frame
	refs[0], owns[0] = ref, release
	for k := depth - 2; k >= 0; k-- {
		code := n.store.Find(ctx.Records[k+1].PC)
		e := code.Exits[ctx.Records[k].Exit]
		maps[k] = e.Frame
		refs[k+1] = n.callee(k, e)
		owns[k+1] = e.Owned
		lent[k+1] = e.Lent
	}

	for k := 0; k < depth; k++ {
		n.frame(i, start, k, maps[k], refs[k], owns[k])
	}
	for k := 1; k < depth; k++ {
		bp := i.frames[start+k].bp
		for _, p := range lent[k] {
			i.retainBox(i.stack[bp+p])
		}
	}
	inner := &i.frames[start+depth-1]
	i.fp = start + depth
	i.fr = inner

	if exit.Kind == jit.ExitCall {
		for _, p := range exit.Lent {
			i.retainBox(i.stack[i.sp+p])
		}
		callee := i.heap[exit.Callee].(*types.Function)
		i.sp += len(callee.Typ.Params)
		ref := types.BoxRef(n.callee(depth-1, exit))
		if !exit.Owned {
			// A borrowed callee carries no reference of its own; the
			// replayed CALL releases whatever it adopts, so it needs one.
			i.retainBox(ref)
		}
		i.stack[i.sp] = ref
		i.sp++
		// CALL is one byte (interp.go's handler walk relies on the same
		// fact), so its own ip is the map's IP, recorded past it, minus one.
		inner.ip--
		if exit.Closure != nil {
			n.see(i, transform.Callee{Function: exit.Callee, Closure: true})
			if n.store.Code(exit.Callee) == nil {
				n.count(i, exit.Callee, callee)
			}
		}
	}
	ctx.Depth = 0
	ctx.Abandon()
}

// callee is the reference activation k's ExitCall e calls through: the
// closure it maps, or else its function.
func (n *native) callee(k int, e jit.Exit) int {
	if e.Closure == nil {
		return e.Callee
	}
	return types.Boxed(n.ctx.Read(k, *e.Closure)).Ref()
}

// frame materializes activation k from m, entered through ref. release
// reports whether k owns ref: the outermost activation follows the entering
// call site, and every other one follows the call site's own Owned decision
// in the caller's compiled code.
func (n *native) frame(i *Interpreter, start, k int, m jit.Frame, ref int, release bool) {
	ctx := n.ctx
	f := &i.frames[start+k]
	f.addr = m.Address
	f.code = n.exactCode(i, m.Address)
	f.ref = ref
	f.release = release
	f.bp = int((ctx.Records[k].FB - base(i.stack)) / unsafe.Sizeof(types.Boxed(0)))
	f.returns = m.Returns
	f.ip = m.IP
	f.upvals = i.upvals(ref, m.Address)
	f.coro = 0

	sp := f.bp + slots(i.function(m.Address))
	for j, o := range m.Stack {
		boxed := n.box(i, o.Value.Kind, ctx.Read(k, o.Value))
		// A boxed wide i64 is fresh and already owned; only a ref borrows.
		if !o.Owned && o.Value.Kind == types.KindRef {
			i.retainBox(boxed)
		}
		i.stack[sp+j] = boxed
	}
	sp += len(m.Stack)

	for _, l := range m.Locals {
		boxed := n.box(i, l.Value.Kind, ctx.Read(k, l.Value))
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

// box converts a native word to the interpreter's Boxed representation.
// Wide i64 values use the normal heap-promotion path.
func (n *native) box(i *Interpreter, kind types.Kind, word uint64) types.Boxed {
	switch kind {
	case types.KindI1:
		return types.BoxI1(uint32(word) != 0)
	case types.KindI8:
		return types.BoxI8(int8(uint32(word)))
	case types.KindI32:
		return types.BoxI32(int32(uint32(word)))
	case types.KindF32:
		return types.BoxF32(math.Float32frombits(uint32(word)))
	case types.KindF64:
		return types.Boxed(word)
	case types.KindRef:
		return types.Boxed(word)
	case types.KindI64:
		return i.boxI64(int64(word))
	default:
		panic("interp: invalid native value kind " + kind.String())
	}
}

// unbox converts v, a value a handler pushed, to the native word of kind,
// consuming a heap-boxed i64's reference; box is its inverse.
func (n *native) unbox(i *Interpreter, kind types.Kind, v types.Boxed) uint64 {
	switch kind {
	case types.KindI1, types.KindI8, types.KindI32:
		return uint64(uint32(v.I32()))
	case types.KindI64:
		return uint64(i.unboxI64(v))
	case types.KindF32:
		return uint64(math.Float32bits(v.F32()))
	case types.KindF64, types.KindRef:
		return uint64(v)
	default:
		panic("interp: bridge result kind " + kind.String() + " has no native word")
	}
}

func (n *native) metric(i *Interpreter, name string, labels ...prof.Label) {
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

func base(s []types.Boxed) uintptr {
	return uintptr(unsafe.Pointer(unsafe.SliceData(s)))
}

func end(s []types.Boxed) uintptr {
	return base(s) + uintptr(len(s))*unsafe.Sizeof(types.Boxed(0))
}

func rcBase(rc []int) uintptr {
	return uintptr(unsafe.Pointer(unsafe.SliceData(rc)))
}

func entry(entries []int64) uintptr {
	return uintptr(unsafe.Pointer(unsafe.SliceData(entries)))
}

// heapBase is the address of heap's backing array: jit.SizeofValue bytes
// (an interface word pair) per address, read-only to native code except for
// the non-pointer element/field words a guarded exec op writes in place.
func heapBase(heap []types.Value) uintptr {
	return uintptr(unsafe.Pointer(unsafe.SliceData(heap)))
}

// slots is fn's parameter and local count, without Declared's allocation.
func slots(fn *types.Function) int {
	if fn.Typ == nil {
		return len(fn.Locals)
	}
	return len(fn.Typ.Params) + len(fn.Locals)
}
