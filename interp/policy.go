package interp

import (
	"errors"
	"slices"
	"sync/atomic"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/compile"
	"github.com/siyul-park/minivm/internal/transform"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

// policy decides when to compile, promote, and invalidate native code: HotSpot's
// CompilationPolicy. It compiles and publishes through shared and never runs
// native code.
type policy struct {
	// shared is the runtime policy compiles and publishes through, built by
	// wake and rebound by join: its one owner.
	shared *shared
	// feedback is what running code observed, which compile requests snapshot.
	feedback feedback

	// threshold is the call count that compiles an address's Baseline, and
	// each OSR site's back-edge count: WithThreshold's n, or floor when auto.
	threshold int
	// auto reports WithThreshold(0): an invalidation raises the address's thresholds
	// (backoffs), and the engine stays dormant until the interpreter's heat runs out.
	auto bool
	// backoffs counts each address's invalidations, up to steps: each raises its
	// automatic call and promotion thresholds 4×.
	backoffs []uint8
	// trivial marks, under the automatic policy, each address whose every run
	// executes fewer than short instructions (length): Go runs it threaded,
	// and only its native callers' served calls count it (replay).
	trivial []bool
	// gates closes the CALL hook per address: a failed Baseline or trivial
	// code. It folds both into one load to keep call inlinable.
	gates []bool
	// entries is native-writable: Context.Entries points at it, and every
	// Baseline function prologue increments its own address there, including
	// native-to-native entries.
	entries []int64
	// refutes counts each address's refuted speculations since its last invalidation.
	refutes []int
	// ledgers weighs each address's native work against its exits' cost
	// across Go entries.
	ledgers []jit.Ledger
	// failed records permanent compile failure per address and tier.
	failed [][2]bool
	// built is the profiles each address's published code was compiled from,
	// per tier: an invalidation blocks a recompile only when none has moved since.
	built [][2][]jit.Dep
	// sites indexes every observed OSR site by (address, ip): drain looks a
	// failed OSR unit's site up here to restore its threaded handler.
	sites map[key]*site
}

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
	// code is the published code once a cache lookup has found it; nil
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
// the submit threshold — between its cache lookups (and, while unsubmitted,
// its Queue.Submit retries): rare enough that the lock CodeAt and Submit
// take never runs on the per-iteration path. An entry site observes Runs
// and uses 1.
const interval = 256

// tolerance is the count of refuted speculations under unchanged feedback that
// invalidates native code.
const tolerance = 8

// graduate is the Baseline entry count that tiers an address to Optimized.
const graduate = 1024

// The automatic policy (WithThreshold(0)). One wake — analysis, the first
// compiles, and their garbage — costs ~0.3–0.6 ms of the interpreter's time,
// so a dormant JIT waits for ~30 ms of threaded work before it can cost a
// program 2 %: dormancy frame entries and taken back edges at ~15 ns. A Run
// spends a 32nd of it.
const (
	dormancy = 1 << 21
	// floor is the automatic call and back-edge threshold before any invalidation.
	floor = 256
	// steps caps the invalidations that raise an address's thresholds.
	steps = 4
	// ceiling is the highest automatic threshold: floor raised steps times.
	ceiling = floor << (2 * steps)
	// short is the fewest instructions a run must execute to repay a Go
	// entry: the entry's ~15 ns round trip over the ~0.5 ns native code saves
	// per threaded instruction.
	short = 32
)

// newPolicy returns the dormant policy for WithThreshold(n): the automatic one
// for 0, else a fixed one.
func newPolicy(threshold int) policy {
	if threshold == 0 {
		return policy{auto: true, threshold: floor}
	}
	return policy{threshold: threshold}
}

// wake builds the policy's live state and runtime for i's program and reports whether it
// is live. Under the automatic policy a program whose code is all trivial has
// nothing a Go entry could repay: it stays dormant, unchanged.
func (p *policy) wake(i *Interpreter) bool {
	trivial := make([]bool, len(i.code))
	if p.auto {
		lengths := map[int]int{}
		trivial[0] = p.length(i, 0, lengths) < short
		all := trivial[0]
		for _, c := range i.constants {
			if c.Kind() != types.KindRef {
				continue
			}
			if _, ok := i.heap[c.Ref()].(*types.Function); ok {
				trivial[c.Ref()] = p.length(i, c.Ref(), lengths) < short
				all = all && trivial[c.Ref()]
			}
		}
		if all {
			return false
		}
	}
	p.shared = newShared(i)
	p.backoffs = make([]uint8, len(i.code))
	p.entries = make([]int64, len(i.code))
	p.refutes = make([]int, len(i.code))
	p.ledgers = make([]jit.Ledger, len(i.code))
	p.failed = make([][2]bool, len(i.code))
	p.built = make([][2][]jit.Dep, len(i.code))
	p.sites = map[key]*site{}
	p.feedback.profiles = make([]*transform.Profile, len(i.code))
	p.trivial = trivial
	p.gates = slices.Clone(trivial)
	return true
}

// length returns how many instructions one run of the code at addr executes,
// its constant callees' included, or short when it may run more: a loop, a
// call through anything but a constant function, a tail call, or recursion.
func (p *policy) length(i *Interpreter, addr int, lengths map[int]int) int {
	if count, ok := lengths[addr]; ok {
		return count
	}

	lengths[addr] = short
	code := i.function(addr).Code
	count, callee := 0, -1
	for ip := 0; ip < len(code) && count < short; {
		inst := instr.Instruction(code[ip:])
		switch inst.Opcode() {
		case instr.CALL:
			if callee < 0 {
				return short
			}
			count += p.length(i, callee, lengths)
		case instr.RETURN_CALL:
			return short
		case instr.BR, instr.BR_IF, instr.BR_TABLE:
			for _, target := range instr.Targets(code, ip) {
				if target <= ip {
					return short
				}
			}
		}
		callee = -1
		if inst.Opcode() == instr.CONST_GET {
			if c := i.constants[inst.Operand(0)]; c.Kind() == types.KindRef {
				if _, ok := i.heap[c.Ref()].(*types.Function); ok {
					callee = c.Ref()
				}
			}
		}
		count++
		ip += inst.Width()
	}
	count = min(count, short)
	lengths[addr] = count
	return count
}

// join rebinds the policy to r, a Pool's runtime, and returns the runtime it
// was bound to for the caller to release.
func (p *policy) join(r *shared) *shared {
	own := p.shared
	p.shared = r
	return own
}

// live reports whether the policy has built its state and runtime.
func (p *policy) live() bool {
	return p.shared != nil
}

// runtime is the shared runtime the policy compiles and publishes through.
func (p *policy) runtime() *shared {
	return p.shared
}

// cache is the runtime's code cache.
func (p *policy) cache() *jit.Cache {
	return p.shared.cache
}

// release drops the policy's reference to its runtime.
func (p *policy) release() error {
	return p.shared.release()
}

// counters is the address of the per-address Baseline entry counts native code
// increments.
func (p *policy) counters() uintptr {
	return base(p.entries)
}

// ledger is addr's native-work ledger across Go entries.
func (p *policy) ledger(addr int) *jit.Ledger {
	return &p.ledgers[addr]
}

// admit is a direct CALL's tier decision: it records the callee seen when the
// call owns its target, and returns addr's published code, nil when the call
// runs threaded: the function has captures, the native records are full (full),
// or no code is published yet, which counts the call toward compilation.
func (p *policy) admit(i *Interpreter, addr int, fn *types.Function, release, full bool) *jit.Code {
	if release {
		p.feedback.see(i, transform.Callee{Function: addr})
	}
	if len(fn.Captures) > 0 || full {
		return nil
	}
	p.drain(i)
	code := p.shared.cache.Code(addr)
	if code == nil {
		p.count(i, addr, fn)
	}
	return code
}

// reached counts the callee of a call the interpreter replays: a closure
// callee is recorded and counted, since the threaded closure CALL has no
// native hook; so is trivial code, whose hook is gated.
func (p *policy) reached(i *Interpreter, ref int) {
	if ref <= 0 || ref >= len(i.heap) {
		return
	}
	switch callee := i.heap[ref].(type) {
	case *types.Closure:
		addr := int(callee.Fn)
		p.feedback.see(i, transform.Callee{Function: addr, Closure: true})
		if p.shared.cache.Code(addr) == nil {
			p.count(i, addr, i.function(addr))
		}
	case *types.Function:
		if ref < len(p.trivial) && p.trivial[ref] && p.shared.cache.Code(ref) == nil {
			p.count(i, ref, callee)
		}
	}
}

// count tracks cold calls, pool-wide, and requests Baseline compilation once
// the total across every native sharing addr's code reaches threshold.
func (p *policy) count(i *Interpreter, addr int, fn *types.Function) {
	total := p.shared.calls[addr].Add(1)
	if total < int64(p.threshold)<<p.raise(addr) || p.hasFailed(addr, jit.Baseline) {
		return
	}
	p.shared.queue.Submit(compile.Unit{Address: addr, Function: fn, Module: p.shared.module, Profiles: p.feedback.snapshot(), Tier: jit.Baseline})
}

// reject records exit, a guard that failed, and judges it.
func (p *policy) reject(exit *jit.Exit, entered, code *jit.Code, refute func() bool) bool {
	p.feedback.record(exit)
	return p.judge(entered, code, refute)
}

// judge weighs a refuted speculation in entered, the code that took the
// exit, and reports whether the run's own code is invalidated. refute judges the
// run's own code; a native callee's code that is still published is judged
// against its own feedback and count, and invalidated here.
func (p *policy) judge(entered, code *jit.Code, refute func() bool) bool {
	if entered == code {
		return refute()
	}
	if !entered.Invalidated() && p.refute(entered.Address, entered.Tier) {
		p.invalidate(entered.Address, entered.Tier)
	}
	return false
}

// refute counts one refuted speculation against addr's code at tier and
// reports whether it is invalidated: at once when a profile it was built from moved,
// which a recompile can use, else at the tolerance count.
func (p *policy) refute(addr int, tier jit.Tier) bool {
	p.refutes[addr]++
	return p.moved(addr, tier) || p.refutes[addr] >= tolerance
}

// invalidate unpublishes addr's code at tier and restarts its counters. The tier
// fails only when no profile it was built from moved: a recompile would get
// no new input.
func (p *policy) invalidate(addr int, tier jit.Tier) {
	p.shared.cache.Invalidate(addr)
	if !p.moved(addr, tier) {
		p.markFailed(addr, tier)
	}
	if p.auto && p.backoffs[addr] < steps {
		p.backoffs[addr]++
	}
	p.shared.calls[addr].Store(0)
	p.entries[addr], p.refutes[addr], p.ledgers[addr] = 0, 0, jit.Ledger{}
}

// moved reports whether a profile addr's code at tier was built from changed
// since: a recompile at that tier has new input.
func (p *policy) moved(addr int, tier jit.Tier) bool {
	return p.feedback.stale(p.built[addr][tier-1])
}

// observe wraps every loop header's threaded handler of fn at addr with OSR
// observation, address 0 (module code) included, entering through enter. A
// header a fusion absorbed (code[ip] == nil) is left alone: nothing runs there
// to observe.
//
// Module code is also observed at ip 0, whether or not it has loops: its
// entry site's submit waits for every header site of the same address to
// resolve first (resolved), so it never takes the queue's one address-0
// slot ahead of them.
func (p *policy) observe(i *Interpreter, addr int, fn *types.Function, enter func(*Interpreter, *site) bool) {
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
		threshold, cadence := int64(p.threshold), int64(interval)
		var own []int
		if entry {

			threshold, cadence = max(threshold, 2), 1
			own = headers
			if len(headers) > 0 {
				cadence = interval
			}
		}
		s := &site{address: addr, ip: ip, fn: fn, module: addr == 0, inner: code[ip], entry: entry, headers: own, threshold: threshold, cadence: cadence}
		p.sites[key{addr, ip}] = s
		code[ip] = p.observer(s, enter)
	}
	for _, ip := range headers {
		install(ip, false)
	}

	if addr == 0 && !slices.Contains(headers, 0) && !p.trivial[0] {
		install(0, true)
	}
}

// observer wraps s's own threaded handler. Once resolved (s.code set), its
// steady cost is one field read and a call either into native code (enter)
// or straight through to the handler; a site that enters native code never
// falls through to it itself, since native code always leaves the interpreter
// at the right next instruction, materialized or not.
func (p *policy) observer(s *site, enter func(*Interpreter, *site) bool) func(*Interpreter) {
	return func(i *Interpreter) {
		if s.code != nil && enter(i, s) {
			return
		}
		due := s.onCadence()
		switch {
		case !s.submitted:
			if s.total == nil {
				s.total = p.shared.total(key{s.address, s.ip})
			}
			total := s.total.Add(1)
			if total >= s.threshold && (total-s.threshold)%s.cadence == 0 && p.resolved(s.address, s.headers) {

				p.drain(i)
				u := compile.Unit{Address: s.address, Function: s.fn, Module: p.shared.module, Profiles: p.feedback.snapshot(), Tier: jit.Optimized, IP: s.ip, OSR: true}
				s.submitted = p.shared.queue.Submit(u)
			}
		case due:
			p.drain(i)
			s.code = p.shared.cache.CodeAt(s.address, s.ip)
		}
		s.inner(i)
	}
}

// resolved reports whether every one of addr's header sites at ips has
// published, permanently failed, or disabled: an entry site's submit waits
// for this instead of competing with them for addr's one queue slot. A
// header absent from sites has already failed or disabled (drain and
// invalidateSite delete it on either); one still present is resolved once the
// cache holds its published code.
func (p *policy) resolved(addr int, ips []int) bool {
	for _, ip := range ips {
		if _, ok := p.sites[key{addr, ip}]; ok && p.shared.cache.CodeAt(addr, ip) == nil {
			return false
		}
	}
	return true
}

// tick counts one entry of s and, at its cadence, drains: a callee called only
// from native code would otherwise tier up only at a safepoint. It reports
// whether it drained.
func (p *policy) tick(i *Interpreter, s *site) bool {
	if !s.onCadence() {
		return false
	}
	p.drain(i)
	return true
}

// drain publishes completed jobs, records permanent compile failures, and
// tiers every Baseline candidate whose prologue count reached graduate.
// A candidate leaves once it is no longer Baseline or Optimized has failed.
func (p *policy) drain(i *Interpreter) {
	for _, job := range p.shared.queue.Drain() {
		if job.Err != nil {

			if job.Unit.OSR {
				k := key{job.Unit.Address, job.Unit.IP}
				if s, ok := p.sites[k]; ok {
					p.disable(i, s)
				}
			} else if p.feedback.generation(job.Unit.Address) == job.Unit.Profiles.Of(job.Unit.Address).Generation {

				p.markFailed(job.Unit.Address, job.Unit.Tier)
			}
			outcome := outcomeFailed
			if errors.Is(job.Err, compile.ErrUnsupported) {
				outcome = outcomeUnsupported
			}
			metric(i, metricCompiles, 1, prof.Label{Key: "tier", Value: job.Unit.Tier.String()}, prof.Label{Key: "outcome", Value: outcome})
			continue
		}
		p.shared.cache.Publish(job.Code)
		if !job.Unit.OSR {
			p.built[job.Unit.Address][job.Code.Tier-1] = job.Code.Deps
		} else if s, ok := p.sites[key{job.Unit.Address, job.Unit.IP}]; ok {
			s.built = job.Code.Deps
		}
		if job.Code.Tier == jit.Baseline {
			p.shared.nominate(job.Unit.Address)
		}
		metric(i, metricCompiles, 1, prof.Label{Key: "tier", Value: job.Unit.Tier.String()}, prof.Label{Key: "outcome", Value: outcomeOK})
	}

	p.shared.sweep(func(addr int) bool {
		code := p.shared.cache.Code(addr)
		if code == nil || code.Tier != jit.Baseline || p.hasFailed(addr, jit.Optimized) {
			return false
		}
		if p.entries[addr] >= graduate<<p.raise(addr) {
			p.shared.queue.Submit(compile.Unit{Address: addr, Function: i.function(addr), Module: p.shared.module, Profiles: p.feedback.snapshot(), Tier: jit.Optimized})
		}
		return true
	})
}

// refresh returns s's published code, looked up again once invalidated; nil
// when none is, which clears s.code.
func (p *policy) refresh(s *site) *jit.Code {
	c := s.code
	if c.Invalidated() {
		if c = p.shared.cache.CodeAt(s.address, s.ip); c == nil {
			s.code = nil
			return nil
		}
		s.code = c
	}
	return c
}

// siteLedger is s's native-work ledger, independent of native CALL entries at
// the same address.
func (p *policy) siteLedger(s *site) *jit.Ledger {
	return &s.ledger
}

// refuteSite counts one refuted speculation of s's code and reports whether it
// is invalidated: at once when a profile it was built from moved, else at the
// tolerance count.
func (p *policy) refuteSite(s *site) bool {
	if p.feedback.stale(s.built) {
		return true
	}
	s.refutes++
	return s.refutes >= tolerance
}

// invalidateSite unpublishes s's code. Feedback that moved re-arms s for a
// recompile, bounded by tolerance and, under the automatic policy, spaced by a
// raised threshold; otherwise s is disabled.
func (p *policy) invalidateSite(i *Interpreter, s *site) {
	p.shared.cache.InvalidateAt(s.address, s.ip)
	s.code = nil
	if s.refutes < tolerance && p.feedback.stale(s.built) {

		s.refutes++
		s.submitted = false
		s.ledger = jit.Ledger{}
		if p.auto {
			s.threshold = min(s.threshold<<2, ceiling)
		}
		return
	}
	p.disable(i, s)
}

// disable restores s's threaded handler and forgets s.
func (p *policy) disable(i *Interpreter, s *site) {
	i.code[s.address][s.ip] = s.inner
	delete(p.sites, key{s.address, s.ip})
}

// pending reports whether addr has no code yet but may still compile: a
// served call to it is its callee's warm-up, which costs the caller nothing.
func (p *policy) pending(addr int) bool {
	return addr < len(p.failed) && !p.hasFailed(addr, jit.Baseline) && p.shared.cache.Code(addr) == nil
}

// raise is the shift addr's invalidations apply to its call and promotion
// thresholds: 4× per invalidation under the automatic policy, none when fixed.
func (p *policy) raise(addr int) uint {
	return 2 * uint(p.backoffs[addr])
}

// hasFailed reports whether addr's compile at tier permanently failed.
func (p *policy) hasFailed(addr int, tier jit.Tier) bool {
	return p.failed[addr][tier-1]
}

// markFailed permanently marks addr's compile at tier as failed.
func (p *policy) markFailed(addr int, tier jit.Tier) {
	p.failed[addr][tier-1] = true
	if tier == jit.Baseline {

		p.gates[addr] = true
	}
}

func (s *site) onCadence() bool {
	s.count++
	return s.count%s.cadence == 0
}
