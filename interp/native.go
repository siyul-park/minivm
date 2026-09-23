package interp

import (
	"errors"
	"math"
	"runtime"
	"sync/atomic"
	"unsafe"

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
	// Per-address tiering state is indexed by function address.
	calls []int
	// entries is native-writable: Context.Entries points at it, and every
	// Baseline function prologue increments its own address there, including
	// native-to-native entries.
	entries []int64
	deopts  []int
	// failed records permanent compile failure per address and tier.
	failed []uint8

	// exact caches unfused threaded code for materialized frames.
	exact map[int][]func(*Interpreter)

	// compile is captured once so deoptimization does not add a static
	// dependency from generated threaded handlers back to their compiler.
	// Calling i.compile here directly creates the threaded/fusions
	// initialization cycle.
	compile func(fn *types.Function, exact bool) []func(*Interpreter)
}

// shared contains the pool-shareable native runtime: Store, Queue, and Module.
// refs closes it after the last interpreter releases it.
type shared struct {
	store  *jit.Store
	queue  *compile.Queue
	module transform.Module

	refs atomic.Int64
}

// nativeStack is the native stack size per interpreter.
const nativeStack = 1 << 20

// budget is the back-edge count between safepoints.
const budget = 1 << 16

// promote is the Baseline-to-Optimized entry threshold.
const promote = 1000

// refute is the deopt threshold that retires native code.
const refute = 8

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
			objects[addr] = transform.Object{Type: v.Typ}
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
		calls:     make([]int, len(i.code)),
		entries:   make([]int64, len(i.code)),
		deopts:    make([]int, len(i.code)),
		failed:    make([]uint8, len(i.code)),
		exact:     map[int][]func(*Interpreter){},
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
	n.drain(i)
	if addr >= len(n.calls) {
		// Bound after construction (Alloc, Store): never compiled.
		return false
	}
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
	retire := n.run(i, addr, fn, code, release, advance)
	n.store.Leave()
	if retire {
		n.store.Retire(addr)
		// The unchanged input has no new feedback for a recompile at this tier.
		n.markFailed(addr, code.Tier)
		n.forget(addr)
	}
	_ = n.store.Reclaim()
	return true
}

// count tracks cold calls and requests Baseline compilation.
func (n *native) count(i *Interpreter, addr int, fn *types.Function) {
	n.calls[addr]++
	if n.calls[addr] < n.threshold || n.hasFailed(addr, jit.Baseline) {
		return
	}
	n.queue.Submit(compile.Unit{Address: addr, Function: fn, Module: n.module, Tier: jit.Baseline})
}

// refute counts deopts; refute retires the code at its current tier.
func (n *native) refute(addr int) bool {
	n.deopts[addr]++
	return n.deopts[addr] >= refute
}

// forget resets runtime tiering counters; failed tiers remain blocked.
func (n *native) forget(addr int) {
	n.calls[addr] = 0
	n.entries[addr] = 0
	n.deopts[addr] = 0
}

// hasFailed reports whether addr's compile at tier permanently failed.
func (n *native) hasFailed(addr int, tier jit.Tier) bool {
	return n.failed[addr]&tierBit(tier) != 0
}

// markFailed permanently marks addr's compile at tier as failed.
func (n *native) markFailed(addr int, tier jit.Tier) {
	n.failed[addr] |= tierBit(tier)
}

// tierBit is tier's bit in a failed[addr] mark.
func tierBit(tier jit.Tier) uint8 {
	return 1 << (tier - 1)
}

// drain publishes completed jobs, records permanent compile failures, and
// tiers every address the prologue's own entry count has since promoted.
func (n *native) drain(i *Interpreter) {
	for _, job := range n.queue.Drain() {
		if job.Err != nil {
			// failed is the entry-0 call site's own tiering; an OSR unit is
			// a different site, so only a non-OSR failure marks it.
			if !job.Unit.OSR {
				n.markFailed(job.Unit.Address, job.Unit.Tier)
			}
			n.metric(i, metricCompiles, prof.Label{Key: "tier", Value: job.Unit.Tier.String()}, prof.Label{Key: "outcome", Value: outcome(job.Err)})
			continue
		}
		n.store.Publish(job.Code)
		n.metric(i, metricCompiles, prof.Label{Key: "tier", Value: job.Unit.Tier.String()}, prof.Label{Key: "outcome", Value: "ok"})
	}
	for addr, count := range n.entries {
		if count < promote || n.hasFailed(addr, jit.Optimized) {
			continue
		}
		code := n.store.Code(addr)
		if code == nil || code.Tier != jit.Baseline {
			continue
		}
		n.queue.Submit(compile.Unit{Address: addr, Function: i.function(addr), Module: n.module, Tier: jit.Optimized})
	}
}

// run executes one native call and reports whether it should retire.
func (n *native) run(i *Interpreter, addr int, fn *types.Function, code *jit.Code, release bool, advance int) bool {
	params := len(fn.Typ.Params)
	returns := len(fn.Typ.Returns)
	bp := i.sp - params
	if release {
		bp--
	}

	ctx := n.ctx
	ctx.Stack = base(i.stack)
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

	n.metric(i, metricEntries, prof.Label{Key: "tier", Value: code.Tier.String()})

	trap := jit.Enter(code.Entry(), ctx)
	for {
		if trap == jit.TrapReturn {
			if release {
				i.release(addr)
			}
			i.sp = bp + returns
			i.fr.ip += advance
			return false
		}

		exit := n.store.Find(ctx.PC()).Exits[ctx.Exit()]
		n.metric(i, metricExits, prof.Label{Key: "kind", Value: exit.Kind.String()})
		switch exit.Kind {
		case jit.ExitSafepoint:
			if cancelled(i) {
				// Threaded code reports the cancellation at its next safepoint,
				// as an error no guest handler can catch.
				n.deopt(i, exit, release, advance)
				return n.refute(addr)
			}
			ctx.Heap = heapBase(i.heap)
			ctx.RC = rcBase(i.rc)
			ctx.Budget = budget
			n.drain(i)
			trap = jit.Resume(ctx)
		case jit.ExitRelease:
			if ref := types.Boxed(ctx.Read(int(ctx.Depth)-1, exit.Release)).Ref(); ref != 0 {
				i.release(ref)
			}
			ctx.Heap = heapBase(i.heap)
			ctx.RC = rcBase(i.rc)
			trap = jit.Resume(ctx)
		default:
			n.deopt(i, exit, release, advance)
			return n.refute(addr)
		}
	}
}

// deopt materializes native activations outermost-first and positions the
// interpreter at the innermost threaded frame. release/advance preserve the
// entering call site's ownership and IP semantics.
func (n *native) deopt(i *Interpreter, exit jit.Exit, release bool, advance int) {
	ctx := n.ctx
	depth := int(ctx.Depth)

	maps := make([]jit.Frame, depth)
	owns := make([]bool, depth)
	maps[depth-1] = exit.Frames[0]
	owns[0] = release
	for k := depth - 2; k >= 0; k-- {
		code := n.store.Find(ctx.Records[k+1].PC)
		e := code.Exits[ctx.Records[k].Exit]
		maps[k] = e.Frames[0]
		owns[k+1] = e.Owned
	}

	start := i.fp
	for k := 0; k < depth; k++ {
		n.frame(i, ctx, start, k, maps[k], owns[k])
	}
	inner := &i.frames[start+depth-1]

	if exit.Kind == jit.ExitCall {
		callee := i.heap[exit.Callee].(*types.Function)
		i.sp += len(callee.Typ.Params)
		ref := types.BoxRef(exit.Callee)
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
	}

	i.fr.ip += advance
	i.fp += depth
	i.fr = inner

	ctx.Depth = 0
	ctx.Abandon()
}

// frame materializes activation k from m. release reports whether k owns
// the callee reference it was entered with: the outermost activation
// follows the entering call site, and every other one follows the call
// site's own Owned decision in the caller's compiled code.
func (n *native) frame(i *Interpreter, ctx *jit.Context, start, k int, m jit.Frame, release bool) {
	f := &i.frames[start+k]
	f.addr = m.Address
	f.code = n.exactCode(i, m.Address)
	f.ref = m.Address
	f.release = release
	f.bp = int((ctx.Records[k].FB - ctx.Stack) / unsafe.Sizeof(types.Boxed(0)))
	f.returns = m.Returns
	f.ip = m.IP
	f.upvals = nil
	f.coro = 0

	sp := f.bp + len(i.function(m.Address).Declared())
	for j, o := range m.Stack {
		boxed := n.box(i, o.Value.Kind, ctx.Read(k, o.Value))
		if !o.Owned {
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
	if code, ok := n.exact[addr]; ok {
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

func (n *native) metric(i *Interpreter, name string, labels ...prof.Label) {
	if i.profiler == nil {
		return
	}
	i.samples.AddMetric(name, 1, labels...)
}

func outcome(err error) string {
	if errors.Is(err, compile.ErrUnsupported) {
		return "unsupported"
	}
	return "failed"
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
