package interp

import (
	"errors"
	"math"
	"runtime"
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
	// Per-address tiering state is indexed by function address.
	calls []int
	// entries is native-writable: Context.Entries points at it, and every
	// Baseline function prologue increments its own address there, including
	// native-to-native entries.
	entries []int64
	deopts  []int
	// bridged counts resumed bridges unamortized by real native work since
	// the last one that was, per address (see resume/amortize); reaching
	// resume retires the address.
	bridged []int
	// failed records permanent compile failure per address and tier.
	failed [][2]bool
	// candidates holds every address with published Baseline code not yet
	// promoted or permanently blocked: drain's promotion scan visits only
	// these instead of every address's own entry count.
	candidates []int
	// sites indexes every observed OSR site by (address, ip): drain looks a
	// failed OSR unit's site up here to restore its threaded handler.
	sites map[key]*site

	// exact caches unfused threaded code for materialized frames, by address.
	exact [][]func(*Interpreter)

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

// refute is the deopt threshold that retires native code.
const refute = 8

// resume is how many unamortized bridges in a row (see amortized) retire a
// site: a resumed bridge round-trips through Go on every occurrence, so a
// site whose native work between bridges never pays for that cost (measured:
// AllocationGraph and BinaryTrees, whose bridges recur with no intervening
// loop work, cost more staying native than deopting once and running
// threaded) must stop resuming rather than pay the round trip forever.
// Matches refute: both give a site the same number of chances before giving
// up on its current tier.
const resume = refute

// amortize is the fewest back edges between two bridges (Context.Budget's
// own fall since the prior mark) that counts the second one as amortized by
// real native work rather than against resume. PermutationFlips recurses
// natively per call with ~47 back edges (two array fill/swap loops) between
// each array.new_default; AllocationGraph's own loop header takes exactly
// one back edge per bridge; BinaryTrees' struct.new_default recurses with
// none at all (no loop between two levels). 2 separates the first from the
// other two on both measured kernels.
const amortize = 2

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
		bridged:   make([]int, len(i.code)),
		failed:    make([][2]bool, len(i.code)),
		exact:     make([][]func(*Interpreter), len(i.code)),
		sites:     map[key]*site{},
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
// threaded CALL handlers.
func (n *native) attempt(i *Interpreter, addr int, fn *types.Function, release bool, advance int) bool {
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
	n.bridged[addr] = 0
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
// tiers every Baseline candidate whose prologue count reached jit.Promote.
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
			} else {
				n.markFailed(job.Unit.Address, job.Unit.Tier)
			}
			n.metric(i, metricCompiles, prof.Label{Key: "tier", Value: job.Unit.Tier.String()}, prof.Label{Key: "outcome", Value: outcome(job.Err)})
			continue
		}
		n.store.Publish(job.Code)
		if job.Code.Tier == jit.Baseline {
			n.candidates = append(n.candidates, job.Unit.Address)
		}
		n.metric(i, metricCompiles, prof.Label{Key: "tier", Value: job.Unit.Tier.String()}, prof.Label{Key: "outcome", Value: "ok"})
	}
	if len(n.candidates) == 0 {
		return
	}
	live := n.candidates[:0]
	for _, addr := range n.candidates {
		code := n.store.Code(addr)
		if code == nil || code.Tier != jit.Baseline || n.hasFailed(addr, jit.Optimized) {
			continue
		}
		if n.entries[addr] >= jit.Promote {
			n.queue.Submit(compile.Unit{Address: addr, Function: i.function(addr), Module: n.module, Tier: jit.Optimized})
		}
		live = append(live, addr)
	}
	n.candidates = live
}

// run executes one native call whose frame starts at bp and reports whether
// it should retire.
func (n *native) run(i *Interpreter, addr int, fn *types.Function, code *jit.Code, bp int, release bool, advance int) bool {
	returns := len(fn.Typ.Returns)

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
	mark := ctx.Budget

	if i.profiler != nil {
		n.metric(i, metricEntries, prof.Label{Key: "tier", Value: code.Tier.String()})
	}

	trap := jit.Enter(code.Entry(), ctx)
	for {
		if trap == jit.TrapReturn {
			boxRegisters(i, code, bp)
			if release {
				i.release(addr)
			}
			i.sp = bp + returns
			i.fr.ip += advance
			return false
		}

		exit := n.exit(ctx, code)
		if i.profiler != nil {
			n.metric(i, metricExits, prof.Label{Key: "kind", Value: exit.Kind.String()})
		}
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
			mark = ctx.Budget
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
			// Checked before serving: a deopt after a bridge would run the op twice.
			if n.bridged[addr] < resume && n.serve(i, exit) {
				n.bridged[addr] = n.amortized(mark, ctx.Budget, n.bridged[addr])
				mark = ctx.Budget
				ctx.Heap = heapBase(i.heap)
				ctx.RC = rcBase(i.rc)
				trap = jit.Resume(ctx)
				continue
			}
			n.deopt(i, exit, release, advance)
			if n.bridged[addr] >= resume {
				// The site bridged every native entry with nothing else
				// between: retire rather than pay the round trip forever.
				return true
			}
			return n.refute(addr)
		default:
			n.deopt(i, exit, release, advance)
			return n.refute(addr)
		}
	}
}

// serve runs a resumable ExitBridge or ExitBox in Go; false declines, and
// the exit deopts.
func (n *native) serve(i *Interpreter, exit jit.Exit) bool {
	if exit.Kind == jit.ExitBox {
		return n.widen(i, exit)
	}
	return bridgeable(exit.Code) && n.bridge(i, exit)
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

// bridgeable reports whether code's threaded handler may run once, in place,
// to resume native code: it never releases a heap reference or overwrites
// its own argument stack slots before its only possible panic (allocation
// failure), so a decline neither double-mutates nor loses what bridge
// retained for cleanup. STRING_CONCAT is excluded: its handler releases
// both operands before allocating the joined result, so a decline there
// would re-release on threaded retry. array.new is excluded: its true pop
// count can exceed the SSA Args this backend records for it (a variadic
// length), unlike the three allowlisted here whose Args always equal their
// real pop count.
func bridgeable(code instr.Opcode) bool {
	switch code {
	case instr.STRUCT_NEW, instr.STRUCT_NEW_DEFAULT, instr.ARRAY_NEW_DEFAULT:
		return true
	default:
		return false
	}
}

// bridge runs exit's opcode once through its own threaded handler against a
// scratch operand stack, and reports whether native code may resume.
// Promoted locals and other live values stay in native registers and spill
// slots, entirely untouched: asm.Resume already restores every allocatable
// register regardless of what bridge does. Operands cross through boxed
// interpreter values; a borrowed one (Owned false), or one native will
// independently release downstream (outside the top Adopts operands, per
// transform.Adopts), is retained fresh so the handler's own consumption
// never touches native's copy. A ref result is left owned by the handler's
// own push, matching native code's own expectation of an owned value.
//
// A trap declines instead of unwinding: on a Go call chain from n.run/
// osr.settle through native.call/native.enter, only a normal return runs
// n.store.Leave(); re-raising the panic here would skip it and corrupt the
// store's in-native accounting. Declining releases bridge's own extra
// retains and lets the caller's existing deopt path (unchanged) rebuild the
// interpreter frame from the same exit map and let real threaded execution
// hit the same trap once, under dispatch's own recover.
func (n *native) bridge(i *Interpreter, exit jit.Exit) bool {
	if i.fp >= len(i.frames) {
		return false
	}

	ctx := n.ctx
	k := int(ctx.Depth) - 1
	m := exit.Frames[0]
	bp := int((ctx.Records[k].FB - ctx.Stack) / unsafe.Sizeof(types.Boxed(0)))
	sp := bp + len(i.function(m.Address).Declared())

	tail := m.Stack[len(m.Stack)-exit.Pops:]
	top := len(tail) - exit.Adopts
	for j, o := range tail {
		v := n.box(i, o.Value.Kind, ctx.Read(k, o.Value))
		// A boxed wide i64 is fresh and already owned; only a ref borrows.
		if o.Value.Kind == types.KindRef && (!o.Owned || j < top) {
			i.retainBox(v)
		}
		i.stack[sp+j] = v
	}

	savedFr, savedSP := i.fr, i.sp
	f := &i.frames[i.fp]
	*f = frame{addr: m.Address, code: n.exactCode(i, m.Address), bp: bp, ip: m.IP}
	i.fr = f
	i.sp = sp + len(tail)

	ok := n.exec(i, f, exit.Results)

	i.fr = savedFr
	if !ok {
		// A bridgeable handler never writes its argument slots before it can
		// panic, so i.stack[sp+j] still holds what was retained above.
		for j, o := range tail {
			if o.Value.Kind == types.KindRef && (!o.Owned || j < top) {
				i.releaseBox(i.stack[sp+j])
			}
		}
		i.sp = savedSP
	}
	return ok
}

// exec runs f's own instruction and, on success, unboxes the len(results)
// values it pushed into Context.Results. A panic is recovered and reported
// as a decline; nothing i.heap/i.rc-visible happens before it for any
// bridgeable opcode (see bridgeable), so a decline needs no further cleanup
// of what the handler itself touched.
func (n *native) exec(i *Interpreter, f *frame, results []types.Kind) (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	f.code[f.ip](i)
	for j, kind := range results {
		n.ctx.Results[j] = n.word(i, kind, i.stack[i.sp-len(results)+j])
	}
	return true
}

// word converts a KindRef boxed value to its raw native word: refs are
// boxed words natively (no runtime tag transform), so this is a plain
// reinterpretation. Every bridgeable opcode's own result is a ref.
func (n *native) word(i *Interpreter, kind types.Kind, v types.Boxed) uint64 {
	if kind != types.KindRef {
		panic("interp: bridge result kind " + kind.String() + " is not a ref")
	}
	return uint64(v)
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

// exit resolves ctx's current exit map. Depth 1 means the activation that
// exited is the one entered, own: no native call has run since, so its
// code is entered's own, avoiding Store.Find's locked scan (osr/bridge
// resume loops keep Depth at 1 for as long as the unit makes no native
// call, the common shape of a bridge-dominated loop). A deeper Depth means
// some native call changed which code is running, so only Find resolves it.
func (n *native) exit(ctx *jit.Context, entered *jit.Code) jit.Exit {
	code := entered
	if ctx.Depth != 1 {
		code = n.store.Find(ctx.PC())
	}
	return code.Exits[ctx.Exit()]
}

// amortized reports bridged's next value: reset to 0 when at least amortize
// back edges (mark - now, Context.Budget only falls between refills) ran
// since the prior bridge, else bridged+1.
func (n *native) amortized(mark, now int64, bridged int) int {
	if mark-now >= amortize {
		return 0
	}
	return bridged + 1
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
