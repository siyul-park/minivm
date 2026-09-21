package interp

import (
	"errors"
	"runtime"
	"unsafe"

	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/arm64"
	"github.com/siyul-park/minivm/internal/jit/compile"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/transform"
	"github.com/siyul-park/minivm/types"
)

// native holds one interpreter's JIT runtime: the shared native execution
// context, the published code, the compile queue, and per-address tiering
// state. It is nil on an interpreter built without WithThreshold.
type native struct {
	ctx    *jit.Context
	store  *jit.Store
	queue  *compile.Queue
	module transform.Module

	threshold int
	calls     map[int]int
	failed    map[int]bool
	pending   int
}

// nativeStack is the size of the Go-allocated stack native code runs on.
const nativeStack = 1 << 20

// budget is Context.Budget's refill: how many loop back-edges native code
// takes before it reaches a safepoint.
const budget = 1 << 16

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

func newNative(i *Interpreter, threshold int) *native {
	ctx, err := jit.NewContext(nativeStack)
	if err != nil {
		panic(err)
	}
	return &native{
		ctx:       ctx,
		store:     jit.NewStore(len(i.code)),
		queue:     compile.NewQueue(func() compile.Machine { return arm64.New() }, 1),
		module:    newModule(i),
		threshold: threshold,
		calls:     map[int]int{},
		failed:    map[int]bool{},
	}
}

// close closes the compile queue, freeing every code it finished but this
// interpreter never drained, and closes the store. The interpreter's native
// calls are synchronous, so no native code is ever suspended when close runs.
func (n *native) close() error {
	var err error
	for _, job := range n.queue.Close() {
		if job.Code != nil {
			err = errors.Join(err, job.Code.Free())
		}
	}
	return errors.Join(err, n.store.Close())
}

// call is the CALL handler's hook for a *types.Function target at addr,
// generated into pushFrame's function-target path. It reports whether it ran
// the call to completion, in which case the caller must not push a frame.
// release and advance mirror the call site's own releaseTarget and ip
// advance, since native completion must apply them exactly as pushFrame
// would.
func (n *native) call(i *Interpreter, addr int, fn *types.Function, release bool, advance int) bool {
	n.drain(i)

	n.store.Enter()
	code := n.store.Code(addr)
	if code == nil {
		n.store.Leave()
		n.count(i, addr, fn)
		return false
	}
	n.run(i, addr, fn, code, release, advance)
	n.store.Leave()
	_ = n.store.Reclaim()
	return true
}

// count tracks calls to addr and submits it for Baseline compilation once
// the threshold is reached, unless it already failed to compile.
func (n *native) count(i *Interpreter, addr int, fn *types.Function) {
	n.calls[addr]++
	if n.calls[addr] < n.threshold || n.failed[addr] {
		return
	}
	if n.queue.Submit(compile.Unit{Address: addr, Function: fn, Module: n.module, Tier: jit.Baseline}) {
		n.pending++
	}
}

// drain collects every unit this interpreter submitted and the queue has
// finished, publishing successful code and marking a failed address so it is
// never resubmitted.
func (n *native) drain(i *Interpreter) {
	if n.pending == 0 {
		return
	}
	for _, job := range n.queue.Drain() {
		n.pending--
		if job.Err != nil {
			n.failed[job.Unit.Address] = true
			n.metric(i, metricCompiles, prof.Label{Key: "tier", Value: job.Unit.Tier.String()}, prof.Label{Key: "outcome", Value: outcome(job.Err)})
			continue
		}
		n.store.Publish(job.Code)
		n.metric(i, metricCompiles, prof.Label{Key: "tier", Value: job.Unit.Tier.String()}, prof.Label{Key: "outcome", Value: "ok"})
	}
}

// run drives code to completion for a call to fn at addr whose frame would
// begin at bp, the same bp pushFrame would have computed.
func (n *native) run(i *Interpreter, addr int, fn *types.Function, code *jit.Code, release bool, advance int) {
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
	trap := jit.Enter(code.Entry(), ctx)
	for {
		switch trap {
		case jit.TrapReturn:
			if release {
				i.release(addr)
			}
			i.sp = bp + returns
			i.fr.ip += advance
			return
		case jit.TrapBridge:
			exit := n.store.Find(ctx.PC()).Exits[ctx.Exit()]
			n.metric(i, metricExits, prof.Label{Key: "kind", Value: exit.Kind.String()})
			switch exit.Kind {
			case jit.ExitSafepoint:
				if cancelled(i) {
					n.deopt(exit.Kind)
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
				n.deopt(exit.Kind)
			}
		default:
			n.deopt(jit.ExitDeopt)
		}
	}
}

// deopt is S2-P6b's materializer, not yet built: it will rebuild every
// native activation as an interpreter frame and continue threaded. Reaching
// it now is not a bug; it names work this phase intentionally leaves undone.
func (n *native) deopt(kind jit.Kind) {
	panic("interp: native exit " + kind.String() + " requires deoptimization, which S2-P6b implements")
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
