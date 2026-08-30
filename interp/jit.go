package interp

import (
	"math"
	"slices"
	"sync/atomic"
	"unsafe"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/journal"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

// This file holds the interpreter's JIT integration: tier-up event counting,
// compilation requests, native installation, the native dispatch wrappers
// threaded code hands control to, deoptimization, and journal marshalling.
// interp/tier.go owns tier-up and retirement policy - counters, the watchdog,
// and their thresholds; this file is what drives an Interpreter against that
// policy and the architecture-neutral internal/jit driver.
//
// entryWarmup is how many hot events capture an entry trace before the entry
// compile fires. The compile is one-shot per root, so a trace has to already be
// published when it runs; capturing from the first few events records the
// shallowest states the function ever has. tracer.attemptLimit bounds the cost
// independently.
const entryWarmup = 8

// coolLimit is the number of consecutive unproductive observations - every
// compilation root already tried and nothing installed - before a function
// is cooled and stops being instrumented. See checkCool.
const coolLimit = 2

// compile lowers traces already recorded for root and installs the resulting
// native entries. Recording belongs to the hot-event hooks and side-exit
// handling because only those paths hold the exact runtime state for their
// anchor.
func (i *Interpreter) compile(root jit.Anchor) error {
	i.samples.AddMetric("vm_jit_attempts_total", 1)
	if i.compiler == nil {
		compiler, err := newCompiler()
		if err != nil {
			i.samples.AddMetric("vm_jit_errors_total", 1)
			i.recordCompile(prof.TriggerHot, jit.Result{Anchor: root, Outcome: prof.CompileOutcomeError, Reason: prof.CompileReasonError, Err: err})
			return err
		}
		i.compiler = compiler
	}
	if i.compiler == nil {
		i.recordCompile(prof.TriggerHot, jit.Result{Anchor: root, Outcome: prof.CompileOutcomeRejected, Reason: prof.CompileReasonBackendUnavailable})
		return nil
	}
	result := i.attempt(i.compiler, root, prof.TriggerHot)
	if result.Err != nil {
		return result.Err
	}
	if result.Code == nil {
		return nil
	}
	i.install(result.Code, true)
	return nil
}

// compileSnapshot builds the compile-time-stable view of addr's function that
// the architecture-neutral JIT driver plans and lowers against: building it
// is the interpreter's job, because it is the only side that holds the
// private state (function bodies, constants, globals, heap, declared types,
// recorded traces) a snapshot copies out of. It reports false when addr names
// no compilable function.
func (i *Interpreter) compileSnapshot(addr int) (*jit.Input, bool) {
	fn, ok := i.function(addr)
	if !ok || fn == nil || len(fn.Code) == 0 {
		return nil, false
	}
	input := &jit.Input{
		Address:   addr,
		Function:  fn,
		Module:    i.module,
		Constants: i.constants,
		Globals:   i.globalKinds(),
		Heap:      i.heap,
		Decl:      i.types,
		// Layout is computed here, in architecture-neutral code where
		// HostStruct, field, conversion, and coroutine are visible, so an
		// architecture backend consuming jit.Input never has to import
		// them (see jit.Layout). HostStructItab and CoroutineItab are the
		// same treatment applied to the two heap type identities a backend
		// guards against: jit.Itab only needs the types.Value interface, so
		// interp computes them here rather than exporting the concrete
		// types.
		Layout: jit.Layout{
			HostFields:      int(unsafe.Offsetof(HostStruct{}.fields)),
			HostPtr:         int(unsafe.Offsetof(HostStruct{}.ptr)),
			HostFieldOffset: int(unsafe.Offsetof(field{}.offset)),
			HostFieldConv:   int(unsafe.Offsetof(field{}.conversion)),
			HostFieldSize:   int(unsafe.Sizeof(field{})),
			HostConvKind:    int(unsafe.Offsetof(conversion{}.kind)),
			CoroutineValue:  int(unsafe.Offsetof(coroutine{}.value)),
			CoroutineDone:   int(unsafe.Offsetof(coroutine{}.done)),
			HostStructItab:  jit.Itab((*HostStruct)(nil)),
			CoroutineItab:   jit.Itab((*coroutine)(nil)),
		},
		Installed: i.stub(addr) != nil,
	}
	// i.tracer is nil only for a speculative clone (see tracer.clone), which
	// never compiles; widening a nil *tracer into the RecordedTraces
	// interface would produce a non-nil interface holding a nil pointer, so
	// jit.TracePlan's own nil check would stop seeing "no recorder" the way
	// it did before this value lived behind an interface.
	if i.tracer != nil {
		input.Traces = i.tracer
	}
	return input, true
}

// globalKinds returns the logical kinds of current global values for JIT
// specialization. Heap-backed i64 values recover KindI64 from the heap object.
// Dynamic scalar globals remain unknown so native lowering does not assume a
// stable representation.
func (i *Interpreter) globalKinds() []types.Kind {
	kinds := make([]types.Kind, len(i.globals))
	for idx, val := range i.globals {
		kind := val.Kind()
		if kind == types.KindRef && i.alive(val.Ref()) {
			kind = i.heap[val.Ref()].Kind()
		}
		if i.globalTypes[idx] == types.TypeAny && kind != types.KindRef {
			kind = instr.KindAny
		}
		kinds[idx] = kind
	}
	return kinds
}

// attempt runs one Compile, records the outcome under trigger, and counts any
// compile error. Acquisition and delivery of the result stay with the caller.
func (i *Interpreter) attempt(c *jit.Compiler, root jit.Anchor, trigger prof.Trigger) jit.Result {
	input, ok := i.compileSnapshot(root.Addr)
	result := jit.Result{Anchor: root, Outcome: prof.CompileOutcomeEmpty, Reason: prof.CompileReasonNoInput}
	if ok {
		result = c.Compile(input, root)
	}
	i.recordCompile(trigger, result)
	if result.Err != nil {
		i.samples.AddMetric("vm_jit_errors_total", 1)
	}
	return result
}

// install accounts a successful Compile and rewires the dispatch table: a
// trace entry replaces the function's first opcode handler and keeps the
// shadowed threaded handler for guard fallback.
func (i *Interpreter) install(mod *jit.Code, account bool) {
	if account {
		i.account(mod)
	}
	for a, entry := range mod.Entries {
		if a.Addr < 0 || a.Addr >= len(i.code) || a.IP < 0 || a.IP >= len(i.code[a.Addr]) || entry.Callable == nil {
			continue
		}
		// Two loop roots of one function can both compile, and an installed
		// root keeps execution inside itself, so whichever one holds the
		// dispatch slot silently starves the other. When one covers the other
		// they are nested, and the inner root is the specialized one: a
		// recorded trace folds its legs and hoists its container, while the
		// enclosing static plan - pruned by plain forward reachability, so it
		// swallows the inner header whole (see jit.Plan.prune) - does neither.
		// The static loop plan is the fallback for a loop no trace could
		// record (see internal/jit/compiler.go's frontend order), so it must
		// never take the slot from a recorded one it contains.
		//
		// Sibling loops cover neither the other and are left alone, which is
		// the distinction this rule turns on: coexistence is normal, and only
		// containment starves.
		if i.covered(a, entry) {
			continue
		}
		if entry.Kind == jit.EntryLoop {
			i.uncover(a)
		}
		// A peer's publish can land on a function this interpreter already
		// cooled (see cool); installing native code for it makes further
		// instrumentation useful again, so resume it.
		if a.Addr < len(i.cold) && i.cold[a.Addr] {
			i.cold[a.Addr] = false
			i.misses[a.Addr] = 0
		}
		// Save the original threaded handler once so deopt always resumes in the
		// interpreter, but reinstall the latest native callable on every publish:
		// a recompiled trace tree (with a hot side exit now inlined) must replace
		// the earlier one, not be dropped because one was already installed. An
		// entry root (ip 0) compiles the whole function and tears down the frame
		// on return; a loop root re-enters mid-function and never unwinds it.
		if i.exits[a] == nil {
			i.exits[a] = i.code[a.Addr][a.IP]
		}
		// natives keeps its New-time size: growing it in bind could dangle the
		// journal base cached by a native frame suspended across a trap
		// fallback, so dynamically bound functions never get a natives slot.
		if entry.Kind == jit.EntryFunction && a.Addr < len(i.natives) {
			atomic.StorePointer(&i.natives[a.Addr], entry.Callable.Addr())
		}
		// A module loop root owns the hot body far better than the whole-module
		// entry plan that also contains it, and an installed entry answers
		// i.code[0][0] and keeps execution inside itself, so the loop stub
		// would never be dispatched. Retire the entry in favour of the loop.
		// Only module code: its entry runs once per execution, while a
		// function entry carries the call path and must stay.
		if entry.Kind == jit.EntryLoop && a.Addr == 0 {
			if shadowed := i.exits[jit.Anchor{Addr: 0}]; shadowed != nil {
				i.code[0][0] = shadowed
			}
		}
		i.live[a] = entry
		stats := i.counters(a, entry)
		// wd tracks give-up exits independent of stats: counters is a no-op
		// under WithProfiler off (see i.counters), but a net-loss native entry
		// must still be caught and retired without profiling enabled.
		wd := newWatchdog(entry)
		i.code[a.Addr][a.IP] = i.cycle(a, entry, stats, wd)
	}
}

// covered reports whether the static plan about to install at a would swallow
// a loop root of the same function that is already dispatching. Only a static
// plan can lose here: the trace frontend anchors a nested loop as an edge to
// that root's own entry instead of inlining it, so a recorded plan never takes
// an inner root's work away in the first place.
func (i *Interpreter) covered(a jit.Anchor, entry jit.Entry) bool {
	if entry.Frontend != prof.FrontendStatic {
		return false
	}
	for root, live := range i.live {
		if root.Addr != a.Addr || root == a || live.Kind != jit.EntryLoop {
			continue
		}
		if i.swallows(a, entry, root.IP) {
			return true
		}
	}
	return false
}

// swallows reports whether the plan installing at a runs the loop headed at
// header as part of itself. A function entry plans the whole function, so it
// swallows every loop in it; a loop root swallows only the loops nested in
// its own body (see tracer.encloses), never a sibling that merely follows it.
//
// A module entry is excluded. It runs once per execution rather than once per
// call, so the whole-module plan is the program, and measurement says it wins:
// withdrawing it costs Control_Sieve about 10%. Its arbitration against a
// module loop root stays the one install already had.
func (i *Interpreter) swallows(a jit.Anchor, entry jit.Entry, header int) bool {
	switch entry.Kind {
	case jit.EntryFunction:
		return true
	case jit.EntryLoop:
		return i.tracer.encloses(i, a.Addr, a.IP, header)
	default:
		return false
	}
}

// uncover withdraws an installed static loop root that covers a, so the inner
// root about to install at a is reachable at all: the outer one holds the
// dispatch slot execution passes through first and would otherwise keep
// running the whole nest itself (see install and covered, the same rule in
// the other install order).
//
// It restores the shadowed threaded handler rather than cooling the function,
// unlike retire: nothing here says the outer root fails to pay for itself,
// only that a better root now owns the work, so the address must stay
// instrumented and its watchdog must stay free to retire the inner root later.
func (i *Interpreter) uncover(a jit.Anchor) {
	for root, live := range i.live {
		if root.Addr != a.Addr || root == a || live.Frontend != prof.FrontendStatic {
			continue
		}
		if !i.swallows(root, live, a.IP) {
			continue
		}
		if fn := i.exits[root]; fn != nil {
			i.code[root.Addr][root.IP] = fn
		}
		if live.Kind == jit.EntryFunction && root.Addr < len(i.natives) {
			atomic.StorePointer(&i.natives[root.Addr], nil)
		}
		delete(i.live, root)
	}
}

func (i *Interpreter) sync() {
	if i.cache == nil {
		return
	}
	modules := i.cache.modules.Load()
	if modules == nil {
		return
	}
	for i.gen < len(*modules) {
		i.install((*modules)[i.gen], false)
		i.gen++
	}
}

// entered records one call into the current function. Threaded handlers report
// failures by panicking so dispatch can annotate them at the interpreter boundary.
func (i *Interpreter) entered() {
	if err := i.hit(); err != nil {
		panic(err)
	}
}

// hit records one hot event.
func (i *Interpreter) hit() error {
	addr := i.fr.addr
	if i.trigger == 0 || addr < 0 || addr >= len(i.entries) {
		return nil
	}
	hits := i.entries[addr]
	if hits < math.MaxUint64 {
		hits++
		i.entries[addr] = hits
	}
	if hits <= entryWarmup || hits >= i.trigger {
		if err := i.warm(addr, hits); err != nil {
			return err
		}
	}
	if i.cache != nil {
		request, ok := i.cache.claim(addr, i.threshold)
		if ok {
			return i.shared(request.root, request.trigger)
		}
	}
	return nil
}

// warm handles entry tracing and compilation once an event reaches the warmup
// window or threshold. Entry capture records the shallowest runtime state.
//
// Only an event raised at the entry itself may capture. A back edge is a hot
// event for the same function but stands mid-body, and recording the entry root
// from there replays the entry instructions against loop-carried locals - a
// state the function never reaches on entry, whose trace then plans nothing.
func (i *Interpreter) warm(addr int, hits uint64) error {
	if i.isCold(addr) {
		return nil
	}
	root := jit.Anchor{Addr: addr}
	if hits <= entryWarmup && i.fr.ip == 0 {
		i.tracer.capture(i, root)
	}
	if hits >= i.trigger && i.cache == nil && !i.tried[root] && i.settled(addr, hits) {
		i.tried[root] = true
		if err := i.compile(root); err != nil {
			return err
		}
	}
	i.checkCool(addr, root)
	return nil
}

// settled reports whether addr's entry root is ready to be compiled. A loop
// header is the better root of the two - it carries the hoisting and the native
// back-edge that the enclosing entry's copy of the same blocks does not - and an
// installed entry runs its loops inside itself, so their back edges stop
// reporting and they can never earn one. Headers therefore go first.
//
// The wait is bounded, because a header the function never actually reaches
// would otherwise hold its entry back forever.
func (i *Interpreter) settled(addr int, hits uint64) bool {
	if hits >= i.trigger+entryWarmup {
		return true
	}
	for _, l := range i.tracer.headers(i, addr) {
		// A header at ip 0 is this very root, not a separate one to wait for.
		if l.header != 0 && !i.tried[jit.Anchor{Addr: addr, IP: l.header}] {
			return false
		}
	}
	return true
}

// checkCool counts one unproductive observation when addr's entry root and
// every loop header returned by i.tracer.headers have already been attempted
// (recorded in i.tried). coolLimit consecutive such observations cool addr,
// which permanently stops instrumenting it. A root still waiting to be
// attempted resets nothing and costs one map lookup.
//
// Whether anything installed is deliberately not consulted. Once every root
// has been attempted there is nothing further to compile, so continuing to
// sample, capture, and observe back-edges only costs dispatch overhead - a
// function that installed native code pays that overhead in the tier that
// wins, and its installed entries keep running. checkRetire, not this, is what
// reacts to native code that turned out not to pay for itself.
func (i *Interpreter) checkCool(addr int, root jit.Anchor) {
	if !i.tried[root] {
		return
	}
	for _, l := range i.tracer.headers(i, addr) {
		if !i.tried[jit.Anchor{Addr: addr, IP: l.header}] {
			return
		}
	}
	if addr < 0 || addr >= len(i.misses) {
		return
	}
	if i.misses[addr] < math.MaxUint8 {
		i.misses[addr]++
	}
	if i.misses[addr] >= coolLimit {
		i.cool(addr)
	}
}

// backedge receives the exact target of a warmed backward branch, which is the
// loop header itself - no header scan is needed here. The arrival is also a hot
// event for the enclosing function, so a loop makes its own function's entry
// eligible without any instruction sampling. It drives cooling too: once every
// root of this function has been attempted, repeated arrivals are what
// eventually retire its instrumentation.
func (i *Interpreter) backedge(f *frame) error {
	if f.ip <= 0 {
		return nil
	}
	if i.isCold(f.addr) {
		return nil
	}
	if err := i.hit(); err != nil {
		return err
	}
	// The loop root waits for the same hot count as the entry root, so the
	// whole-function plan is always attempted first. Compiling the header first
	// would install a native loop that stops running this hook, and the function
	// it belongs to would never accumulate the events its own entry needs.
	if f.addr >= 0 && f.addr < len(i.entries) && i.entries[f.addr] < i.trigger {
		return nil
	}
	err := i.trace(f)
	i.checkCool(f.addr, jit.Anchor{Addr: f.addr})
	return err
}

// yielded reports a native loop yield through the same back-edge path as the threaded loop.
func (i *Interpreter) yielded() error {
	if err := i.backedge(i.fr); err != nil {
		return err
	}
	return i.safepoint()
}

func (i *Interpreter) trace(f *frame) error {
	root := jit.Anchor{Addr: f.addr, IP: f.ip}
	if i.exits[root] != nil || i.tried[root] {
		return nil
	}
	i.tried[root] = true
	result := i.tracer.capture(i, root)
	if result.trace == nil {
		return nil
	}
	if i.cache != nil {
		i.cache.request(request{root: root, trigger: prof.TriggerHot})
		return nil
	}
	if err := i.compile(root); err != nil {
		return err
	}
	// A loop header reached on the iteration that exits records the path out of
	// the loop rather than the loop, and the walk ends cut at some later header -
	// a partial. That plans nothing, and keeping it would serve the same
	// recording to every later arrival, so forget it and let the next arrival
	// record again; tracer.attemptLimit bounds the retries. A recording that
	// completed and still planned nothing is a real answer about this header, not
	// bad luck, and retrying only spends clones to reach the same plan.
	if root.IP != 0 && i.exits[root] == nil && result.outcome == prof.CaptureOutcomePartial {
		i.tracer.forget(root)
		delete(i.tried, root)
	}
	return nil
}

// cycle builds the native dispatch closure threaded code hands control to at
// anchor root: it enters counters and the watchdog, seeds the journal's
// resume IP and back-edge budget, calls the native Callable, then dispatches
// on the trap it reports. All three installed roles - function entry, module
// entry, and loop header - share this scaffold; entry.Kind and root.Addr
// alone say what each one does differently:
//
//   - EntryFunction: the CALL handler has already pushed a frame and set i.fr
//     before this closure runs, so the fresh frame's code and upvals are
//     cleared before the call - refreshing the back-edge budget the same way
//     a loop does, since an entry trace can carry a self tail-call back-edge
//     (see tailLoop) that polls the safepoint every loopBudget iterations,
//     re-entering native here after each yield. The native Entry reads params
//     from stack scratch slots. sp is restored from the journal only on a
//     trap, because a clean return runs popFrame, which recomputes it from
//     the frame itself and performs the teardown RETURN would do in the
//     threaded interpreter; on a trap this closure rebuilds the native call
//     chain into real VM frames before resuming threaded execution at the
//     fallback IP, or gives up and bails out.
//   - EntryModule: top-level code. The frame is fresh the same way, but a
//     clean return does not tear it down - it preserves the operand stack and
//     marks the module frame exhausted so dispatch returns normally, so sp
//     must come from the journal unconditionally. A give-up bails out the
//     same way.
//   - EntryLoop: the header is reached mid-function with the frame already
//     live, so it is never reinitialized, and sp is restored unconditionally
//     like EntryModule. A clean return completes the module when the loop
//     owns the whole module (root.Addr == 0) and tears the frame down
//     otherwise. A spent budget yields to the safepoint and the Run loop
//     re-enters native at the header. A give-up records the exit and, only if
//     execution resumed at the header itself, runs the shadowed handler once
//     so the interpreter makes progress instead of re-dispatching the same
//     native stub (see resumeShadowed) - a loop root has no function-entry
//     stub to bail out to instead.
//
// checkRetire's clearNatives is set only for EntryFunction: retiring a
// function entry must also clear its fast-call slot in i.natives (see
// install and retire), which a module or loop root never has.
func (i *Interpreter) cycle(root jit.Anchor, entry jit.Entry, stats counters, wd *watchdog) func(*Interpreter) {
	resetFrame := entry.Kind != jit.EntryLoop
	earlySP := entry.Kind != jit.EntryFunction
	isFunction := entry.Kind == jit.EntryFunction
	// popOnReturn and shadow fold the per-role decisions the dispatch loop
	// would otherwise re-derive on every native entry: whether a clean return
	// tears the frame down, and which installation point a give-up may have
	// resumed at. Only the loop role's shadow anchor is the root itself; the
	// other two ask about the function they returned into, whose address is
	// not known until the trap.
	popOnReturn := isFunction || (entry.Kind == jit.EntryLoop && root.Addr != 0)
	loopShadow := entry.Kind == jit.EntryLoop
	return func(i *Interpreter) {
		resume := uint64(0)
		for cycles := 0; ; cycles++ {
			stats.enter()
			wd.enter()
			ctx := i.journalPtr()
			i.journal[journal.CellEntry] = resume
			if resetFrame {
				i.fr.code = nil
				i.fr.upvals = nil
			}
			i.journal[journal.CellBudget] = loopBudget
			if err := entry.Callable.Call(ctx); err != nil {
				panic(err)
			}

			if earlySP {
				i.sp = int(i.journal[journal.CellSP])
			}
			trap := journal.Trap(i.journal[journal.CellTrap])
			if trap == journal.TrapNone {
				if popOnReturn {
					i.popFrame()
				} else {
					i.complete()
				}
				break
			}

			// A trap rebuilt the native call chain into real VM frames; resume the
			// innermost in the interpreter, surface a frame overflow, or service a
			// loop safepoint.
			if !earlySP {
				i.sp = int(i.journal[journal.CellSP])
			}
			i.deopt()
			if trap == journal.TrapBridge {
				next, ok := i.bridge(root, entry, wd, cycles)
				if !ok {
					break
				}
				resume = next
				continue
			}
			switch trap {
			case journal.TrapOverflow:
				panic(ErrFrameOverflow)
			case journal.TrapYield:
				stats.yield()
				// A loop back-edge spent its budget. deopt left i.fr at the loop header;
				// report it and run coordination, then let the threaded Run loop
				// continue from there.
				if err := i.yielded(); err != nil {
					panic(err)
				}
			default:
				stats.exit(i.journal[journal.CellExitID])
				wd.exit(i.journal[journal.CellExitID])
				// Record the exit as a branch so the tracer captures the leg and a
				// hot in-loop branch recompiles the tree with the leg folded in.
				i.exit(root)
				if loopShadow {
					// An exit that resumes at the header itself made no progress - the
					// header slot holds this native stub, so dispatching it again would
					// livelock (the hoist prologue's shape guard exits here). Run the
					// shadowed threaded handler once so the interpreter advances.
					i.resumeShadowed(root)
				} else {
					// A give-up that resumed at some function's own entry (ip 0) would
					// otherwise retrap immediately on redispatch; run that function's
					// shadowed entry handler once instead.
					i.resumeShadowed(jit.Anchor{Addr: i.fr.addr, IP: 0})
				}
			}
			break
		}
		i.checkRetire(root, wd, isFunction)
	}
}

// bridge runs the one opcode native code could not lower, through its own
// threaded closure, records the crossing on wd, and reports the IP native
// execution may resume at. Counting here rather than at each of the three
// wrappers keeps the tally with the crossing it measures (see watchdog).
// The trap already handed the interpreter a fully flushed, owned operand stack
// (see internal/jit/arm64's bridge lowering), so the closure runs exactly as
// it would under ordinary threaded dispatch.
//
// ok is false whenever native execution must not resume, and the caller
// continues in the interpreter instead: the closure moved to another frame or
// function (a call or a return), it made no forward progress, the new IP is
// not one the callable can be re-entered at (only a block the planner marked
// as a bridge resume carries an entry dispatch label, see internal/jit/arm64's
// dispatch), or this dispatch has already bridged its budget of cycles — that
// last case keeps a bridge-dense function reaching the Run loop's safepoints
// instead of cycling here indefinitely.
func (i *Interpreter) bridge(root jit.Anchor, entry jit.Entry, wd *watchdog, cycles int) (uint64, bool) {
	wd.bridge()
	f := i.fr
	if cycles >= loopBudget || f.addr != root.Addr {
		return 0, false
	}
	ip := f.ip
	if ip < 0 || ip >= len(i.code[f.addr]) {
		return 0, false
	}
	// Run the threaded handler, never whatever occupies the dispatch slot. An
	// installed native entry lives in that slot - the function entry at ip 0
	// this wrapper is already running inside, or a loop header compiled as its
	// own root - and invoking it here would re-enter native code from inside
	// this wrapper, resetting the journal the outer activation is about to
	// reuse. i.exits keeps the shadowed threaded closure for exactly this
	// case, the same one resumeShadowed uses when a give-up resumes on an anchor.
	closure := i.code[f.addr][ip]
	if shadowed, installed := i.exits[jit.Anchor{Addr: f.addr, IP: ip}]; installed {
		if shadowed == nil {
			return 0, false
		}
		closure = shadowed
	}
	closure(i)
	if i.fr != f || f.addr != root.Addr || f.ip <= ip {
		return 0, false
	}
	if !slices.Contains(entry.Resumable, f.ip) {
		return 0, false
	}
	return uint64(f.ip), true
}

func (i *Interpreter) popFrame() {
	f := i.fr
	i.sp = f.bp + f.returns
	if f.release {
		i.release(f.ref)
	}
	f.code = nil
	i.fp--
	i.fr = &i.frames[i.fp-1]
}

func (i *Interpreter) complete() {
	i.fr.ip = len(i.code[i.fr.addr])
	i.fr.code = i.code[i.fr.addr]
}

func (i *Interpreter) exit(root jit.Anchor) {
	hits := i.tracer.branch(i, root, jit.Anchor{Addr: i.fr.addr, IP: i.fr.ip})
	if i.cache != nil {
		if hits < exitThreshold || hits%exitThreshold != 0 {
			return
		}
		// Queue a side-exit build request without disturbing an active owner.
		i.cache.request(request{root: root, trigger: prof.TriggerSideExit})
		return
	}
	if hits != exitThreshold {
		return
	}
	if i.compiler == nil {
		return
	}
	i.samples.AddMetric("vm_jit_attempts_total", 1)
	result := i.attempt(i.compiler, root, prof.TriggerSideExit)
	if result.Err != nil {
		panic(result.Err)
	}
	if result.Code == nil {
		return
	}
	// A side exit asks for this root to be rebuilt with the exit's leg folded
	// in, and the answer is only an improvement if it is still a recording.
	// When the trace frontend cannot plan the tree any more the compiler falls
	// through to the static plan (see internal/jit/compiler.go's frontend
	// order), and installing that would swap the running recording - folded
	// legs, hoisted container - for the fallback that has neither.
	//
	// Module code keeps the behaviour it had. Its loop root competes with a
	// whole-module plan that is the program rather than one call of it, and
	// holding the recording there costs Control_Sieve about 10%.
	if live, ok := i.live[root]; ok && root.Addr != 0 && live.Frontend == prof.FrontendTrace {
		if rebuilt, ok := result.Code.Entries[root]; ok && rebuilt.Frontend != prof.FrontendTrace {
			// The code was still emitted, so it is still accounted; only the
			// dispatch slot stays with the recording that already owns it.
			i.account(result.Code)
			return
		}
	}
	i.install(result.Code, true)
}

// resumeShadowed runs the threaded handler installed anchor a's native code
// shadowed (see install) exactly once, when a give-up has resumed execution
// precisely at a's own installation point: redispatching a's native stub
// again there would just retrap or, for a loop header, livelock instead of
// making progress (see cycle). It is a no-op wherever execution resumed
// somewhere else, or nothing was ever shadowed at a.
func (i *Interpreter) resumeShadowed(a jit.Anchor) {
	if i.fr.addr != a.Addr || i.fr.ip != a.IP {
		return
	}
	if fn := i.exits[a]; fn != nil {
		fn(i)
	}
}

func (i *Interpreter) shared(root jit.Anchor, trigger prof.Trigger) error {
	addr := root.Addr
	i.samples.AddMetric("vm_jit_attempts_total", 1)
	compiler, err := newCompiler()
	if err != nil {
		i.samples.AddMetric("vm_jit_errors_total", 1)
		i.recordCompile(trigger, jit.Result{Anchor: root, Outcome: prof.CompileOutcomeError, Reason: prof.CompileReasonError, Err: err})
		i.cache.fail(addr)
		return err
	}
	if compiler == nil {
		i.recordCompile(trigger, jit.Result{Anchor: root, Outcome: prof.CompileOutcomeRejected, Reason: prof.CompileReasonBackendUnavailable})
		i.cache.fail(addr)
		return nil
	}
	result := i.attempt(compiler, root, trigger)
	if result.Err != nil {
		_ = compiler.Close()
		i.cache.fail(addr)
		return result.Err
	}
	if result.Code == nil {
		_ = compiler.Close()
		i.cache.fail(addr)
		return nil
	}
	mod := result.Code
	i.account(mod)
	var buf *asm.Buffer
	if len(mod.Entries) > 0 {
		buf = compiler.Buffer()
	} else {
		_ = compiler.Close()
	}
	i.cache.publish(addr, mod, buf)
	return nil
}

func (i *Interpreter) counters(a jit.Anchor, entry jit.Entry) counters {
	if i.profiler == nil {
		return counters{}
	}
	kind := entry.Kind.Profile()
	stats := counters{
		entry:  i.samples.RegisterEntry(a.Addr, a.IP, kind, entry.Frontend),
		yields: i.samples.RegisterYield(a.Addr, a.IP, kind, entry.Frontend),
		exits:  make([]*prof.Counter, len(entry.Exits)),
	}
	for id, exit := range entry.Exits {
		stats.exits[id] = i.samples.RegisterExit(a.Addr, a.IP, kind, entry.Frontend, exit.Reason, exit.Opcode)
	}
	return stats
}

func (i *Interpreter) account(mod *jit.Code) {
	i.samples.AddMetric("vm_jit_emits_total", float64(len(mod.Entries)))
	i.samples.AddMetric("vm_jit_bytes_total", float64(mod.Bytes))
	if i.profiler == nil {
		return
	}
	for a, entry := range mod.Entries {
		i.samples.RecordEmit(a.Addr, a.IP, entry.Kind.Profile(), entry.Frontend, entry.Bytes)
	}
}

func (i *Interpreter) recordCompile(trigger prof.Trigger, result jit.Result) {
	if i.profiler != nil {
		i.samples.RecordCompile(result.Anchor.Addr, result.Anchor.IP, trigger, result.Frontend, result.Outcome, result.Reason)
	}
}

// cool permanently stops instrumenting addr once every compilation root has
// been tried and nothing installed: further entry capture and back-edge
// observation would only pay dispatch overhead for no benefit (see checkCool).
// Reverting the function to the zero-overhead BR handler is what actually
// removes the loop instrumentation, since back-edge hooks are installed by
// default rather than upgraded into; sync still runs afterward so a peer's
// later publish can still be adopted (see install).
func (i *Interpreter) cool(addr int) {
	if addr < 0 || addr >= len(i.cold) || i.cold[addr] {
		return
	}
	i.cold[addr] = true
	if i.backedges[addr] {
		i.rethread(addr, false)
	}
}

// checkRetire evaluates wd's window (see watchdog.expired) once native code
// has already finished making this dispatch's forward progress — a normal
// return, a serviced yield, or a bailout have all already run — so retiring
// a's installed entry here never skips a step the interpreter owed the
// program. clearNatives is true only for a function-entry anchor (see
// Interpreter.retire).
func (i *Interpreter) checkRetire(a jit.Anchor, wd *watchdog, clearNatives bool) {
	if wd.failed() {
		i.retire(a, clearNatives)
	}
}

// retire undoes install for anchor a once its watchdog finds it spends at least
// retireGiveUpThreshold of a retireWindow giving up (see watchdog): it restores the shadowed threaded handler saved in i.exits,
// clears a's function-entry call-fast-path slot in i.natives when
// clearNatives is set (a null slot already makes callers fall back at the
// CALL, see install), and calls cool so addr is neither re-instrumented nor
// recompiled. The threaded handler must be restored before cool, because
// cool may rethread addr's whole table and preserves whatever i.code
// currently holds at every live anchor (see rethread).
//
// retire only ever writes i.code, i.natives, and i.cold, all local to this
// Interpreter, so it never reaches into a pool's shared published module (see
// sync). It is safe to call from inside the very wrapper closure it replaces:
// the caller is already done making this dispatch's progress and is about to
// return to the Run loop.
func (i *Interpreter) retire(a jit.Anchor, clearNatives bool) {
	if a.Addr < 0 || a.Addr >= len(i.code) || a.IP < 0 || a.IP >= len(i.code[a.Addr]) {
		return
	}
	if fn := i.exits[a]; fn != nil {
		i.code[a.Addr][a.IP] = fn
	}
	delete(i.live, a)
	if clearNatives && a.Addr < len(i.natives) {
		atomic.StorePointer(&i.natives[a.Addr], nil)
	}
	i.cool(a.Addr)
}

// rethread rebuilds addr's dispatch table for the given back-edge mode: true
// installs the counting back-edge handlers every function starts with, false
// reverts to the plain zero-overhead ones (see cool).
//
// The rethreaded handlers are copied into the table already installed rather
// than replacing it. Compile emits one handler per code byte, so both tables
// describe the same function at the same length, and keeping that slice live
// rewires every frame currently executing addr in place.
func (i *Interpreter) rethread(addr int, backedge bool) {
	fn, ok := i.function(addr)
	if !ok || fn == nil {
		return
	}
	c := i.threader(backedge)
	installed := i.code[addr]
	compiled := c.Compile(fn.Code, fn.Slots(), fn.Declared(), types.Kinds(fn.Captures), fn.Captures)
	// Rethreading replaces only interpreted handlers. Installed native entries
	// stay live while their saved fallbacks advance to the rebuilt table.
	for root := range i.exits {
		if root.Addr != addr || root.IP < 0 || root.IP >= len(compiled) || root.IP >= len(installed) {
			continue
		}
		i.exits[root] = compiled[root.IP]
		compiled[root.IP] = installed[root.IP]
	}
	copy(installed, compiled)
	i.backedges[addr] = backedge
}

// function returns the *types.Function at addr in the heap, or false if
// addr does not point at a function.
func (i *Interpreter) function(addr int) (*types.Function, bool) {
	if addr == 0 {
		return i.module, true
	}
	if addr <= 0 || addr >= len(i.heap) {
		return nil, false
	}
	fn, ok := i.heap[addr].(*types.Function)
	return fn, ok
}

// stub returns the threaded handler a function entry's native code shadows,
// or nil where nothing is installed at addr's entry (see install).
func (i *Interpreter) stub(addr int) func(*Interpreter) {
	return i.exits[jit.Anchor{Addr: addr}]
}

// isCold reports whether addr has been cooled (see cool).
func (i *Interpreter) isCold(addr int) bool {
	return addr >= 0 && addr < len(i.cold) && i.cold[addr]
}

// deopt rebuilds VM frames from the native journal after a trap. Native frames
// record themselves while unwinding, so record[depth-1] is the outermost native
// frame — already live at i.frames[i.fp-1]. Earlier records become deeper VM
// frames in reverse order, matching generated fused direct calls (ref
// unretained, code/upvals restored).
func (i *Interpreter) deopt() {
	depth := int(i.journal[journal.CellDepth])
	if depth == 0 {
		return
	}
	base := i.fp - 1

	// The last record is the live outermost frame; reconcile its resume state.
	fn, bp, ip, _ := i.unpack(depth - 1)
	outer := &i.frames[base]
	outer.bp = bp
	outer.ip = ip
	i.restore(outer, fn)

	// Earlier records become fresh frames from outer to inner. Like the fused
	// generated direct call, the callee ref was never pushed or retained, so
	// release stays false.
	for n := 1; n < depth; n++ {
		fn, bp, ip, returns := i.unpack(depth - 1 - n)
		f := &i.frames[base+n]
		f.addr = fn
		f.ref = fn
		f.release = false
		f.bp = bp
		f.ip = ip
		f.returns = returns
		i.restore(f, fn)
	}
	i.fp += depth - 1
	i.fr = &i.frames[i.fp-1]
}

// unpack reads frame record n from the native journal.
func (i *Interpreter) unpack(n int) (addr, bp, ip, returns int) {
	return int(i.journal[journal.At(n, journal.RecordAddr)]),
		int(i.journal[journal.At(n, journal.RecordBP)]),
		int(i.journal[journal.At(n, journal.RecordIP)]),
		int(i.journal[journal.At(n, journal.RecordReturns)])
}

// journalPtr resets and fills the journal handed to native code: stack/global base
// pointers, current frame BP/SP, pointer cells for native fast paths, and the
// per-call frame budget. It returns &journal[0], passed to native code in X0.
func (i *Interpreter) journalPtr() unsafe.Pointer {
	i.journal[journal.CellStack] = 0
	if len(i.stack) > 0 {
		i.journal[journal.CellStack] = uint64(uintptr(unsafe.Pointer(&i.stack[0])))
	}
	i.journal[journal.CellGlobals] = 0
	if len(i.globals) > 0 {
		i.journal[journal.CellGlobals] = uint64(uintptr(unsafe.Pointer(&i.globals[0])))
	}
	i.journal[journal.CellBP] = uint64(i.fr.bp)
	i.journal[journal.CellSP] = uint64(i.sp)

	i.journal[journal.CellRC] = 0
	if len(i.rc) > 0 {
		i.journal[journal.CellRC] = uint64(uintptr(unsafe.Pointer(&i.rc[0])))
	}
	i.journal[journal.CellUpvals] = 0
	if len(i.fr.upvals) > 0 {
		i.journal[journal.CellUpvals] = uint64(uintptr(unsafe.Pointer(&i.fr.upvals[0])))
	}
	i.journal[journal.CellHeap] = 0
	if len(i.heap) > 0 {
		i.journal[journal.CellHeap] = uint64(uintptr(unsafe.Pointer(&i.heap[0])))
	}
	i.journal[journal.CellNatives] = 0
	if len(i.natives) > 0 {
		i.journal[journal.CellNatives] = uint64(uintptr(unsafe.Pointer(&i.natives[0])))
	}

	i.journal[journal.CellDepth] = 0
	i.journal[journal.CellCap] = uint64(min(len(i.frames)-i.fp, nativeFrameLimit))
	i.journal[journal.CellTrap] = uint64(journal.TrapNone)
	i.journal[journal.CellExitID] = 0
	i.journal[journal.CellEntry] = 0
	i.journal[journal.CellBudget] = uint64(i.tick)
	i.journal[journal.CellActive] = 0
	return unsafe.Pointer(&i.journal[0])
}
