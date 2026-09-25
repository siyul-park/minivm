package interp

import (
	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/compile"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

// key identifies one OSR site by its loop header's (address, ip): how drain
// looks a site up to restore it once its unit's compile permanently fails.
type key struct {
	address, ip int
}

// site is one loop header's OSR observation state, owned by the wrapper
// closure that replaces its threaded handler when the JIT is constructed.
type site struct {
	address, ip int
	fn          *types.Function
	// module reports whether fn completes through OpComplete instead of
	// returning through OpReturn.
	module bool
	// inner is s's own threaded handler, restored in place of the observer
	// once s is known to never resolve.
	inner func(*Interpreter)

	count     int64
	submitted bool
	// code is the published code once a store lookup has found it; nil
	// until then, and again once the site fails.
	code   *jit.Code
	deopts int
	// bridged counts resumed bridges unamortized by real native work since
	// the last one that was (see native.go's resume/amortize); it does not
	// share native.bridged, since a site's own retirement is independent of
	// any CALL entry at the same address.
	bridged int
}

// interval is how many back edges pass — once a site has crossed the submit
// threshold — between its store lookups (and, while unsubmitted, its
// Queue.Submit retries): rare enough that the lock CodeAt and Submit take
// never runs on the per-iteration path.
const interval = 256

// refute counts one deopt against s and reports when it should retire.
func (s *site) refute() bool {
	s.deopts++
	return s.deopts >= refute
}

// observe wraps every loop header's threaded handler of fn at addr with OSR
// observation, address 0 (module code) included. A header a fusion
// absorbed (code[ip] == nil) is left alone: nothing runs there to observe.
func (n *native) observe(i *Interpreter, addr int, fn *types.Function) {
	headers, err := analysis.Headers(fn)
	if err != nil {
		return
	}
	code := i.code[addr]
	for _, ip := range headers {
		if ip < 0 || ip >= len(code) || code[ip] == nil {
			continue
		}
		// translate.go completes address 0 through OpComplete regardless of
		// fn.Typ, which i.module sets to an empty, non-nil FunctionType.
		s := &site{address: addr, ip: ip, fn: fn, module: addr == 0, inner: code[ip]}
		n.sites[key{addr, ip}] = s
		code[ip] = n.observer(s, code, s.inner)
	}
}

// observer wraps inner, the threaded handler at s's own header. Once
// resolved (s.code set), its steady cost is one field read and a call
// either into native code or straight through to inner; a site that enters
// native code never falls through to inner itself, since native code
// always leaves the interpreter at the right next instruction, materialized
// or not.
func (n *native) observer(s *site, code []func(*Interpreter), inner func(*Interpreter)) func(*Interpreter) {
	return func(i *Interpreter) {
		if s.code != nil && n.enter(i, s, code, inner) {
			return
		}
		s.count++
		switch {
		case !s.submitted:
			if s.count >= int64(n.threshold) && (s.count-int64(n.threshold))%interval == 0 {
				// The queue accepts one unit per address; an entry-0 CALL
				// compile of the same address may hold it, undrained,
				// since its own last call.
				n.drain(i)
				u := compile.Unit{Address: s.address, Function: s.fn, Module: n.feedback(s.address), Tier: jit.Optimized, Entry: s.ip, OSR: true}
				s.submitted = n.queue.Submit(u)
			}
		case s.count%interval == 0:
			n.drain(i)
			s.code = n.store.CodeAt(s.address, s.ip)
		}
		inner(i)
	}
}

// enter runs the current frame as s's cached OSR activation. A cache hit
// enters directly; queue publication and native-only promotion are drained
// at OSR submission checks and native safepoints.
func (n *native) enter(i *Interpreter, s *site, code []func(*Interpreter), inner func(*Interpreter)) bool {
	n.store.Enter()
	c := n.store.CodeAt(s.address, s.ip)
	if c == nil {
		n.store.Leave()
		s.code = nil
		return false
	}

	ctx := n.ctx
	ctx.Stack = base(i.stack)
	ctx.Heap = heapBase(i.heap)
	ctx.Globals = base(i.globals)
	ctx.RC = rcBase(i.rc)
	ctx.Natives = n.store.Natives()
	ctx.Entries = entry(n.entries)
	ctx.Top = end(i.stack)
	ctx.FB = base(i.stack[i.fr.bp:])
	ctx.Limit = uint64(min(len(ctx.Records), len(i.frames)-i.fp+1))
	ctx.Budget = budget
	ctx.Depth = 0
	mark := ctx.Budget

	if i.profiler != nil {
		n.metric(i, metricEntries, prof.Label{Key: "tier", Value: c.Tier.String()})
	}

	trap := jit.Enter(c.Entry(), ctx)
	if n.settle(i, s, c, trap, mark) {
		n.store.RetireAt(s.address, s.ip)
		code[s.ip] = inner
		s.code = nil
		delete(n.sites, key{s.address, s.ip})
	}
	n.store.Leave()
	_ = n.store.Reclaim()
	return true
}

// settle materializes OSR exits into the current interpreter frame and any
// suspended outer activations, stopping at TrapReturn or permanent refute.
func (n *native) settle(i *Interpreter, s *site, c *jit.Code, trap jit.Trap, mark int64) bool {
	ctx := n.ctx
	for {
		if trap == jit.TrapReturn {
			n.finish(i, s, c)
			return false
		}

		exit := n.exit(ctx, c)
		if i.profiler != nil {
			n.metric(i, metricExits, prof.Label{Key: "kind", Value: exit.Kind.String()})
		}
		switch exit.Kind {
		case jit.ExitSafepoint:
			if cancelled(i) {
				n.materialize(i, exit)
				return s.refute()
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
			// The limit is checked before serving: running a bridge and then
			// materializing anyway would run Code twice.
			if s.bridged < resume && n.serve(i, exit) {
				s.bridged = n.amortized(mark, ctx.Budget, s.bridged)
				mark = ctx.Budget
				ctx.Heap = heapBase(i.heap)
				ctx.RC = rcBase(i.rc)
				trap = jit.Resume(ctx)
				continue
			}
			n.materialize(i, exit)
			if s.bridged >= resume {
				// The site bridged every native entry with nothing else
				// between: retire rather than pay the round trip forever.
				return true
			}
			return s.refute()
		default:
			n.materialize(i, exit)
			return s.refute()
		}
	}
}

// finish closes an OSR activation after TrapReturn. Ordinary RETURN results are
// already at the frame base; module completion leaves results past locals and
// advances IP past code so threaded dispatch completes.
func (n *native) finish(i *Interpreter, s *site, c *jit.Code) {
	f := i.fr
	if s.module {
		f.ip = len(s.fn.Code)
		i.sp = f.bp + len(s.fn.Slots()) + c.Results
		return
	}
	boxRegisters(i, c, f.bp)
	i.leave(f, f.bp+len(s.fn.Typ.Returns))
}

// materialize rewrites the current frame from exit's own map in place,
// instead of pushing a new one: an OSR activation is entered without a
// call, so there is no caller frame above it in Records to preserve.
func (n *native) materialize(i *Interpreter, exit jit.Exit) {
	ctx := n.ctx
	depth := int(ctx.Depth)
	inner := n.rebuild(i, exit, i.fp-1, i.fr.release)

	i.fp += depth - 1
	i.fr = inner

	ctx.Depth = 0
	ctx.Abandon()
}
