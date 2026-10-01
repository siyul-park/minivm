package interp

import (
	"slices"
	"sync/atomic"

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
	code   *jit.Code
	deopts int
	// bridge carries this site's unamortized bridge work independently of
	// native CALL entries at the same address.
	bridge bridge
}

// interval is how many back edges pass — once a header site has crossed
// the submit threshold — between its store lookups (and, while unsubmitted,
// its Queue.Submit retries): rare enough that the lock CodeAt and Submit
// take never runs on the per-iteration path. An entry site observes Runs
// and uses 1.
const interval = 256

// refute counts one deopt against s and reports when it should retire.
func (s *site) refute() bool {
	s.deopts++
	return s.deopts >= refute
}

// observe wraps every loop header's threaded handler of fn at addr with OSR
// observation, address 0 (module code) included. A header a fusion
// absorbed (code[ip] == nil) is left alone: nothing runs there to observe.
//
// Module code is also observed at ip 0, whether or not it has loops: its
// entry site's submit waits for every header site of the same address to
// resolve first (resolved), so it never takes the queue's one address-0
// slot ahead of them.
func (n *native) observe(i *Interpreter, addr int, fn *types.Function) {
	headers, err := analysis.Headers(fn)
	if err != nil {
		return
	}
	code := i.code[addr]
	// install adds an observer at ip, entry only for module code's own ip
	// 0. translate.go completes address 0 through OpComplete regardless of
	// fn.Typ, which i.module sets to an empty, non-nil FunctionType.
	install := func(ip int, entry bool) {
		if ip < 0 || ip >= len(code) || code[ip] == nil {
			return
		}
		threshold, cadence := int64(n.threshold), int64(interval)
		var own []int
		if entry {
			// A module run once never compiles ahead of its callees. Loop-free
			// code has no safepoint to drain at, and Runs are rare: cadence 1.
			// Code with loops polls its headers (resolved) at their cadence.
			threshold, cadence = max(threshold, 2), 1
			own = headers
			if len(headers) > 0 {
				cadence = interval
			}
		}
		s := &site{address: addr, ip: ip, fn: fn, module: addr == 0, inner: code[ip], entry: entry, headers: own, threshold: threshold, cadence: cadence}
		n.sites[key{addr, ip}] = s
		code[ip] = n.observer(s, code, s.inner)
	}
	for _, ip := range headers {
		install(ip, false)
	}
	// A header already at ip 0 (the whole module is one loop) is observed
	// above, as a header: entry semantics do not apply twice.
	if addr == 0 && !slices.Contains(headers, 0) {
		install(0, true)
	}
}

// resolved reports whether every one of addr's header sites at ips has
// published, permanently failed, or disabled: an entry site's submit waits
// for this instead of competing with them for addr's one queue slot. A
// header absent from sites has already failed or disabled (drain and enter
// delete it on either); one still present is resolved once the store holds
// its published code.
func (n *native) resolved(addr int, ips []int) bool {
	for _, ip := range ips {
		if _, ok := n.sites[key{addr, ip}]; ok && n.store.CodeAt(addr, ip) == nil {
			return false
		}
	}
	return true
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
			if s.total == nil {
				s.total = n.total(key{s.address, s.ip})
			}
			total := s.total.Add(1)
			if total >= s.threshold && (total-s.threshold)%s.cadence == 0 && n.resolved(s.address, s.headers) {
				// The queue accepts one unit per address; an entry-0 CALL
				// compile of the same address may hold it, undrained,
				// since its own last call.
				n.drain(i)
				u := compile.Unit{Address: s.address, Function: s.fn, Module: n.feedback(s.address), Tier: jit.Optimized, Entry: s.ip, OSR: true}
				s.submitted = n.queue.Submit(u)
			}
		case s.count%s.cadence == 0:
			n.drain(i)
			s.code = n.store.CodeAt(s.address, s.ip)
		}
		inner(i)
	}
}

// enter runs the current frame as s's cached OSR activation. It drains at
// s's lookup cadence, counting entries: a callee called only from native
// code would otherwise tier up only at a safepoint. Loop-free module code
// reaches no safepoint, so an entry site with no headers declines a
// cancelled Run, leaving threaded code to report it; an entry site whose
// module has loops reaches one at each header, so settle already reports
// cancellation there. Native code reads a word per capture without a
// bounds check, so a frame without its captures declines too.
func (n *native) enter(i *Interpreter, s *site, code []func(*Interpreter), inner func(*Interpreter)) bool {
	if s.entry && len(s.headers) == 0 && cancelled(i) || len(i.fr.upvals) < len(s.fn.Captures) || n.depth >= uint64(len(n.ctx.Records)) {
		return false
	}
	if s.count++; s.count%s.cadence == 0 {
		n.drain(i)
	}
	n.store.Enter()
	c := s.code
	if c.Retired() {
		if c = n.store.CodeAt(s.address, s.ip); c == nil {
			n.store.Leave()
			s.code = nil
			return false
		}
		s.code = c
	}

	ctx := n.ctx
	ctx.Heap = heapBase(i.heap)
	ctx.Globals = base(i.globals)
	ctx.RC = rcBase(i.rc)
	ctx.Natives = n.store.Natives()
	ctx.Entries = entry(n.entries)
	ctx.Top = end(i.stack)
	ctx.FB = base(i.stack[i.fr.bp:])
	ctx.Upvals = base(i.fr.upvals)
	ctx.Limit = uint64(min(len(ctx.Records), int(n.depth)+len(i.frames)-i.fp+1))
	ctx.Budget = budget
	ctx.Depth = n.depth

	if i.profiler != nil {
		n.metric(i, metricEntries, prof.Label{Key: "tier", Value: c.Tier.String()})
	}

	ok, retire := true, false
	var fault any
	if trap := jit.Enter(c.Entry(), ctx); trap != jit.TrapReturn {
		// An OSR activation is entered without a call, so there is no caller
		// frame above it in Records to preserve: rebuild rewrites the current
		// frame in place instead of pushing a new one.
		start := i.fp - 1
		f := i.fr
		ok, retire, fault = n.settle(i, c, trap, &s.bridge, start,
			func(exit jit.Exit) { n.rebuild(i, exit, start, f.ref, f.release) },
			s.refute,
		)
	}
	if ok {
		n.finish(i, s, c)
	}
	if retire {
		n.store.RetireAt(s.address, s.ip)
		code[s.ip] = inner
		s.code = nil
		delete(n.sites, key{s.address, s.ip})
	}
	n.store.Leave()
	if fault != nil {
		panic(fault)
	}
	_ = n.store.Reclaim()
	return true
}

// finish closes an OSR activation after TrapReturn. Ordinary RETURN results are
// already at the frame base; module completion leaves results past locals and
// advances IP past code so threaded dispatch completes.
func (n *native) finish(i *Interpreter, s *site, c *jit.Code) {
	f := i.fr
	if s.module {
		f.ip = len(s.fn.Code)
		i.sp = f.bp + slots(s.fn) + c.Results
		return
	}
	boxRegisters(i, c, f.bp)
	i.leave(f, f.bp+len(s.fn.Typ.Returns))
}
