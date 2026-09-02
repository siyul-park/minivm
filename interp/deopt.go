package interp

import (
	"slices"
	"sync/atomic"

	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/journal"
	"github.com/siyul-park/minivm/prof"
)

// This file holds the native dispatch wrappers threaded code hands control
// to and the path back into the interpreter after a trap: the per-anchor
// dispatch closure (cycle), the single-opcode bridge back into threaded
// dispatch, frame teardown on a clean native return (popFrame, complete),
// resuming the shadowed threaded handler after a give-up (resumeShadowed),
// and rebuilding VM frames from the native journal after a trap (deopt,
// unpack). interp/jit.go drives compilation and installs the native entries
// this file dispatches; interp/tier.go owns tier-up and retirement policy.

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
		if wd.probe == probeShadow {
			done := wd.shadowReach()
			i.resumeShadowed(root)
			if done {
				if isFunction && root.Addr < len(i.natives) {
					atomic.StorePointer(&i.natives[root.Addr], entry.Callable.Addr())
				}
				if wd.probeRetire {
					i.retire(root, isFunction)
				}
			}
			return
		}

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
			budget := uint64(loopBudget)
			if entry.Frontend == prof.FrontendStatic && entry.Kind != jit.EntryLoop {
				budget = loopWarmup
			}
			i.journal[journal.CellBudget] = budget
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
