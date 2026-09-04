package interp

import (
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/compile"
	"github.com/siyul-park/minivm/internal/jit/tier"
	"github.com/siyul-park/minivm/internal/journal"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

// This file drives an Interpreter against the architecture-neutral
// internal/jit driver: building the compile-time-stable snapshot a Compile
// call plans and lowers against, serving a claimed compile job, and installing
// the resulting native entries into the dispatch table.
// internal/jit/compile owns the job queue serve is claimed from, the store it
// publishes into, and whether the build runs here or on a worker; a pool
// shares all three and a solo interpreter keeps them private. interp/tier.go
// owns tier-up and retirement policy - counters, the tier.Watchdog, and when
// to request a compile, cool, or retire - and drives this file against that
// policy. interp/deopt.go owns the native dispatch wrappers threaded code
// hands control to and the path back into the interpreter after a trap.

// builds is the handoff between a build and the interpreter that claimed it.
// The build may run on the queue's worker, but recording its profile rows and
// installing its code belong to the claiming interpreter, whose collector and
// dispatch table nothing else may touch, so a finished build parks here until
// that interpreter reaches a safepoint (see sync).
type builds struct {
	done   []build
	parked atomic.Bool

	wg sync.WaitGroup
	mu sync.Mutex
}

// build is one finished compile: what the compiler produced and the event
// that asked for it.
type build struct {
	result  jit.Result
	trigger prof.Trigger
}

// start registers a claimed build, so wait holds until it ends.
func (b *builds) start() {
	b.wg.Add(1)
}

// push parks a finished build's outcome for the next take.
func (b *builds) push(next build) {
	b.mu.Lock()
	b.done = append(b.done, next)
	b.parked.Store(true)
	b.mu.Unlock()
}

// end releases the registration start took, once the build has published
// everything it produced.
func (b *builds) end() {
	b.wg.Done()
}

// take removes and returns every build parked since the last call. Nothing
// parked answers from the flag alone, because every safepoint asks and almost
// none of them find anything.
func (b *builds) take() []build {
	if !b.parked.Load() {
		return nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	done := b.done
	b.done = nil
	b.parked.Store(false)
	return done
}

// wait blocks until every claimed build has ended, which is what lets a
// closing interpreter drop its hold on the store knowing nothing is still
// publishing into it.
func (b *builds) wait() {
	b.wg.Wait()
}

// serve compiles the claimed job. Only what this interpreter alone can
// produce is resolved here: the compile-time snapshot, which reads private
// runtime state, and the compiler the build links into. The build itself runs
// wherever the queue puts it, so the sync below adopts it immediately when the
// queue ran it inline and a later safepoint adopts it when a worker did.
func (i *Interpreter) serve(job compile.Job) error {
	root := job.Root
	i.tried[root] = true
	i.samples.AddMetric("vm_jit_attempts_total", 1)
	i.builds.start()

	compiler, err := newCompiler()
	switch {
	case err != nil:
		i.reject(job, jit.Result{Anchor: root, Outcome: prof.CompileOutcomeError, Reason: prof.CompileReasonError, Err: err})
	case compiler == nil:
		i.reject(job, jit.Result{Anchor: root, Outcome: prof.CompileOutcomeRejected, Reason: prof.CompileReasonBackendUnavailable})
	default:
		input, ok := i.compileSnapshot(root.Addr)
		if !ok {
			_ = compiler.Close()
			i.reject(job, jit.Result{Anchor: root, Outcome: prof.CompileOutcomeEmpty, Reason: prof.CompileReasonNoInput})
			break
		}
		i.queue.Serve(func() { i.compile(job, compiler, input) })
	}
	return i.sync()
}

// compile runs one claimed build and is the only part of serve that may run
// off this interpreter's own goroutine. It reads the immutable snapshot and
// publishes what it emitted for everyone sharing the store; it touches no
// dispatch table, counter, or runtime state, all of which stay with the
// interpreter that adopts the outcome.
func (i *Interpreter) compile(job compile.Job, compiler *jit.Compiler, input *jit.Input) {
	result := compiler.Compile(input, job.Root)
	// Park the outcome before publishing the code. sync adopts what is parked
	// before it installs what is published, so a native entry never starts
	// counting dispatches before the build that emitted it was recorded.
	i.builds.push(build{result: result, trigger: job.Trigger})
	if result.Code == nil {
		_ = compiler.Close()
	} else {
		// The store takes over the buffer the callables were linked into,
		// because they stay executable for as long as anyone sharing the store
		// can still dispatch into them.
		i.store.Publish(result.Code, compiler.Buffer())
	}
	// The claim ends after the publish, so a peer that learns the root is built
	// finds its code; the registration ends last, so a Close waiting on it
	// knows nothing is still publishing into the store.
	i.queue.Done(job.Root.Addr, result.Code)
	i.builds.end()
}

// reject ends a claim nothing was built for. The outcome is still this
// interpreter's to record, so it parks like any other, and the next winner can
// take the queue.
func (i *Interpreter) reject(job compile.Job, result jit.Result) {
	i.builds.push(build{result: result, trigger: job.Trigger})
	i.queue.Done(job.Root.Addr, nil)
	i.builds.end()
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
		Constants: i.constants,
		Globals:   i.globalKinds(),
		Objects:   i.objects(),
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

// objects resolves the heap cells a compile may name into the immutable facts
// jit.Object records: address zero for the module body, every cell the
// constant pool publishes, and every function the host bound at a runtime
// address. Those are exactly the addresses a plan can reach — a static plan
// resolves a container or a callee only through a constant, and a trace plan
// names a callee the tracer already resolved to a function address.
//
// Resolving here, on the goroutine that owns the interpreter, is what lets a
// compile run without reading heap storage execution keeps mutating: a
// *types.Struct is recycled through a pool that rewrites its type, an array
// header is rewritten in place, and a released slot is handed to the next
// allocation, so neither the slot nor the object behind it is stable to read
// from elsewhere.
func (i *Interpreter) objects() jit.Objects {
	objects := make(jit.Objects, len(i.constants)+len(i.dynamic)+1)
	objects[0] = jit.Object{Fn: i.module}
	for _, val := range i.constants {
		if val.Kind() != types.KindRef {
			continue
		}
		if object, ok := i.object(val.Ref()); ok {
			objects[val.Ref()] = object
		}
	}
	for addr := range i.dynamic {
		if object, ok := i.object(addr); ok {
			objects[addr] = object
		}
	}
	return objects
}

// object resolves the cell at addr, and reports false when addr names none. A
// cell carrying nothing a compile reads still resolves, because presence is
// how a lowering tests that an address names a live cell it may retain.
func (i *Interpreter) object(addr int) (jit.Object, bool) {
	if addr <= 0 || addr >= len(i.heap) {
		return jit.Object{}, false
	}
	var object jit.Object
	switch val := i.heap[addr].(type) {
	case *types.Function:
		object.Fn = val
	case *types.Closure:
		object.Calls = int(val.Fn)
	case *types.Struct:
		object.Typ = val.Typ
	case types.TypedArray[bool], types.TypedArray[int8], types.TypedArray[int32],
		types.TypedArray[int64], types.TypedArray[float32], types.TypedArray[float64]:
		object.Array = jit.Itab(val)
	}
	return object, true
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

// install rewires the dispatch table for every entry mod emitted: a trace
// entry replaces the function's first opcode handler and keeps the shadowed
// threaded handler for guard fallback. Accounting belongs to serve, which
// knows whether this interpreter is the one that compiled mod.
func (i *Interpreter) install(mod *jit.Code) {
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
		wd := tier.New(entry)
		i.watchdogs[a] = wd
		i.code[a.Addr][a.IP] = i.cycle(a, entry, stats, wd)
	}
}

// covered reports whether the static plan about to install at a would take
// work away from a better root already dispatching: the recording at a itself,
// or a loop root of the same function that a swallows. Only a static plan can
// lose here: the trace frontend anchors a nested loop as an edge to that
// root's own entry instead of inlining it, so a recorded plan never takes an
// inner root's work away in the first place.
//
// A side exit asks for its root to be rebuilt with the exit's leg folded in,
// and that answer is only an improvement while it is still a recording. When
// the trace frontend can no longer plan the tree the compiler falls through to
// the static plan (see internal/jit/compiler.go's frontend order), and
// installing that would swap the running recording - folded legs, hoisted
// container - for the fallback that has neither. Module code is excluded for
// the same reason swallows excludes it.
func (i *Interpreter) covered(a jit.Anchor, entry jit.Entry) bool {
	if entry.Frontend != prof.FrontendStatic {
		return false
	}
	if live, ok := i.live[a]; ok && a.Addr != 0 && live.Frontend == prof.FrontendTrace {
		return true
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
		return i.tracer.encloses(i.instrs, a.Addr, a.IP, header)
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
// instrumented and its tier.Watchdog must stay free to retire the inner root later.
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

// sync adopts everything waiting for this interpreter, and is where a compile
// that ran elsewhere rejoins the goroutine owning the counters and the
// dispatch table: it records the outcome of every build this interpreter
// claimed, then installs every code published since it last looked, whether it
// compiled that code itself or a peer sharing the store did.
func (i *Interpreter) sync() error {
	err := i.adopt()
	for {
		code, ok := i.store.Code(i.gen)
		if !ok {
			return err
		}
		i.install(code)
		i.gen++
	}
}

// adopt records the outcome of every build this interpreter claimed. Compile
// and emission rows follow compilation ownership whatever goroutine ran the
// build, and a prof.Collector is owned by one interpreter, so they are
// recorded here rather than where the build finished. It returns the first
// compile error, which reaches Run through the safepoint that adopted it.
func (i *Interpreter) adopt() error {
	var err error
	for _, done := range i.builds.take() {
		if done.result.Err != nil {
			i.samples.AddMetric("vm_jit_errors_total", 1)
			if err == nil {
				err = done.result.Err
			}
		}
		if done.result.Code != nil {
			i.account(done.result.Code)
		}
		if i.profiler != nil {
			a := done.result.Anchor
			i.samples.RecordCompile(a.Addr, a.IP, done.trigger, done.result.Frontend, done.result.Outcome, done.result.Reason)
		}
	}
	return err
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
