package interp

import (
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/compile"
	"github.com/siyul-park/minivm/internal/jit/host"
	"github.com/siyul-park/minivm/internal/transform"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

// engine is one interpreter's JIT: it enters native code, serves its exits, and
// materializes its frames, under policy's decisions.
type engine struct {
	policy

	// ctx is built by the first entry: its native stack is the largest
	// allocation a JIT makes, and most programs never enter native code.
	ctx *jit.Context
	// reader is the engine's registration with its shared store.
	reader *jit.Reader
	// quota is the work units a safepoint adds to Context.Budget: the tick
	// of an interpreter with fuel or a hook, else budget.
	quota int64
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
	freeErr error
	// compile is captured once so deoptimization does not add a static
	// dependency from generated threaded handlers back to their compiler.
	// Calling i.compile here directly creates the threaded/fusions
	// initialization cycle.
	compile func(fn *types.Function, exact bool) ([]func(*Interpreter), []uint8)
}

// hold is one heap operand of a bridge attempt and its count before it.
type hold struct {
	ref, count int
}

// key identifies one OSR site by its loop header's (address, ip): how drain
// looks a site up to restore it once its unit's compile permanently fails.
type key struct {
	address, ip int
}

// callout computes an allocating operation's new object from its operands,
// args holding the deepest first, from code, the operation's bytecode. It is
// the interpreter's own helper for the operation, which writes no reference
// count of an operand (native code owns and releases each).
type callout func(i *Interpreter, code []byte, args [2]types.Boxed) types.Value

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

// site is one OSR entry's observation state — a loop header, or loop-free
// module code's ip 0 — owned by the wrapper closure that replaces its
// threaded handler when the JIT is constructed.
type site struct {
	address, ip int
	fn          *types.Function
	// module reports whether fn completes through OpComplete instead of
	// returning through OpReturn.
	module bool
	// inner is s's own threaded handler, restored in place of the observer
	// once s is known to never resolve.
	inner func(*Interpreter)
	// entry reports that s observes module code's ip 0, once per Run.
	entry bool
	// headers lists addr's own loop headers, set only on an entry site:
	// empty for loop-free module code, which has no safepoint, so entry
	// declines a cancelled Run instead; non-empty for module code with
	// loops, which reaches one at each header, so entry need not. Submit
	// also reads it, waiting for every one of addr's header sites to
	// resolve before competing with them for addr's one queue slot.
	headers []int
	// threshold is s's submit count; cadence is how often past it s retries
	// submission, looks up published code, and drains on a cached entry.
	threshold, cadence int64

	count int64
	// total is s's pool-wide entry counter while unsubmitted. It is looked
	// up on first observation: observe runs before Pool.share replaces the
	// native's shared runtime.
	total     *atomic.Int64
	submitted bool
	// code is the published code once a store lookup has found it; nil
	// until then, and again once the site fails.
	code *jit.Code
	// built is the profiles s's published code was compiled from.
	built []jit.Dep
	// refutes counts s's refuted speculations and re-arms: never reset, so
	// they bound s's recompiles.
	refutes int
	// ledger weighs s's native work against its exits' cost, independently
	// of native CALL entries at the same address.
	ledger jit.Ledger
}

// interval is how many back edges pass — once a header site has crossed
// the submit threshold — between its store lookups (and, while unsubmitted,
// its Queue.Submit retries): rare enough that the lock CodeAt and Submit
// take never runs on the per-iteration path. An entry site observes Runs
// and uses 1.
const interval = 256

// nativeStack is the native stack size per interpreter.
const nativeStack = 1 << 20

// budget is the work units between safepoints of an unmetered interpreter.
const budget = 1 << 16

const (
	metricCompiles = "vm_jit_compiles_total"
	metricEntries  = "vm_jit_entries_total"
	metricExits    = "vm_jit_exits_total"

	outcomeOK          = "ok"
	outcomeFailed      = "failed"
	outcomeUnsupported = "unsupported"
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

// hasJIT reports whether the host architecture has a JIT backend.
var _, hasJIT = host.New()

// newEngine builds i's JIT: live at once for a fixed threshold, dormant for the
// automatic one.
func newEngine(i *Interpreter, threshold int) *engine {

	quota := int64(budget)
	if i.gas >= 0 || i.hook != nil {
		quota = int64(i.tick)
	}
	if threshold == 0 {
		return &engine{policy: policy{auto: true, threshold: floor}, quota: quota, compile: i.compile}
	}
	e := &engine{policy: policy{threshold: threshold}, quota: quota, compile: i.compile}
	e.wake(i)
	return e
}

// wake builds the engine's live state, once, and reports whether it is live. Under the
// automatic policy a program whose code is all brief has nothing a Go entry
// could repay: it stays without a JIT.
func (e *engine) wake(i *Interpreter) bool {
	if e.shared != nil {
		return true
	}
	briefs := make([]bool, len(i.code))
	if e.auto {
		lengths := map[int]int{}
		briefs[0] = e.length(i, 0, lengths) < short
		all := briefs[0]
		for _, c := range i.constants {
			if c.Kind() != types.KindRef {
				continue
			}
			if _, ok := i.heap[c.Ref()].(*types.Function); ok {
				briefs[c.Ref()] = e.length(i, c.Ref(), lengths) < short
				all = all && briefs[c.Ref()]
			}
		}
		if all {
			return false
		}
	}
	r := newShared(i)
	e.shared = r
	e.reader = r.store.Attach()
	e.backoffs = make([]uint8, len(i.code))
	e.entries = make([]int64, len(i.code))
	e.refutes = make([]int, len(i.code))
	e.ledgers = make([]jit.Ledger, len(i.code))
	e.failed = make([][2]bool, len(i.code))
	e.built = make([][2][]jit.Dep, len(i.code))
	e.exact = make([][]func(*Interpreter), len(i.code))
	e.borrows = make([][]bool, len(i.code))
	e.sites = map[key]*site{}
	e.profiles = make([]*transform.Profile, len(i.code))
	e.briefs = briefs
	e.gates = slices.Clone(briefs)

	e.observe(i, 0, i.module)
	for addr, obj := range e.module.Objects {
		if obj.Function != nil {
			e.observe(i, addr, obj.Function)
		}
	}
	return true
}

// newShared builds a fresh, unshared JIT runtime for i's program: a Store
// sized to i's code, a single-worker compile Queue, and i's constant module.
func newShared(i *Interpreter) *shared {
	r := &shared{
		store:  jit.NewStore(len(i.code)),
		queue:  compile.NewQueue(func() compile.Machine { m, _ := host.New(); return m }, 1),
		module: newModule(i),
		calls:  make([]atomic.Int64, len(i.code)),
	}
	r.refs.Store(1)
	return r
}

// close releases the engine's reference to its shared runtime.
func (e *engine) close() error {
	if e.shared == nil {
		return nil
	}
	e.reader.Detach()
	return errors.Join(e.freeErr, e.shared.release())
}

// join moves the engine onto r, a Pool's runtime, releasing its own; it has never
// entered native code.
func (e *engine) join(r *shared) error {
	e.reader.Detach()
	own := e.shared
	e.shared, e.reader = r, r.store.Attach()
	return own.release()
}

// call is the CALL handler's hook for a *types.Function target at addr,
// generated into pushFrame's function-target path. It reports whether it ran
// the call to completion, in which case the caller must not push a frame.
// release and advance mirror the call site's own releaseTarget and ip
// advance, since native completion must apply them exactly as pushFrame
// would.
func (e *engine) call(i *Interpreter, addr int, fn *types.Function, release bool, advance int) bool {

	if addr >= len(e.gates) || e.gates[addr] {
		return false
	}
	return e.attempt(i, addr, fn, release, advance)
}

// attempt is call's slow path; call stays small enough to inline into the
// threaded CALL handlers. Only a direct call reaches it, which carries no
// upvals: a function with captures runs natively only through its closure.
func (e *engine) attempt(i *Interpreter, addr int, fn *types.Function, release bool, advance int) bool {
	if release {
		e.see(i, transform.Callee{Function: addr})
	}
	if len(fn.Captures) > 0 || e.depth >= uint64(len(e.ctx.Records)) {
		return false
	}
	e.drain(i)
	code := e.store.Code(addr)
	if code == nil {
		e.count(i, addr, fn)
		return false
	}

	bp := i.sp - len(fn.Typ.Params)
	if release {
		bp--
	}
	for index, k := range code.Arguments {
		if k == types.KindI64 && i.stack[bp+index].Kind() == types.KindRef {
			return false
		}
	}
	retire, fault := e.run(i, addr, fn, code, bp, release, advance)
	if fault != nil {
		panic(fault)
	}
	if retire {
		e.retire(addr, code.Tier)
	}
	return true
}

// run executes one native call whose frame starts at bp and reports whether
// it should retire and the fault settle reports.
func (e *engine) run(i *Interpreter, addr int, fn *types.Function, code *jit.Code, bp int, release bool, advance int) (bool, any) {
	returns := len(fn.Typ.Returns)

	e.load(i, bp, 0)
	ctx := e.ctx
	mark := ctx.Budget

	e.metricEntry(i, code)

	// Threaded code pushed every argument owned, but a return above depth 1
	// releases no borrowed parameter: Go releases each once it returns.
	var buf [4]types.Boxed
	owned := buf[:0]
	if e.depth > 0 {
		if e.borrows[addr] == nil {
			e.borrows[addr] = transform.Borrows(fn)
		}
		for p, b := range e.borrows[addr] {
			if b {
				owned = append(owned, i.stack[bp+p])
			}
		}
	}

	if trap := jit.Enter(code.Entry(), ctx); trap != jit.TrapReturn {
		start := i.fp
		ok, retire, fault := e.settle(i, code, trap, &e.ledgers[addr], mark, start,

			func(exit jit.Exit) { i.fr.ip += advance; e.rebuild(i, exit, start, addr, release) },
			func() bool { return e.refute(addr, code.Tier) },
		)
		if !ok {
			return retire, fault
		}
	} else {
		e.ledgers[addr].Spend(mark - ctx.Budget)
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

// observe wraps every loop header's threaded handler of fn at addr with OSR
// observation, address 0 (module code) included. A header a fusion
// absorbed (code[ip] == nil) is left alone: nothing runs there to observe.
//
// Module code is also observed at ip 0, whether or not it has loops: its
// entry site's submit waits for every header site of the same address to
// resolve first (resolved), so it never takes the queue's one address-0
// slot ahead of them.
func (e *engine) observe(i *Interpreter, addr int, fn *types.Function) {
	headers, err := analysis.Headers(fn)
	if err != nil {
		metric(i, metricCompiles, 1, prof.Label{Key: "tier", Value: jit.Optimized.String()}, prof.Label{Key: "outcome", Value: outcomeFailed})
		return
	}
	code := i.code[addr]

	install := func(ip int, entry bool) {
		if ip < 0 || ip >= len(code) || code[ip] == nil {
			return
		}
		threshold, cadence := int64(e.threshold), int64(interval)
		var own []int
		if entry {

			threshold, cadence = max(threshold, 2), 1
			own = headers
			if len(headers) > 0 {
				cadence = interval
			}
		}
		s := &site{address: addr, ip: ip, fn: fn, module: addr == 0, inner: code[ip], entry: entry, headers: own, threshold: threshold, cadence: cadence}
		e.sites[key{addr, ip}] = s
		code[ip] = e.observer(s, code, s.inner)
	}
	for _, ip := range headers {
		install(ip, false)
	}

	if addr == 0 && !slices.Contains(headers, 0) && !e.briefs[0] {
		install(0, true)
	}
}

// observer wraps inner, the threaded handler at s's own header. Once
// resolved (s.code set), its steady cost is one field read and a call
// either into native code or straight through to inner; a site that enters
// native code never falls through to inner itself, since native code
// always leaves the interpreter at the right next instruction, materialized
// or not.
func (e *engine) observer(s *site, code []func(*Interpreter), inner func(*Interpreter)) func(*Interpreter) {
	return func(i *Interpreter) {
		if s.code != nil && e.enter(i, s, code, inner) {
			return
		}
		due := s.onCadence()
		switch {
		case !s.submitted:
			if s.total == nil {
				s.total = e.total(key{s.address, s.ip})
			}
			total := s.total.Add(1)
			if total >= s.threshold && (total-s.threshold)%s.cadence == 0 && e.resolved(s.address, s.headers) {

				e.drain(i)
				u := compile.Unit{Address: s.address, Function: s.fn, Module: e.module, Profiles: e.snapshot(), Tier: jit.Optimized, IP: s.ip, OSR: true}
				s.submitted = e.queue.Submit(u)
			}
		case due:
			e.drain(i)
			s.code = e.store.CodeAt(s.address, s.ip)
		}
		inner(i)
	}
}

// resolved reports whether every one of addr's header sites at ips has
// published, permanently failed, or disabled: an entry site's submit waits
// for this instead of competing with them for addr's one queue slot. A
// header absent from sites has already failed or disabled (drain and enter
// delete it on either); one still present is resolved once the store holds
// its published code.
func (e *engine) resolved(addr int, ips []int) bool {
	for _, ip := range ips {
		if _, ok := e.sites[key{addr, ip}]; ok && e.store.CodeAt(addr, ip) == nil {
			return false
		}
	}
	return true
}

// enter runs the current frame as s's cached OSR activation. It drains at
// s's lookup cadence, counting entries: a callee called only from native
// code would otherwise tier up only at a safepoint. Loop-free module code
// reaches no safepoint, so an entry site with no headers declines a
// cancelled Run, leaving threaded code to report it; an entry site whose
// module has loops reaches one at each header, so settle already reports
// cancellation there. Native code reads a word per capture without a
// bounds check, so a frame without its captures declines too.
func (e *engine) enter(i *Interpreter, s *site, code []func(*Interpreter), inner func(*Interpreter)) bool {
	if (s.entry && len(s.headers) == 0 && cancelled(i)) || len(i.fr.upvals) < len(s.fn.Captures) || e.depth >= uint64(len(e.ctx.Records)) {
		return false
	}
	if s.onCadence() {
		e.drain(i)
		if e.depth == 0 {
			e.quiesce()
		}
	}
	c := s.code
	if c.Retired() {
		if c = e.store.CodeAt(s.address, s.ip); c == nil {
			s.code = nil
			return false
		}
		s.code = c
	}

	e.load(i, i.fr.bp, 1)
	ctx := e.ctx
	ctx.Upvals = base(i.fr.upvals)
	mark := ctx.Budget

	e.metricEntry(i, c)

	ok, retire := true, false
	var fault any
	if trap := jit.Enter(c.Entry(), ctx); trap != jit.TrapReturn {

		start := i.fp - 1
		f := i.fr
		ok, retire, fault = e.settle(i, c, trap, &s.ledger, mark, start,
			func(exit jit.Exit) { e.rebuild(i, exit, start, f.ref, f.release) },
			func() bool {
				if e.stale(s.built) {
					return true
				}
				s.refutes++
				return s.refutes >= tolerance
			},
		)
	} else {
		s.ledger.Spend(mark - ctx.Budget)
	}
	if ok {
		e.finish(i, s, c)
	}
	if retire {
		e.store.RetireAt(s.address, s.ip)
		s.code = nil
		if s.refutes < tolerance && e.stale(s.built) {

			s.refutes++
			s.submitted = false
			s.ledger = jit.Ledger{}
			if e.auto {
				s.threshold = min(s.threshold<<2, ceiling)
			}
		} else {
			code[s.ip] = inner
			delete(e.sites, key{s.address, s.ip})
		}
	}
	if fault != nil {
		panic(fault)
	}
	return true
}

// rebuild materializes every native activation of the current run, from
// depth upward, as frames from start and positions the interpreter at the
// innermost one. The run's first activation was entered through ref, and
// release reports whether it owns that reference.
func (e *engine) rebuild(i *Interpreter, exit jit.Exit, start, ref int, release bool) {
	ctx := e.ctx
	floor, depth := int(e.depth), int(ctx.Depth)

	maps := make([]jit.Frame, depth-floor)
	refs := make([]int, depth-floor)
	owns := make([]bool, depth-floor)

	lent := make([][]int, depth-floor)
	maps[depth-floor-1] = exit.Frame
	refs[0], owns[0] = ref, release
	for k := depth - 2; k >= floor; k-- {
		code := e.store.Find(ctx.Records[k+1].PC)
		call := code.Exits[ctx.Records[k].Exit]
		maps[k-floor] = call.Frame
		refs[k-floor+1] = e.callee(k, call)
		owns[k-floor+1] = call.Owned
		lent[k-floor+1] = call.Lent
	}

	// Materialization must not fail: threaded code would already have paid for
	// these values, so their boxes may pass the heap limit. The next allocation
	// that needs a new entry trips it.
	limit := i.limit
	i.limit = 0
	defer func() { i.limit = limit }()
	for j := range maps {
		e.frame(i, start+j, floor+j, maps[j], refs[j], owns[j])
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

// frame materializes activation k from m as frame at, entered through ref.
// release reports whether k owns ref: the run's first activation follows the
// entering call site, and every other one follows the call site's own Owned
// decision in the caller's compiled code.
func (e *engine) frame(i *Interpreter, at, k int, m jit.Frame, ref int, release bool) {
	ctx := e.ctx
	if m.Address != entered(i, ref) {
		// A tail call inside native code moved to another function of its
		// translation: threaded released ref there and the frame runs addr through
		// no reference of its own.
		if release {
			i.release(ref)
		}
		ref, release = m.Address, false
	}
	f := &i.frames[at]
	f.addr = m.Address
	f.code = e.exactCode(i, m.Address)
	f.ref = ref
	f.release = release
	f.bp = e.frameBase(i, k)
	f.returns = m.Returns
	f.ip = m.IP
	f.upvals = i.upvals(ref, m.Address)
	f.coro = 0

	sp := f.bp + slots(i.function(m.Address))
	for j, o := range m.Stack {
		boxed := fromWord(i, o.Value.Kind, ctx.Read(k, o.Value))

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

// entered is the function a call through ref runs: ref itself, or the
// function of the closure it names.
func entered(i *Interpreter, ref int) int {
	if ref > 0 && ref < len(i.heap) {
		if cl, ok := i.heap[ref].(*types.Closure); ok {
			return int(cl.Fn)
		}
	}
	return ref
}

// quiesce marks a quiescent point, where the engine runs no native activation and
// holds no code it read before, then frees what every reader has passed.
func (e *engine) quiesce() {
	if e.shared == nil {
		return
	}
	e.reader.Quiesce()
	e.freeErr = errors.Join(e.freeErr, e.store.Reclaim())
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
func (e *engine) settle(i *Interpreter, code *jit.Code, trap jit.Trap, ledger *jit.Ledger, mark int64, start int, deopt func(jit.Exit), refute func() bool) (ok, retire bool, fault any) {
	ctx := e.ctx
	for {
		ledger.Spend(mark - ctx.Budget)
		mark = ctx.Budget
		if trap == jit.TrapReturn {
			return true, false, nil
		}

		entered := code
		if ctx.Depth != e.depth+1 {
			entered = e.store.Find(ctx.PC())
		}
		exit := entered.Exits[ctx.Exit()]
		metric(i, metricExits, 1, prof.Label{Key: "kind", Value: exit.Kind.String()})
		switch exit.Kind {
		case jit.ExitSafepoint:
			if cancelled(i) {

				deopt(exit)
				return false, false, nil
			}
			ctx.Budget += e.quota
			if i.metered() {
				if i.hook != nil || !i.burn() {

					deopt(exit)
					e.rethread(i, start)
					i.due, i.parked, i.fr.ip = true, i.fr.ip, park
					return false, false, nil
				}
				i.sample(exit.Frame.Address, exit.Frame.IP)
			}
			e.sync(i)
			mark = ctx.Budget
			e.drain(i)
			trap = jit.Resume(ctx)
		case jit.ExitRelease:

			ledger.Charge(jit.ClassRelease)
			if ref := types.Boxed(ctx.Read(int(ctx.Depth)-1, exit.Word)).Ref(); ref != 0 {
				i.release(ref)
			}
			e.sync(i)
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
				if !e.callout(i, exit, serve) {
					class = jit.ClassTrap
				}
			case exit.Kind == jit.ExitBridge:
				class = e.bridge(i, exit)
			case !e.widen(i, exit):
				class = jit.ClassTrap
			}
			switch class {
			case jit.ClassBridge, jit.ClassCallout:
				e.sync(i)
				trap = jit.Resume(ctx)
				continue
			case jit.ClassTrap:
				deopt(exit)
				return false, false, nil
			}
			deopt(exit)
			return false, e.judge(entered, code, refute), nil
		case jit.ExitCall:
			ref := e.callee(int(ctx.Depth)-1, exit)
			if i.hook != nil {

				deopt(exit)
				e.replay(i, exit, ref)
				e.rethread(i, start)
				return false, false, nil
			}
			addr, ok := target(i, exit, ref)
			if !ok || !e.nests(i, exit, addr) {
				deopt(exit)
				e.replay(i, exit, ref)
				return false, e.judge(entered, code, refute), nil
			}
			if !e.pending(addr) && !ledger.Charge(jit.ClassCall) {
				deopt(exit)
				e.replay(i, exit, ref)
				return false, true, nil
			}
			if fault, ok := e.nest(i, exit, ref, start, deopt); !ok {
				return false, false, fault
			}
			e.sync(i)
			mark = ctx.Budget
			trap = jit.Resume(ctx)
		default:
			deopt(exit)
			if exit.Trap {
				return false, false, nil
			}
			e.record(exit)
			return false, e.judge(entered, code, refute), nil
		}
	}
}

// rethread points every frame materialized from start at its observed
// threaded code where its ip has a handler, so its loop headers enter native
// code again.
func (e *engine) rethread(i *Interpreter, start int) {
	for at := start; at < i.fp; at++ {
		f := &i.frames[at]
		if code := i.code[f.addr]; f.ip < len(code) && code[f.ip] != nil {
			f.code = code
		}
	}
}

// load points the native context at i for an entry whose frame starts at stack
// index bp. spare is how many more nested activations the entry may start: an
// OSR entry replaces the running frame instead of pushing one.
func (e *engine) load(i *Interpreter, bp, spare int) {
	if e.ctx == nil {
		ctx, err := jit.NewContext(nativeStack)
		if err != nil {
			panic(err)
		}
		ctx.Budget = e.quota
		e.ctx = ctx
	}
	ctx := e.ctx
	e.sync(i)
	ctx.Globals = base(i.globals)
	ctx.Natives = e.store.Natives()
	ctx.Entries = base(e.entries)
	ctx.Top = end(i.stack)
	ctx.FB = base(i.stack[bp:])
	ctx.Limit = uint64(min(len(ctx.Records), int(e.depth)+len(i.frames)-i.fp+spare))
	ctx.Depth = e.depth
}

// sync points the native context at i's heap and counts, which a bridge or a
// call may have moved.
func (e *engine) sync(i *Interpreter) {
	e.ctx.Heap = base(i.heap)
	e.ctx.RC = base(i.rc)
}

// widen heap-boxes exit.Word, a wide i64, into Context.Results[0] as
// threaded boxI64 does; a panic (heap exhaustion) declines.
func (e *engine) widen(i *Interpreter, exit jit.Exit) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	k := int(e.ctx.Depth) - 1
	v := int64(e.ctx.Read(k, exit.Word))
	e.ctx.Results[0] = uint64(i.boxI64(v))
	return true
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
func (e *engine) bridge(i *Interpreter, exit jit.Exit) jit.Class {
	if !bridgeable[exit.Code] {

		if exit.Code.Writes(instr.Branch) {
			return jit.ClassTrap
		}
		return jit.ClassGuard
	}
	if i.fp >= len(i.frames) {
		return jit.ClassGuard
	}

	ctx := e.ctx
	k := int(ctx.Depth) - 1
	m := exit.Frame
	bp := e.frameBase(i, k)
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
	*f = frame{addr: m.Address, code: e.exactCode(i, m.Address), bp: bp, ip: m.IP}
	i.fr = f
	i.sp = sp + len(tail)

	ok := exec(i, f)
	if ok {
		for j, kind := range exit.Results {
			ctx.Results[j] = toWord(i, kind, i.stack[i.sp-len(exit.Results)+j])
		}

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
func (e *engine) callout(i *Interpreter, exit jit.Exit, serve callout) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	ctx := e.ctx
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

// nests reports whether exit's call to the function at addr can run while its
// caller stays suspended: not a coroutine, whose CALL returns a handle instead
// of the results native code expects, and with room to push the callee.
func (e *engine) nests(i *Interpreter, exit jit.Exit, addr int) bool {
	if addr < len(i.coros) && i.coros[addr] {
		return false
	}
	k := int(e.ctx.Depth) - 1
	bp := e.frameBase(i, k)
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
func (e *engine) nest(i *Interpreter, exit jit.Exit, ref, start int, deopt func(jit.Exit)) (fault any, ok bool) {
	ctx := e.ctx
	k := int(ctx.Depth) - 1
	m := exit.Frame
	at := start + k - int(e.depth)
	bp := e.frameBase(i, k)

	state, limit, depth, floor := ctx.State, ctx.Limit, e.depth, i.floor
	fr, fp, sp, saved := i.fr, i.fp, i.sp, i.frames[at]

	code := e.exactCode(i, m.Address)
	i.frames[at] = frame{addr: m.Address, code: code[:m.IP], bp: bp, ip: m.IP, returns: m.Returns}
	i.fr, i.fp = &i.frames[at], at+1
	args := bp + slots(i.function(m.Address)) + len(m.Stack)
	i.sp = args
	e.replay(i, exit, ref)
	for _, p := range exit.Kept {
		i.retainBox(i.stack[args+p])
	}
	e.depth, i.floor = uint64(k+1), at+1
	fault, err := dispatch(i)
	e.depth, i.floor = depth, floor

	if err == nil && fault == nil {
		for j, kind := range exit.Results {
			ctx.Results[j] = toWord(i, kind, i.stack[i.sp-len(exit.Results)+j])
		}
		i.fr, i.fp, i.sp, i.frames[at] = fr, fp, sp, saved
		ctx.State, ctx.Limit, ctx.Depth = state, limit, uint64(k+1)
		return nil, true
	}

	for _, p := range exit.Kept {
		i.releaseBox(i.stack[args+p])
	}

	ip, live := i.frames[at].ip, [...]int{i.fp, i.sp}
	inner := i.fr
	ctx.State, ctx.Depth = state, uint64(k+1)
	i.fr, i.fp, i.sp, i.frames[at] = fr, fp, sp, saved
	deopt(exit)
	i.frames[at].ip = ip
	i.fr, i.fp, i.sp = inner, live[0], live[1]
	if _, ok := fault.(escape); ok {

		i.sp++
		fault = nil
	}
	return fault, false
}

// exactCode returns addr's threaded code compiled exact, building and caching
// it the first time a deoptimization at addr needs it.
func (e *engine) exactCode(i *Interpreter, addr int) []func(*Interpreter) {
	if code := e.exact[addr]; code != nil {
		return code
	}
	code, _ := e.compile(i.function(addr), true)
	e.exact[addr] = code
	return code
}

// frameBase is the stack index where native activation k's frame starts.
func (e *engine) frameBase(i *Interpreter, k int) int {
	return int((e.ctx.Records[k].FB - base(i.stack)) / unsafe.Sizeof(types.Boxed(0)))
}

// replay pushes exit's callee over its arguments at i.fr's CALL, which the
// interpreter then runs: it retains each lent argument and a borrowed ref,
// since the CALL adopts both. A closure callee is counted and recorded here,
// since the threaded closure CALL has no native hook; so is brief code, whose
// hook is gated.
func (e *engine) replay(i *Interpreter, exit jit.Exit, ref int) {
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

	i.fr.ip--
	if ref > 0 && ref < len(i.heap) {
		switch callee := i.heap[ref].(type) {
		case *types.Closure:
			addr := int(callee.Fn)
			e.see(i, transform.Callee{Function: addr, Closure: true})
			if e.store.Code(addr) == nil {
				e.count(i, addr, i.function(addr))
			}
		case *types.Function:
			if ref < len(e.briefs) && e.briefs[ref] && e.store.Code(ref) == nil {
				e.count(i, ref, callee)
			}
		}
	}
}

// callee is the reference activation k's ExitCall call goes through: the
// closure it maps, or else its function.
func (e *engine) callee(k int, call jit.Exit) int {
	if call.Target == nil {
		return call.Callee
	}
	return types.Boxed(e.ctx.Read(k, *call.Target)).Ref()
}

func (e *engine) metricEntry(i *Interpreter, c *jit.Code) {
	metric(i, metricEntries, 1, prof.Label{Key: "tier", Value: c.Tier.String()})
}

// finish closes an OSR activation after TrapReturn. Ordinary RETURN results are
// already at the frame base; module completion leaves results past locals and
// advances IP past code so threaded dispatch completes.
func (e *engine) finish(i *Interpreter, s *site, c *jit.Code) {
	f := i.fr
	if s.module {
		f.ip = len(s.fn.Code)
		i.sp = f.bp + slots(s.fn) + c.Results
		return
	}
	boxRegisters(i, c, f.bp)
	i.leave(f, f.bp+len(s.fn.Typ.Returns))
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

func (s *site) onCadence() bool {
	s.count++
	return s.count%s.cadence == 0
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

// end is the address one past the last word of the operand stack s.
func end(s []types.Boxed) uintptr {
	return base(s) + uintptr(len(s))*unsafe.Sizeof(types.Boxed(0))
}

// slots is fn's parameter and local count, without Declared's allocation.
func slots(fn *types.Function) int {
	if fn.Typ == nil {
		return len(fn.Locals)
	}
	return len(fn.Typ.Params) + len(fn.Locals)
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

// jitEnabled reports whether opt selects the JIT: WithThreshold(n) with n >=
// 0, on a host with a JIT backend, with a tick above 1: a tick of 1 is exact
// execution, every instruction boundary observable, which native code does
// not expose.
func jitEnabled(opt option) bool {
	return opt.threshold >= 0 && hasJIT && opt.tick > 1
}

// index is the two-byte type index the operation at code[0] carries, read
// as the threader reads it.
func index(code []byte) int {
	return int(*(*uint16)(unsafe.Pointer(&code[1])))
}

// base is the address of s's backing array. The heap's is jit.SizeofValue
// bytes (an interface word pair) per address, read-only to native code except
// for the non-pointer element and field words a guarded exec op writes in place.
func base[T any](s []T) uintptr {
	return uintptr(unsafe.Pointer(unsafe.SliceData(s)))
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

func metric(i *Interpreter, name string, value float64, labels ...prof.Label) {
	if i.profiler == nil {
		return
	}
	i.samples.AddMetric(name, value, labels...)
}
