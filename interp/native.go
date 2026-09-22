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

// native holds one interpreter's JIT runtime: its own native execution
// context and per-address tiering state, plus the shared published code,
// compile queue, and constant module a Pool may lend to every interpreter of
// one Program. It is nil on an interpreter built without WithThreshold.
type native struct {
	ctx *jit.Context
	*shared

	threshold int
	// calls, entries, deopts, and failed are indexed by address, up to
	// len(i.code): Store.Publish already refuses an address beyond that, so
	// no address past it ever compiles.
	calls   []int
	entries []int
	deopts  []int
	// failed marks a permanently failed compile attempt: bit tierBit(tier)
	// of failed[addr] is set once tier has failed at addr. A Baseline
	// failure never blocks a later Optimized attempt at the same address,
	// and vice versa.
	failed []uint8

	// exact caches deoptimization's own compile of each address: i.code[addr]
	// may be fused, and a fusion leaves no handler at the IPs it absorbs (see
	// threader.Compile), so a materialized frame must run code compiled
	// exactly, which this builds once per address and keeps for every later
	// deoptimization at it.
	exact map[int][]func(*Interpreter)

	// compile is i.compile, captured once here rather than called on i
	// directly: threaded[] stores the CALL handler that reaches native.call,
	// so a direct reference from deoptimization back to compile's own
	// threader.Compile (which threaded[] indexes) would be a package
	// initialization cycle. Going through this field instead of the method
	// stays outside that static reference graph.
	compile func(fn *types.Function, exact bool) []func(*Interpreter)
}

// shared is the part of a native's JIT runtime a Pool may lend to every
// interpreter of one Program: the published code, the compile queue, and the
// constant module every compile reads (L11). Nothing here is interpreter-
// specific; jit.Context and the tiering counters are not, and stay on native
// itself. refs counts how many natives are using it, closing it only once
// the last one releases — Put on a closed Pool can defer an outstanding
// Interpreter's Close to a later call, so a Pool cannot always name the
// native that closes last, and reference counting is the rule that is
// correct regardless.
type shared struct {
	store  *jit.Store
	queue  *compile.Queue
	module transform.Module

	refs atomic.Int64
}

// nativeStack is the size of the Go-allocated stack native code runs on.
const nativeStack = 1 << 20

// budget is Context.Budget's refill: how many loop back-edges native code
// takes before it reaches a safepoint.
const budget = 1 << 16

// promote is how many times an address's Baseline code is entered before it
// is submitted for an Optimized compile.
const promote = 1000

// refute is how many times an address deoptimizes before its code is
// retired and its tiering counts reset, so it may compile again.
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
	return &native{
		ctx:       ctx,
		shared:    newShared(i),
		threshold: threshold,
		calls:     make([]int, len(i.code)),
		entries:   make([]int, len(i.code)),
		deopts:    make([]int, len(i.code)),
		failed:    make([]uint8, len(i.code)),
		exact:     map[int][]func(*Interpreter){},
		compile:   i.compile,
	}
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

	n.store.Enter()
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
		n.forget(addr)
	}
	_ = n.store.Reclaim()
	return true
}

// count tracks calls to addr and submits it for Baseline compilation once
// the threshold is reached, unless Baseline already failed there.
func (n *native) count(i *Interpreter, addr int, fn *types.Function) {
	n.calls[addr]++
	if n.calls[addr] < n.threshold || n.hasFailed(addr, jit.Baseline) {
		return
	}
	n.queue.Submit(compile.Unit{Address: addr, Function: fn, Module: n.module, Tier: jit.Baseline})
}

// promote counts a native entry to addr's Baseline code and submits it for
// an Optimized compile once entries reach the promote threshold, unless
// Optimized already failed there.
func (n *native) promote(addr int, fn *types.Function, code *jit.Code) {
	if code.Tier != jit.Baseline {
		return
	}
	n.entries[addr]++
	if n.entries[addr] < promote || n.hasFailed(addr, jit.Optimized) {
		return
	}
	n.queue.Submit(compile.Unit{Address: addr, Function: fn, Module: n.module, Tier: jit.Optimized})
}

// refute counts a deoptimization of addr and reports whether it reached the
// refute threshold, at which point call retires addr's code and resets its
// tiering counts.
func (n *native) refute(addr int) bool {
	n.deopts[addr]++
	return n.deopts[addr] >= refute
}

// forget resets addr's call, entry, and deopt counts after call retires its
// code, so it tiers up again from a fresh Baseline compile if it is still
// called. A permanent compile failure (failed) is not runtime behavior and
// stays.
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

// drain collects every job the queue has finished — including one a Pool's
// other interpreter submitted, when the runtime is shared, since Publish is
// safe and idempotent on a stale result — publishing successful code and
// marking a failed (address, tier) so it is never resubmitted.
func (n *native) drain(i *Interpreter) {
	for _, job := range n.queue.Drain() {
		if job.Err != nil {
			n.markFailed(job.Unit.Address, job.Unit.Tier)
			n.metric(i, metricCompiles, prof.Label{Key: "tier", Value: job.Unit.Tier.String()}, prof.Label{Key: "outcome", Value: outcome(job.Err)})
			continue
		}
		n.store.Publish(job.Code)
		n.metric(i, metricCompiles, prof.Label{Key: "tier", Value: job.Unit.Tier.String()}, prof.Label{Key: "outcome", Value: "ok"})
	}
}

// run drives code to completion for a call to fn at addr whose frame would
// begin at bp, the same bp pushFrame would have computed. It reports whether
// addr deoptimized enough times for call to retire its code.
func (n *native) run(i *Interpreter, addr int, fn *types.Function, code *jit.Code, release bool, advance int) bool {
	params := len(fn.Typ.Params)
	returns := len(fn.Typ.Returns)
	bp := i.sp - params
	if release {
		bp--
	}

	ctx := n.ctx
	ctx.Stack = base(i.stack)
	ctx.Globals = base(i.globals)
	ctx.RC = rcBase(i.rc)
	ctx.Natives = n.store.Natives()
	ctx.Top = end(i.stack)
	ctx.FB = base(i.stack[bp:])
	ctx.Limit = uint64(min(len(ctx.Records), len(i.frames)-i.fp))
	ctx.Budget = budget
	ctx.Depth = 0

	n.metric(i, metricEntries, prof.Label{Key: "tier", Value: code.Tier.String()})
	n.promote(addr, fn, code)

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
			ctx.RC = rcBase(i.rc)
			ctx.Budget = budget
			trap = jit.Resume(ctx)
		case jit.ExitRelease:
			if ref := types.Boxed(ctx.Read(int(ctx.Depth)-1, exit.Release)).Ref(); ref != 0 {
				i.release(ref)
			}
			ctx.RC = rcBase(i.rc)
			trap = jit.Resume(ctx)
		default:
			n.deopt(i, exit, release, advance)
			return n.refute(addr)
		}
	}
}

// deopt materializes every activation ctx.Depth counts as an interpreter
// frame, outermost first, and leaves the interpreter positioned to continue
// threaded at the innermost one. exit is the map of whichever exit brought
// native code here; release and advance are the call site's own, since the
// outermost frame's ownership and the entering caller's ip advance must match
// it exactly (a fused CONST_GET;CALL borrows its target and advances by its
// own width, unlike a dynamic CALL).
func (n *native) deopt(i *Interpreter, exit jit.Exit, release bool, advance int) {
	ctx := n.ctx
	depth := int(ctx.Depth)

	maps := make([]jit.Frame, depth)
	maps[depth-1] = exit.Frames[0]
	for k := depth - 2; k >= 0; k-- {
		code := n.store.Find(ctx.Records[k+1].PC)
		maps[k] = code.Exits[ctx.Records[k].Exit].Frames[0]
	}

	start := i.fp
	for k := 0; k < depth; k++ {
		n.frame(i, ctx, start, k, maps[k], k > 0 || release)
	}
	inner := &i.frames[start+depth-1]

	if exit.Kind == jit.ExitCall {
		callee := i.heap[exit.Callee].(*types.Function)
		i.sp += len(callee.Typ.Params)
		i.stack[i.sp] = types.BoxRef(exit.Callee)
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

// frame materializes activation k of ctx, whose exit map is m, as interpreter
// frame base+k. Every native activation was entered by a CALL that adopted
// its callee reference, so release is true except for the outermost, which
// carries the entering call site's own release.
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

	fn := i.heap[m.Address].(*types.Function)
	sp := f.bp + len(fn.Declared())
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
	code := n.compile(i.heap[addr].(*types.Function), true)
	n.exact[addr] = code
	return code
}

// box materializes a native word of kind into the interpreter's Boxed
// representation: i1/i8/i32 and f32 take it from the low 32 bits, f64 and ref
// carry it unchanged, and a wide i64 promotes through boxI64 exactly as a
// slot store would.
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
