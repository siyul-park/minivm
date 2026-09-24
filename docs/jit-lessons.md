# JIT Lessons

Dated history of the first ARM64 JIT implementation, kept as an audit trail for the rebuild.

**Status**: The ARM64 JIT is being rebuilt (2026-09); this page records what the previous implementation (2026-06 → 2026-09) taught. Contracts live in `jit-internals.md`.

This is a dated history/audit document. It owns no current contract, describes no supported behavior, and `MUST NOT` be read as a specification; `jit-internals.md` owns the contract.

## Lessons

### L1 — Only goldens discriminate two paths that guard the same opcodes

**Observed**: `Compiler.Compile` tried the SSA backend first and, when it declined, fell through to the plan pipeline for the same root. Both lowered the same opcodes behind the same guards and reported the same `prof` labels (`frontend`, `reason`, `opcode`), so an interpreter-level test could not tell which pipeline emitted the code — values and metrics agreed bit for bit. Forcing the SSA emitter to decline one opcode failed only the golden; every `interp` subtest stayed green.

**Evidence**: `internal/jit/compiler.go:59-77` (`Compiler.Compile`: `native` first, then the plan frontends); `docs/testing.md` Evidence table, `Golden` row ("exact native instruction stream for a specified input shape").

**Consequence for the rebuild**: One pipeline, no fallback path to hide behind. Per-lowering goldens stay the backend specification; parity tests alone cannot discriminate two implementations of one behavior.

### L2 — All-or-nothing coverage loses kernels wholesale

**Observed**: One opcode the backend could not lower ended native compilation for the entire region it appeared in; a single non-constant array read rejected a whole static plan. Six benchmark kernels ran slower with the JIT enabled than with the pure interpreter, and profiles of `PermutationFlips/default` and `StructTreeWalk/default` carried no native frames at all.

**Evidence**: PR #190 ("perf(interp): bridge unlowerable opcodes and widen static JIT coverage" — Context section states the six-kernel regression and the profile finding); `internal/jit/backend/compiler.go:360` (`if op.Op == ssa.OpExec && !m.Lowers(op.Code) { return nil, false }`, declining one opcode rejects the whole function).

**Consequence for the rebuild**: Every non-lowered `OpExec` becomes a bridge — exit to the interpreter for that one opcode, resume native at the next IP — never a whole-unit refusal. No `Lowers` query gates the frontend.

### L3 — Deopt-and-abandon is a loss, not a fallback

**Observed**: A guard failure originally unwound the whole native invocation with no resume point, no recorded assumption, and no recompile trigger — every guard miss paid the full compile-then-discard cost again. Native code also runs inside a Go-managed stack frame of a fixed literal size, which cannot support a resumable exit because Go's own stack growth can move that frame.

**Evidence**: commit b7c930e ("feat(interp): implement give-up and retirement mechanisms for native entries" — introduced a give-up/resume path in place of raw abandonment); `internal/asm/arm64/abi_arm64.s:8-17` (`TEXT ·invoke(SB), $8272-16`, the coupled Go-stack-frame literal `abi_arm64.s:8` documents against `arm64.StackReserve`).

**Consequence for the rebuild**: Resumable exits run on a separate native stack, not inside a Go stack frame. Deopt is a compiler feature with frame maps; invalidation records the refuted site and requeues instead of discarding.

### L4 — Instrumentation that never turns off is a standing tax

**Observed**: The dispatch loop counted down on every instruction and, on every tick, walked the profile collector, two anchor maps, and a header scan behind a mutex — win or lose, whether or not anything needed instruction-grained cadence. An entry trace could record from a state the function never actually enters; a hot loop was found only if a tick happened to land on its header. Loop bodies also paid a per-iteration flush/reload cost even once native code was installed.

**Evidence**: PR #205 ("perf(interp): tier up from call and back-edge counters" — replaces the countdown with hot-event counters compiled into the handlers; 60-row benchmark, run back to back, median −0.7%, 19 rows improve ≥5%, none regress >5%); PR #160 / issue #155 ("perf(interp): fold loop branch legs into native back-edges" — `Sieve(256)` drops from 4.19 µs to 1.61 µs, default tier, M4 Pro interleaved A/B; scan-loop native entries fall from ~55 to ~1 per run).

**Consequence for the rebuild**: Observation is an entry hook plus a back-edge hook, with state on the branch site, not a map. Native loop code carries one counter store; nothing runs per instruction.

### L5 — Root arbitration is a symptom of compiling partial units

**Observed**: Two roots of one function could both compile natively; whichever held the interpreter's dispatch slot silently starved the other, with no arbitration beyond one hard-coded case. On `SpectralNorm(24,2)` with a coverage gate lifted, the inner loop's recorded trace fell from 19,200 native entries to 8 while an outer function's static plan took 800 — the kernel lost 45%. Separately, a static whole-function entry installed before a hot loop produced its own specialized trace kept ownership of the dispatch slot and never reached the threaded back-edge hook that would let a better loop root take over.

**Evidence**: PR #226, fixes #225 (decline a static plan that would swallow a root already dispatching, or withdraw the installed loser; containment is judged from the loop's own span, not mere coexistence); PR #233, fixes #222 (a static entry now yields its first safepoint interval to the existing loop-warmup cadence so a hot loop root can still install).

**Consequence for the rebuild**: The compiled unit is the whole function. Entries — IP 0, every loop header — are entries into that one unit, so there is nothing to arbitrate between. Bridges (L2) keep the unit whole instead of forcing a second, competing root to exist.

### L6 — Trace capture is a lottery that gets worse as a program runs longer

**Observed**: `RecursiveFib(20)` recorded a full trace and ran entirely native. The same bytecode at `RecursiveFib(35)` recorded a cut trace, retired on its trace-cut rate, and installed nothing at all — deeper recursion did not get slower native code, it got no native code. Separately, a batch tree-evaluation workload needed many small compiled functions instead of one trace over the whole ensemble because nothing links or inlines traces across a call boundary, paying repeated native/interpreter call overhead per tree.

**Evidence**: PR #191 ("perf(interp): lower direct self-recursion in static plans" — Problem section states the full-trace-vs-cut-trace divergence directly; M4 Pro measured after the fix: `RecursiveFib(35)` `default` 56.7 ms vs `threaded` 412.5 ms vs Wazero 44.4 ms, 0 B/op and 0 allocs/op); issue #101 ("Improve JIT throughput for LightGBM-style batch tree evaluation" — improvement area 3: "Better trace linking/inlining... would remove repeated VM/native call boundaries").

**Consequence for the rebuild**: No tracer. One bytecode-to-SSA translator feeds every tier; feedback vectors and a tiering compiler replace recording, so a deeper or longer-running invocation of the same function reaches the same compiled unit instead of a differently-cut trace of it.

### L7 — Register allocation must be control-flow-aware from the first line

**Observed**: The original allocator judged a spill from one linear index per value (its highest-referencing instruction) with no notion of a branch; soundness came from two all-or-nothing gates layered on top instead — any backward branch anywhere disabled spilling for the whole build, and any container store anywhere disabled it for that plan. A function containing a loop therefore could not spill at all, however far a value sat from the loop, and the gate hid the cost for months. The allocator also has no call-clobber model: a callee may clobber any allocatable register, and the float bank has no spill support at all.

**Evidence**: commit f617023, part of PR #224 ("perf(asm): allocate registers over a real control-flow graph" — replaces the linear-index heuristic and both gates with basic blocks, computed dominance, and a hazard check for loop-carried self-redefinition and self-recursive calls; geomean across eighteen kernels against the pre-change baseline: +0.70%, worst case `SortStress` +3.85%); `internal/asm/rewriter.go:364-369` (documents the missing call-clobber model and the float bank's lack of spill support).

**Consequence for the rebuild**: Linear scan over live intervals computed from per-block liveness, from the start. Calls clobber every allocatable register in the model; both register banks support spilling. No coexistence-only or linear-index heuristics stand in for dominance.

### L8 — Ownership belongs in the IR, per stack entry, not as heuristics at use sites

**Observed**: A single opcode (`DUP`) could put one heap value in two stack slots under one reference count, so any lowering touching one of those slots had to re-derive who owned it. That re-derivation leaked into individual opcode lowerings as bespoke heuristics instead of living in one place.

**Evidence**: PR #151 ("perf(interp): defer ref operand ownership to backing slots in the ARM64 JIT"); commit bebe7e4 ("feat(jit): lower reference retain and release in the SSA emitter").

**Consequence for the rebuild**: `OpRetain`/`OpRelease` and an `Operand.Owned` bit stay in the kept SSA IR (`internal/ssa/operation.go:169-170`, `internal/ssa/verify.go:191,205`) as the one owner of ownership state; a lowering reads the bit instead of re-deriving it.

### L9 — Only the callee can clear its own locals

**Observed**: Two different native call paths each cleared the wrong set of frame slots on entry — the caller cannot know which locals a callee's own prologue still needs zeroed, only the callee can.

**Evidence**: PR #197 ("fix(interp): clear the callee frame's non-parameter locals").

**Consequence for the rebuild**: Every native function's own prologue clears its non-parameter locals. Exactly one owner; no caller-side clearing path to keep in sync with it.

### L10 — A null reference is not always the boxed-null bit pattern

**Observed**: A zero-filled struct field reads as a boxed zero value, not as the sentinel the null-test lowering expected, so the JIT's null check diverged from the threaded interpreter's on exactly that case.

**Evidence**: issue #228 ("perf(jit): a struct-typed slot that can hold null makes the JIT diverge from threaded"); PR #230 ("fix(jit): test ref.is_null on the ref payload, not the whole boxed word").

**Consequence for the rebuild**: The null test masks the payload's low bits; it does not compare the whole boxed word. `value-representation.md` records the rule, and the materializer and every ref guard use it.

### L11 — Compile input must never read the live heap

**Observed**: Trace capture steps a speculative clone of the interpreter, but a host object holds a reference to the live interpreter it was built with and boxes field access into that interpreter's heap — so stepping a host-object read during capture mutated the live heap the clone was supposed to leave untouched, and could exhaust it.

**Evidence**: PR #201 ("fix(interp): refuse to record a host object's field access" — Evidence section reproduces `heap exhausted` from the live-heap write); `internal/jit/input.go:16-29` (`Input` doc comment: "Nothing reachable through an Input is storage the interpreter keeps mutating... A compile therefore reads no cell another goroutine can change underneath it").

**Consequence for the rebuild**: A compile unit holds `*types.Function`, constants, declared types, resolved constant objects, and a feedback snapshot — never the live heap. Any access that could touch live interpreter state is refused at capture time, not filtered after the fact.

### L12 — Retirement needs its own liveness signal; exit rate alone is not one

**Observed**: The counter tracking native exits outlived the retirement decision it should have driven, so a correct-but-slower native entry that kept giving up on the same guard never actually retired — it just kept re-entering and re-exiting.

**Evidence**: PR #232, fixes #223 ("perf(interp): retirement counts give-ups, so a correct-but-slower native entry never retires"); commit 18e6495 ("refactor(jit): give the retirement verdict its own package"); commit dc71e41 ("refactor(interp): move tier-up and retirement policy out of the compiler").

**Consequence for the rebuild**: Compiled code owns an explicit lifecycle state (live → not-entrant → dead), and the deopt count per exit site — not the raw exit rate — drives invalidation.

### L13 — A framed ABI's literal constants are load-bearing and coupled

**Observed**: A single stack-frame-size literal (`$8272`) in hand-written ARM64 assembly had to equal a Go-side `StackReserve` computation, checked only by one dedicated test; the coupling was implicit and easy to break by editing either side alone.

**Evidence**: `internal/asm/arm64/abi_arm64.s:8,17` (`// calls: the $8192 literal below must equal arm64.StackReserve(...)`; `TEXT ·invoke(SB), $8272-16`); `internal/asm/arm64/stack.go:7,21-30` (`SpillBytes`, `StackReserve`, `FrameSize` — "hand-written TEXT and ADD literals must match StackReserve and FrameSize"); `internal/asm/rewriter.go:81-82` (ties the trampoline's stack-reserve literal to `TestARM64_StackReserve`).

**Consequence for the rebuild**: The native stack is a Go-allocated region of one configured size, checked by a prologue frame-limit test that deopts on overflow — no hand-written literal duplicated in assembly.

### L14 — A native call boundary can silently corrupt a caller's pinned state

**Observed**: A call from native code into native code must save and restore the caller's spill-base register around the call, because the callee's own prologue repoints that register at its own frame; skipping the save/restore leaves the caller reading through the wrong frame after the callee returns.

**Evidence**: `internal/jit/arm64/call.go:151-165` (documents and implements the save/restore: "X26 is this activation's spill base... Save and restore X26 around the [call] so X26 still names the right one on return"); `internal/jit/arm64/machine_test.go:2288-2334` (`TestARM64_CallSpillsAcrossBLR`, golden-pins the save-before/reload-after sequence around the call instruction).

**Consequence for the rebuild**: Every prologue reloads pinned registers from the runtime context rather than trusting a caller to have preserved them; nothing addresses closure state through a register that a call can silently repoint.

### L15 — Only test-first goldens and reachability profiles count as evidence

**Observed**: Mutation-based test validation was tried as a substitute for an observed red phase. It cost more than it returned: an agent run killed mid-mutation twice left a live panic injection in the tree, found only by a later reader rather than by the run that introduced it — a silent semantic change indistinguishable from a real bug.

**Evidence**: commit a82c838 ("docs: ban mutation probing outright"); `docs/testing.md` Evidence section ("Coverage measures reachability, not quality... the agent `MUST` use coverage to prove reachability and `MUST NOT` change production behavior to manufacture a failure").

**Consequence for the rebuild**: A golden or public-behavior test is written first and its red observed directly; where no red is available, reachability is proven with a coverage profile over the exercising package instead of a mutation probe.

### L16 — Extraction is redesign; names default to one word

**Observed**: Symbols moved between packages during earlier restructuring carried over their old shape and multi-word names instead of being re-cut for the new package's responsibility.

**Evidence**: commit 668d2d4 ("docs(coding-patterns): enhance dependency direction and physical cohesion guidelines"); commit 281e4e6 ("docs(coding-patterns): refine design principles and clarify structural rules"); `docs/coding-patterns.md:25` ("When extracting shared functionality, code `MUST NOT` merely be moved into a lower layer. The responsibility and its dependencies `MUST` be generalized..."); `docs/coding-patterns.md:63` ("One word is the rule, not a preference...").

**Consequence for the rebuild**: Every move brief states explicitly: re-cut symbol boundaries for the destination package, delete every symbol without a caller, minimize the exported surface, one-word names by default, and never repeat the package name in a symbol it belongs to.

### L17 — Measurement discipline is part of the result, not a footnote

**Observed**: This machine's benchmark numbers drift meaningfully run to run from thermal/power state alone, so a single before/after pair can show an improvement or a regression that is really just which side happened to run in the faster position.

**Evidence**: PR #160 ("Sieve(256) drops from 4.19us to 1.61us... on M4 Pro interleaved A/B"); commit f617023 ("NQueens/jit... back: 129.8us against the pre-soundness baseline's 129.0us, +0.6%, with that baseline in the faster run position"); `docs/benchmarks.md` Statistics note ("canonical rows use `-benchtime=300ms -count=3` and report the median").

**Consequence for the rebuild**: `make benchmark-pr` numbers are recorded, not gated, during the rebuild. Any perf claim compares interleaved A/B runs, never two runs taken minutes apart with the tree changing in between.

### L18 — Memory-carried runtime counters serialize native recursion

**Observed**: `Context.Depth` and `Context.FB`, loaded and stored in every prologue, epilogue, and call site, put every activation of a recursion on one store→load dependency chain; fib(20) was latency-bound, not issue-bound.

**Evidence**: `S2-P15b`; fib(20)/jit 56.0µs → 42.7µs with Depth in X27 and FB in X25 (store-only mirrors), → 36.1µs with results in X0/X1; interleaved A/B, `-benchtime=300ms -count=3`.

**Consequence for the rebuild**: pin per-activation state in registers the call convention preserves, and store it to memory only for out-of-band readers (exits, materializer). Keep the body at offset 0 and load the pinned registers in a separate Go entry stub.

### L19 — A single code layout is not a measurement

**Observed**: shifting all native code by 4 bytes moved fib by 4–10% and Sieve by up to 18%. A change's single-layout A/B read flat (R, fib +2%) or large (R, MatMul −15%) where the layout-averaged result was −9% and flat.

**Evidence**: `S2-P15c`; `~/.claude/plans/jit-rewrite/evidence/p15c/R/` (single-layout vs `layout-*.txt`, 4 entry shifts × 2 interleaved rounds).

**Consequence for the rebuild**: a perf decision averages over entry shifts (+0/4/8/12 bytes); a single-layout gap under ~5% is noise, and a single-layout win on one kernel is not evidence.

## Related

- `jit-internals.md` — current JIT contract and status
- `testing.md` — evidence layers, mutation-probing prohibition
- `coding-patterns.md` — extraction, naming, dependency direction
- `refactoring.md` — structural review applied to the rebuild
- `benchmarks.md` — canonical measurement protocol
