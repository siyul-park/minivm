package interp

import (
	"errors"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/compile"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

// policy decides when to compile, promote, and retire native code: HotSpot's
// CompilationPolicy. It compiles and publishes through shared and never runs
// native code.
type policy struct {
	*shared
	feedback

	// threshold is the call count that compiles an address's Baseline, and
	// each OSR site's back-edge count: WithThreshold's n, or floor when auto.
	threshold int
	// auto reports WithThreshold(0): a retire raises the address's thresholds
	// (backoffs), and the engine stays dormant until the interpreter's heat runs out.
	auto bool
	// backoffs counts each address's retires, up to steps: each raises its
	// automatic call and promotion thresholds 4×.
	backoffs []uint8
	// briefs marks, under the automatic policy, each address whose every run
	// executes fewer than short instructions (length): Go runs it threaded,
	// and only its native callers' served calls count it (replay).
	briefs []bool
	// gates closes the CALL hook per address: a failed Baseline or brief
	// code. It folds both into one load to keep call inlinable.
	gates []bool
	// entries is native-writable: Context.Entries points at it, and every
	// Baseline function prologue increments its own address there, including
	// native-to-native entries.
	entries []int64
	// refutes counts each address's refuted speculations since its last retire.
	refutes []int
	// ledgers weighs each address's native work against its exits' cost
	// across Go entries.
	ledgers []jit.Ledger
	// failed records permanent compile failure per address and tier.
	failed [][2]bool
	// built is the profiles each address's published code was compiled from,
	// per tier: a retire blocks a recompile only when none has moved since.
	built [][2][]jit.Dep
	// sites indexes every observed OSR site by (address, ip): drain looks a
	// failed OSR unit's site up here to restore its threaded handler.
	sites map[key]*site
}

// tolerance is the count of refuted speculations under unchanged feedback that
// retires native code.
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
	// floor is the automatic call and back-edge threshold before any retire.
	floor = 256
	// steps caps the retires that raise an address's thresholds.
	steps = 4
	// ceiling is the highest automatic threshold: floor raised steps times.
	ceiling = floor << (2 * steps)
	// short is the fewest instructions a run must execute to repay a Go
	// entry: the entry's ~15 ns round trip over the ~0.5 ns native code saves
	// per threaded instruction.
	short = 32
)

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

// judge weighs a refuted speculation in entered, the code that took the
// exit, and reports whether the run's own code retires. refute judges the
// run's own code; a native callee's code that is still published is judged
// against its own feedback and count, and retired here.
func (p *policy) judge(entered, code *jit.Code, refute func() bool) bool {
	if entered == code {
		return refute()
	}
	if !entered.Retired() && p.refute(entered.Address, entered.Tier) {
		p.retire(entered.Address, entered.Tier)
	}
	return false
}

// refute counts one refuted speculation against addr's code at tier and
// reports whether it retires: at once when a profile it was built from moved,
// which a recompile can use, else at the tolerance count.
func (p *policy) refute(addr int, tier jit.Tier) bool {
	p.refutes[addr]++
	return p.moved(addr, tier) || p.refutes[addr] >= tolerance
}

// retire unpublishes addr's code at tier and restarts its counters. The tier
// fails only when no profile it was built from moved: a recompile would get
// no new input.
func (p *policy) retire(addr int, tier jit.Tier) {
	p.store.Retire(addr)
	if !p.moved(addr, tier) {
		p.markFailed(addr, tier)
	}
	if p.auto && p.backoffs[addr] < steps {
		p.backoffs[addr]++
	}
	p.calls[addr].Store(0)
	p.entries[addr], p.refutes[addr], p.ledgers[addr] = 0, 0, jit.Ledger{}
}

// moved reports whether a profile addr's code at tier was built from changed
// since: a recompile at that tier has new input.
func (p *policy) moved(addr int, tier jit.Tier) bool {
	return p.stale(p.built[addr][tier-1])
}

// pending reports whether addr has no code yet but may still compile: a
// served call to it is its callee's warm-up, which costs the caller nothing.
func (p *policy) pending(addr int) bool {
	return addr < len(p.failed) && !p.hasFailed(addr, jit.Baseline) && p.store.Code(addr) == nil
}

// count tracks cold calls, pool-wide, and requests Baseline compilation once
// the total across every native sharing addr's code reaches threshold.
func (p *policy) count(i *Interpreter, addr int, fn *types.Function) {
	total := p.calls[addr].Add(1)
	if total < int64(p.threshold)<<p.raise(addr) || p.hasFailed(addr, jit.Baseline) {
		return
	}
	p.queue.Submit(compile.Unit{Address: addr, Function: fn, Module: p.module, Profiles: p.snapshot(), Tier: jit.Baseline})
}

// drain publishes completed jobs, records permanent compile failures, and
// tiers every Baseline candidate whose prologue count reached graduate.
// A candidate leaves once it is no longer Baseline or Optimized has failed.
func (p *policy) drain(i *Interpreter) {
	for _, job := range p.queue.Drain() {
		if job.Err != nil {

			if job.Unit.OSR {
				k := key{job.Unit.Address, job.Unit.IP}
				if s, ok := p.sites[k]; ok {
					i.code[s.address][s.ip] = s.inner
					delete(p.sites, k)
				}
			} else if p.generation(job.Unit.Address) == job.Unit.Profiles.Of(job.Unit.Address).Generation {

				p.markFailed(job.Unit.Address, job.Unit.Tier)
			}
			outcome := outcomeFailed
			if errors.Is(job.Err, compile.ErrUnsupported) {
				outcome = outcomeUnsupported
			}
			metric(i, metricCompiles, 1, prof.Label{Key: "tier", Value: job.Unit.Tier.String()}, prof.Label{Key: "outcome", Value: outcome})
			continue
		}
		p.store.Publish(job.Code)
		if !job.Unit.OSR {
			p.built[job.Unit.Address][job.Code.Tier-1] = job.Code.Deps
		} else if s, ok := p.sites[key{job.Unit.Address, job.Unit.IP}]; ok {
			s.built = job.Code.Deps
		}
		if job.Code.Tier == jit.Baseline {
			p.nominate(job.Unit.Address)
		}
		metric(i, metricCompiles, 1, prof.Label{Key: "tier", Value: job.Unit.Tier.String()}, prof.Label{Key: "outcome", Value: outcomeOK})
	}

	p.sweep(func(addr int) bool {
		code := p.store.Code(addr)
		if code == nil || code.Tier != jit.Baseline || p.hasFailed(addr, jit.Optimized) {
			return false
		}
		if p.entries[addr] >= graduate<<p.raise(addr) {
			p.queue.Submit(compile.Unit{Address: addr, Function: i.function(addr), Module: p.module, Profiles: p.snapshot(), Tier: jit.Optimized})
		}
		return true
	})
}

// raise is the shift addr's retires apply to its call and promotion
// thresholds: 4× per retire under the automatic policy, none when fixed.
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
