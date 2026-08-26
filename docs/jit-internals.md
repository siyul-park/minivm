# JIT Internals

Contracts for the ARM64 JIT in `interp/` and its interaction with `internal/asm/`.

## When to Read

Use this document before changing `interp/jit*.go`, `interp/trace.go`, `asm` callable ABI code, trace recording, lowering, deoptimization, loop safepoints, or JIT installation.

For user-facing performance results, see `docs/benchmarks.md`. For sampling and hotness thresholds, see `docs/profile.md`.

## Source of Truth

| Concern | File or doc |
|---|---|
| opcode semantics | `docs/instruction-set.md`, `instr/type.go` |
| threaded behavior | `interp/threaded.go` |
| trace recording | `interp/trace.go` |
| architecture-neutral compiler | `interp/jit.go`, `interp/jit_plan.go` |
| ARM64 lowering | `interp/jit_arm64.go` |
| callable ABI | `internal/asm/` |
| value layout | `docs/value-representation.md` |
| heap ownership | `docs/memory-model.md` |
| ticks and thresholds | `docs/profile.md` |

## Summary

minivm always compiles bytecode to threaded closures first. The JIT is a lazy ARM64 plan backend layered on top of that portable threaded runtime.

```text
program.Program
  -> threader -> []func(*Interpreter)   always available
  -> tracer           -> trace snapshots        lazy runtime recording
  -> compiler         -> *module                lazy ARM64 backend
```

The threaded interpreter is the source of correctness. Native code is an optimization and must always have a correct threaded fallback.

Default rules:

- preserve threaded and JIT semantic parity
- normalize every frontend into one small plan before architecture lowering
- keep fallback behavior explicit
- keep architecture-specific code isolated
- use short, standard names
- if two designs behave the same, choose the simpler one

## Execution Model

The dispatch table is:

```go
i.code[addr][ip]
```

Where:

- `addr` is the function slot
- `ip` is the bytecode offset
- each entry is a threaded closure or a wrapper around a native callable

A hot JIT attempt records a runtime trace from the current interpreter state. The ARM64 backend then emits native callables for usable roots.

| Root | Meaning | Install point |
|---|---|---|
| module entry | top-level program start | `i.code[0][0]` |
| function entry | function start | `i.code[addr][0]` |
| loop header | hot backward-branch target | `i.code[addr][header]` |

Rejected traces emit nothing. The threaded closure remains installed.

Function entry callables tear down their frame on return. Module entry callables preserve the top-level frame and complete by advancing to the end of program code. Loop callables re-enter a live frame and must not unwind it.

## Solo and Pool JIT

Solo interpreters own a private `tracer` and lazily own a private `compiler`
and `asm.Buffer`.

`Pool` is the only public shared-JIT seam. It owns one private `cache` and one
private `tracer`; borrowed interpreters attach to that state and keep their
runtime stacks, heaps, dispatch tables, and installed wrappers local.

The shared cache provides:

- trigger counts are atomic
- one winning interpreter compiles
- compiled modules publish immutable `asm.Callable`s
- each interpreter installs those callables into its own dispatch table at a safepoint

The cache claims a build's root and trigger together. Each function owns a
coalescing queue of exact anchors rather than one pending slot: distinct loop
roots are retained, duplicate requests are discarded, and a side exit arriving
behind an active hot build remains queued because it represents newer trace work.
Side-exit requests take priority over queued hot roots.
Publication finishes only the claimed build and leaves queued requests cold for
the next winner.

The published native code is shared. The dispatch table remains interpreter-local.

## Compiler

`compiler` is private to `interp` and lives in `jit.go`. The interpreter calls only `compiler.Compile(i, root)` and receives an opaque `module`; it does not select or inspect a compilation strategy. A frontend may discover several recorded roots, but compilation selects only the requested anchor so later loop attempts do not re-emit already-installed entries.

The compiler builds one read-only `compileInput`, then runs two ordered frontends:

1. `staticPlan` constructs complete plans from verified bytecode and dataflow: one entry plan when no entry is installed, plus one `entryLoop` plan per loop header (`headers`).
2. `tracePlan` constructs plans from immutable runtime trace snapshots.
3. If neither frontend produces a lowerable plan, threaded execution remains installed.

The order depends on the requested root. An entry root tries the static frontend first: it plans the whole function deterministically and covers opcodes no trace can record. A loop root tries the trace frontend first, because a recorded loop specializes its body to the path actually taken - folded legs, a hoisted container - and the static loop plan is the fallback for a loop no trace could record.

Both frontends return the same private `plan` model: ABI kind, a root block ID, flat blocks, entry states, ordinary steps, explicit edges, and spill policy. Every internal edge carries a block ID; unresolved edges retain only their threaded fallback anchor. Build, link, validation, accounting, and publication are centralized in the compiler.

## Static Frontend

The static frontend analyzes basic blocks with one forward fixpoint that tracks stack kind, constant-ref provenance, direct-call targets, declared struct types, and known i32 constants. A `STRUCT_GET` whose container carries a declared struct type (or references a known heap struct) and whose field index is a known in-bounds constant resolves its result kind statically; the planner synthesizes `step.seen` as the zero boxed value of that kind, and the lowering's runtime itab, type, and per-field kind guards keep it sound. It emits plan blocks with explicit entry state, decoded operands, and block-ID edges. An opcode `bridgeable` names (see Bridge) ends its block on that opcode instead of rejecting the whole function, provided `applyStep` can still model its stack effect; any other opcode the backend cannot lower, or one whose effect cannot be modeled, rejects the whole static plan for the function.

A container's element kind resolves from the live heap cell when its identity is known, and otherwise from its declared array type (`types.ArrayType.ElemKind`) reached through the local, param, upvalue, or `REF_CAST` slot that carries it. Both are hints the runtime tag, itab, and bounds guards verify before any access, so a slot declared as an array that currently holds null or a differently shaped array deopts instead of being read. The declared type answers only in a call-free plan: the general array path combined with a native call still corrupts native state, so a calling function keeps resolving only from a constant container.

Static plans compute `noSpill` exactly as trace plans do. A store path must never spill, and before declared array types let array code plan statically this was unreachable, because such a function had no static plan at all.

Every root of a function is planned from one shared block list, but each `entryLoop` plan keeps only the blocks its own root reaches (`prune`), renumbered densely. The backend emits every block a plan holds, so an unpruned header would re-emit the whole function once per header - O(headers) redundant code size, register pressure, and branch range. Reachability follows `term.edges` and their tail continuations, plus one edge no terminator names: a `terminateBridge` block resumes at the block planned immediately after it, because resumption is a fresh external entry rather than a branch. A block list that does not satisfy that layout skips the root instead of emitting a plan whose resume target is missing.

`noSpill` and the loop-carried registers are recomputed per pruned plan rather than inherited. A block the header cannot reach is never emitted, so its stores must not force the plan off the spill frame and its bridges must not strip the plan's carried registers.

Top-level modules containing `CALL` or `RETURN_CALL` are rejected because module entry does not implement the framed native-call ABI. Primitive typed-array constants remain ownership-neutral markers until `ARRAY_GET`; native code reloads the current heap cell, guards its shape and index, and retains the marker only on a cold fallback.

## Trace Recording

Trace recording lives in `interp/trace.go`.

Recording clones the interpreter, starts the clone at the requested `(addr, ip)`, and executes threaded closures until it reaches return, loop back-edge, branch exit, unsupported operation, trace limit, or abort condition. A backward edge to a different header cuts the linear prefix so that header can become a standalone loop trace with a native safepoint budget. Reaching the trace limit records a partial trace with a resumable cut instead of aborting. Native execution deoptimizes at that cut; when the exit becomes hot, the existing side-exit machinery records and compiles the next bounded continuation.

A recursive `CALL` from a non-entry loop trace is a hard trace boundary. The recorder cannot model the recursive callee with `skipCall`, because that path supplies placeholder returns and does not reproduce the callee's heap mutations. The recorder therefore marks the `CALL` itself as a resumable cut; native execution falls back at that instruction, and a later hot continuation is recorded from the real frame and heap state. Function-entry traces retain native self-call lowering because that call is part of their native frame contract.

The live interpreter is not mutated while recording.

Each recorded step stores the data needed for speculative lowering:

- opcode
- function and IP
- inline depth
- observed call target
- observed callee address
- observed guard values
- observed heap shape
- branch target and taken state
- partial-trace resume boundary
- selected heap values for read-only fast paths

The tracer aborts before host calls and allocation. It records boxed-array writes, ref-field struct writes, and bulk mutations (`ARRAY_FILL`, `ARRAY_COPY`, `ARRAY_APPEND`, `MAP_SET`) only as terminal fallback boundaries. A primitive typed-array write or a scalar struct-field write may remain inside the trace when it occurs in the anchor frame before any inlined call. Capture clones every overlapping visible range of aliased primitive typed arrays into one replacement backing store, preserving slice offsets while leaving the live heap unchanged. Boxed arrays and structs are copied before their terminal mutation. The clone also owns mutable dispatch metadata and suppresses external finalizers, so speculative reference reclamation cannot alter live functions, trace trees, or host resources.

Every recorded `trace` has one status: `fallback`, `loop`, `returned`,
`completed`, `partial`, or `aborted`. `fallback` is an explicit usable linear
prefix that ends in threaded fallback. The trace frontend maps usable statuses
to plan terminators and excludes aborted fragments from learned continuations.
A loop anchor accepts a `loop` root or a `returned` straight-line root: a body
that hits a terminal boundary before its back-edge compiles as a per-entry
prefix that deopts at the boundary and re-enters at the header next iteration.
When an observed block runs out of steps, lowering decides completion from that
block's terminator, never from the root trace. This prevents an unsupported side
fragment from being mistaken for normal completion.

`tracer.publish` assigns the status and returns the capture result. It publishes
accepted and partial roots under the tree lock. `fallback`, `loop`, `returned`,
and `completed` map to `CaptureOutcomePublished`; `partial` maps to
`CaptureOutcomePartial`; and `aborted` maps to `CaptureOutcomeRejected` without
publishing the trace.

`tracer.capture` serializes recording and returns an already-published root when
one exists, so sampling and compilation cannot record the same entry
concurrently. A tracer is bound to one `program.Program`; `New` isolates it with
a fresh tracer instead of reusing one bound to another program.

`tracer.headers` (the static loop-header scan) uses `instr.Targets(code, ip)` rather than switching on `BR`/`BR_IF` directly, so a loop formed only through a backward `BR_TABLE` case target is recognized as a header too.

Threaded back-edge handlers report loop hotness directly; forward branches carry no hotness state. The hotness policy and thresholds are defined in `docs/profile.md`.

## Trace Snapshots

A pool shares one private `tracer`. Tree mutations are locked. `rootAt` returns a stable snapshot containing immutable trace pointers plus copied branch and hit containers.

`tracePlan` converts that snapshot into flat plan blocks. It excludes aborted fragments and loop-kind legs (a loop-kind leg is a loop root of its own: anchored at this header it is the root itself and its edge already wires to the root block, anchored elsewhere it is a different loop with its own native entry), sorts continuation roots deterministically, connects internal paths by block ID, and derives spill policy from the final plan rather than exposing the trace tree to lowering.

In a loop plan, a partial leg whose cut lands on the plan's own header (same function, depth 0) folds into the loop back-edge: `split` emits a real branch terminator instead of a fallback, `wire` resolves it onto the root block, and lowering takes the committing-flush native back-edge, so an in-loop branch that rejoins the header no longer exits native code (issue #155). A cut record that directly follows an explicit branch with the same target ends the split without materializing a spurious block, leaving the branch edge for `wire` to resolve. Cuts inside an inlined frame or to any other location keep the deopt fallback.

## Backend

`jit.go` is architecture-neutral: `compiler` picks the arch, builds the assembler, and hands both to a build-tagged `machine` through the `machine.Lower(a *asm.Assembler, input *compileInput, p plan, nativeLoop bool) ([]exitDescriptor, bool)` seam. All lowering state — `lowering`, the symbolic `value` stack, inlined `activation`s, deferred `work`, and queued `sideExit`s — lives on the machine's side of that seam; unsupported architectures' `newMachine` stub is never called because `newCompiler` never constructs a compiler there.

`jit_arm64.go` owns all ARM64 lowering: orchestration, the single opcode dispatcher, control flow, numeric operations, calls, frames, deoptimization, heap access, and reference ownership.

Every plan block passes through one `emitBlock` path and every edge carries an explicit block ID or an unresolved threaded-fallback anchor. Bytecode locations describe source positions only; block IDs preserve distinct inlined contexts even when they share the same `(function, IP)`. A state-backed block reloads VM homes, while a profiled successor may continue with the current symbolic state.

Caller continuations are ordinary blocks in the same flat block pool. A cold edge carries the continuation block IDs that must run after an inlined callee returns. A deferred edge receives a label and a canonical symbolic snapshot (register-free values, reset locals); `label` shares the label of a previously scheduled continuation only when block, tail, and canonical snapshot are identical, and its ledger keeps consumed work items so folded legs that branch into one another (a loop nest) converge instead of exhausting the continuation limit. States are never merged by bytecode anchor alone.

## Trace ABI

Native callables use an AAPCS64-shaped entry.

`Callable.Call(ctx unsafe.Pointer)` passes:

```text
&i.journal[0] in X0
```

Native code loads VM state from the journal into pinned scratch registers.

| Name | ARM64 | Purpose |
|---|---|---|
| `scratchStack` | X10 | `&i.stack[0]` |
| `scratchGlobals` | X11 | `&i.globals[0]` |
| `scratchBP` | X12 | current frame base |
| `scratchSP` | X13 | current stack pointer |
| `scratchCtrl` | X14 | journal pointer |

The context stays an `unsafe.Pointer` through the Go call boundary so Go can
relocate a stack-backed context when the trampoline grows the goroutine stack.
Converting it to `uintptr` before that stack split would leave native code with
a stale address.

The Go trampoline preserves X19-X26 and declares an 8,192-byte native reserve:
`asm.MaxSpillSlots` (512) × 8 bytes plus `interp.nativeFrameLimit` (128) × 32
bytes. Its complete Go frame is 8,272 bytes including the 80-byte trampoline
area.
Native code starts at the top of the reserve, so generated SP adjustments stay
inside memory covered by Go's stack-growth check. X26 is the stable spill-frame
base, so a native self-call may move SP without changing spill addresses. Keep
the allocator limit, native frame limit, and `internal/asm/arm64/abi_arm64.s` reserve in
sync: `interp.TestNativeStackReserve` (`interp/jit_arm64_test.go`) asserts
`asm.MaxSpillSlots*8 + nativeFrameLimit*journalStride*8` equals the reserve
literal parsed out of `abi_arm64.s`, and that the reserve plus the 80-byte
callee-saved save area equals the trampoline's total Go frame size, so an
edit to any one constant without the others fails a test instead of
corrupting the native stack at runtime.

Register allocation (`internal/asm/rewriter.go`) is a single linear-scan pass: it spills the live vreg whose final use is farthest ahead to a stack slot at the stream position where pressure was observed. Rewritten labels target the start of any inserted reload/store prefix, and labels on a return target its inserted frame epilogue. A call whose target label is bound in the same build runs through the shared epilogue on return, so the rewriter reserves the caller's spill area again after it. Linear-scan lifetimes describe a forward-only stream, so a build containing a back-edge runs without a spill frame at all: a value live across the loop would otherwise be spilled at what merely looks like its last use. The frame prologue sits ahead of the first instruction, and internal branches rebase past it, so a back-edge can never reserve the frame twice.

Linear spill state is unsafe across a loop back-edge, and mutation blocks can combine paths around state materialization. Two layers enforce safety:

- `internal/asm/rewriter.go` rejects spilling for code containing an intra-code backward branch.
- `noSpill` scans every step in the completed plan, including learned continuations, and forbids spilling whenever `ARRAY_SET` or `STRUCT_SET` is present.

When a plan forbids spilling, the compiler wraps the target architecture in `noSpillArch`. Its `Frame()` returns `nil` according to the assembler contract, so register exhaustion rejects native compilation cleanly and threaded dispatch remains installed. `ARRAY_SET` and `STRUCT_SET` use the common fresh-register heap path rather than a store-specific register-recycling path.

Native code does not marshal parameters or returns. It writes results and trap state into the journal, and the Go wrapper restores interpreter state from there.

## Frame Journal

`i.journal` is owned by `Interpreter`. It is both input context for native entry and output state for deoptimization.

Header cells come before fixed-stride frame records.

| Cell | Purpose |
|---|---|
| `journalStack` | stack base pointer |
| `journalGlobals` | globals base pointer |
| `journalBP` | current frame base |
| `journalSP` | stack pointer |
| `journalDepth` | number of written frame records |
| `journalCap` | available frame record capacity, capped at 128 |
| `journalTrap` | trap state |
| `journalNextIP` | fallback or resume IP |
| `journalBudget` | native loop back-edge budget |
| `journalActive` | active native call depth |
| `journalRC` | refcount base pointer |
| `journalUpvals` | closure upvalue base pointer |
| `journalHeap` | heap base pointer |
| `journalNatives` | fixed per-function native-entry slot base |
| `journalExitID` | fallback descriptor ID plus one; zero means no descriptor |
| `journalHead...` | frame records `{addr, bp, ip, returns}` |

On guard failure, native code writes live stack state, appends frame records, sets trap state, sets the resume IP, and returns to Go.

The Go wrapper rebuilds the VM state and resumes threaded execution.

If the fallback IP is `0`, the wrapper runs the shadowed threaded entry handler once to avoid immediate native re-entry.

### Lifecycle Profiling

Observable profiling is enabled only by an explicit profiler. Internal hotness sampling alone does not emit detailed rows.

Each published native entry carries its frontend, own byte size, and immutable
exit descriptors. Installation resolves stable local counters for entry, yield,
and every descriptor. Native wrappers increment those handles directly; they do
not construct labels.

Every fallback creation site assigns a descriptor with a stable reason. It uses
the concrete source opcode when the fallback is attributable to one; synthetic
boundaries such as an `opLimit` trace cut use `none`. Generated code writes
`descriptor ID + 1` to `journalExitID` before returning with `trapFallback`. The
Go wrapper resolves that ID and counts the exact exit row. Zero means no
descriptor. `trapYield` counts only a yield, and native frame overflow counts
neither an exit nor a yield.

Compile and emission ownership follows compilation ownership: a solo compiler
records its result, while a shared cache records it only on the winning member.
Peers install their own runtime counters without duplicating compile or emission
rows. Collector flush preserves registered handles while moving accumulated
values to the shared profiler.

## Speculation

Observed numeric and heap facts are speculative unless they come from bytecode constants.

Native code may specialize on observed values, but a mismatch must exit before the opcode executes. The threaded handler owns the general case.

This rule keeps native lowering small.

## Calls and Returns

Native lowering supports selected calls:

- direct `CONST_GET function; CALL`
- guarded function-value calls
- eligible closure-body calls

A call may lower to native `BL` when the observed target is a JIT-eligible `*types.Function` with matching arity.

Unsupported targets fall back, including host calls, allocation, maps, unsupported functions, unsupported closures, and heap mutations outside the selected guarded fast paths.

Static plans recognize direct `CONST_GET function; CALL` pairs. Each interpreter owns a fixed-size `natives` slot array; installing or synchronizing a function entry publishes its executable address atomically. The caller loads the slot at runtime and uses `BLR`, so compile order does not matter: a null slot falls back at the CALL, while a later callee installation is visible without recompiling the caller. Supported `RETURN_CALL` paths use native tail-loop or tail-morph lowering.

A call whose callee is the function being compiled uses the native self-call path (`BL` to `ctx.head`) only when the current native frame is that entry plan's own frame and the target has no captures. `selfCall` owns those ABI preconditions; the call sites validate return kinds before consuming the call marker, then either enter `selfCall` or keep the ordinary call path. An inlined frame or loop plan therefore never re-enters an entry prologue over a foreign activation. Lowering the fused form is what lets the static frontend plan a recursive function at all; while it was rejected, such a function had no whole-function plan and fell back to a recorded trace, whose coverage varied with how much of the recursion the recording happened to reach. `TestARM64_SelfCallFromInlinedFrame` covers the foreign-frame case, while `TestARM64_SelfCallWithRefArg` ensures the safe whole-function form still installs native code. Recursive calls encountered inside loop traces are still cut at the call so the continuation is compiled from the real post-call state.

A callee frame's non-parameter locals are cleared by the callee, in the entry prologue at `ctx.head` (`zeroLocals`), not by its callers. Every entry path arrives there with `bp` already pointing at the new frame — the Go wrapper, `directCall`'s `BLR`, and `selfCall`'s `BL` — so one clear covers all three, and it matches what threaded `CALL` does before transferring control. Only a whole-function entry may do this: a loop plan re-enters a frame whose locals are live, and module code has no caller that would have cleared them. Skipping it hands the callee stale boxed words from whatever frame last occupied that stack region, so its first `LOCAL_SET` releases a ref it never owned and `RETURN` teardown releases the rest. `TestARM64_CalleeLocals` covers both native call paths; a function with no non-parameter local cannot expose it, which is the shape every earlier self-call test used.

Native calls are frame-aware. The lowering checks frame budget, increments native depth, saves caller state, publishes the callee BP/SP into the journal, enters the callee trace, and restores the caller state and journal frame on normal return. The journal publication is required because every native entry prologue reloads BP/SP from `journalBP`/`journalSP`; without it, nested native entries inherit the outer caller's frame and mutual recursion can keep reusing the wrong argument slot until frame overflow. A trap leaves the callee's journal state intact for deoptimization, while the normal path restores the caller before continuing. `TestARM64_MutualEntries` covers two independently installed native entries calling each other.

A native call invalidates the caller's cached local registers: the callee owns every allocatable register, so the call sites clear `activation.state` on return, and the committing flush before the call leaves the VM stack slot authoritative. `activation.locals` still names the register each value was last materialized into, so `activation.isLoadedAt` is the one test for whether that name is still good. `guardFrame` reads every ref local for the frame teardown; boxing an unloaded one releases whatever the callee left in that register, which faults inside the Go runtime rather than diverging quietly. `TestARM64_SelfCallFrameLocals` pins it.

X26 carries the caller's spill base across a `BLR`. The callee is entered at its own offset zero, so it runs the frame prologue and repoints X26 at its own frame; the caller saves X26 into its 32-byte save area before the call and restores it immediately after, on both the normal and the trap path. A self-call (`BL` to `ctx.head`) needs no such save: it shares the caller's frame, and that stream cannot spill at all because the backward branch to `head` disables the spill frame (`internal/asm/rewriter.go` `backEdge`).

On deoptimization, native frames append enough journal records for Go to rebuild the VM call chain.

`RETURN` closes a function entry trace only when it returns from the outer recorded frame. Inlined callee returns stitch values back into the caller's symbolic stack. `RETURN_CALL` tail-loop and tail-morph paths first preflight the retiring activation, then own forwarded arguments and release the retiring frame.

A call may return a ref. `checkReturns` admits `KindRef` alongside the scalar kinds, and both branch paths push the result as a boxed operand (`raw: false`) owning exactly one retain, the same shape `ARRAY_GET` and `STRUCT_GET` produce.

Native frame teardown mirrors threaded ownership: `stitch` and `ret` guard the retiring refs, preserve returned refs that still point into the frame, and finally `releaseFrame` drops the owned refs before the frame is removed. The guard counts duplicate addresses together; native teardown deoptimizes when any address cannot cover all pending releases, so native code never decrements an object to zero without the interpreter's reclaim path.

`ret` must take the returned refs' retains *before* that guard, not after. The guard deopts when `rc <= pending`, and the common shape — return a freshly allocated object held only by one frame local — sits exactly at `rc == pending`. Owning first raises `rc` above `pending` so the guard passes, and `releaseFrame` then leaves the single reference the caller owns. With the opposite order every such `RETURN` deopts: still correct, because the interpreter finishes the work, but never native. A spurious deopt is invisible to value and refcount oracles, so `TestARM64_RefReturn` asserts the `guard-value` exit count is zero instead. `stitch` keeps the original order: the inline path rejects any callee holding a ref local, so an inlined return is backed by a parameter or upvalue and cannot reach that boundary.

Top-level module code has no synthetic `RETURN`. Falling off the end closes the module trace and writes live operands back to the VM stack.

## Branches

Recorded forward branches become guarded exits or learned branch continuations.

`BR_IF` and `BR_TABLE` emit the recorded path. Unrecorded targets deoptimize.

When a side exit becomes hot, the tracer records that target. A later compile may fold it into the same native callable as a pending block. The loop wrapper records every fallback exit as a branch, so loop anchors recompile through the same side-exit machinery as entries. Loop roots are never folded as ordinary continuations: a leg that rejoins this plan's header folds into the native back-edge (see Trace Snapshots), while a leg that is another loop's root deoptimizes and uses that loop's standalone entry, which preserves back-edge and safepoint semantics.

A loop callable normally exits through a trap, but a folded depth-0 `RETURN` leg emits `ret()` and a folded completed leg emits `complete()`, both returning with `trapNone`. The loop wrapper handles this like the entry wrappers: a function loop performs the threaded `RETURN` frame teardown, and a module loop marks the frame exhausted.

Pending blocks reload from VM stack slots, run through a FIFO worklist, and stop at a bounded pending cap. The trace frontend orders learned roots once; the backend does not repeatedly sort pending work.

A cold branch edge may carry caller-continuation block IDs. The side trace body lowers first; on callee `RETURN`, lowering stitches the result into the caller frame and follows those IDs. The continuation reloads from VM stack slots before continuing.

A deferred profiled edge gets a label and canonical snapshot, shared only with an identical scheduled continuation (same block, tail, and snapshot). Static state-backed blocks share labels only through explicit block IDs, never through bytecode-anchor equality.

Solo interpreters recompile a side exit when its hit count first reaches the hot-exit threshold. Pooled interpreters also rearm on later threshold multiples so a peer can recover a missed shared-cache publication.

Targets still deoptimize when they are unknown or unsupported.

Branch lowering may skip hot-path flushes only when the branch state is clean. If locals or operands are dirty, flush first. Learned continuations and side exits must see the same stack image as threaded dispatch.

A committing flush (`selfCall`, `tailLoop`) transfers operand ownership to the VM stack, so it accepts a live `backingStack` ref: that ref already carries the retain taken when it was pushed, and committing hands the same edge to the stack, exactly as the inlined call path does when it stores arguments and drops them from the operand stack. It rejects any live deferred ref (a const marker or a slot-backed operand): a deferred ref carries no retain of its own, and a loop back-edge has no cold stub to take one, so owning it each iteration would leak. Eligible loop-carried scalars are the exception to local materialization: their registers remain authoritative across the back-edge and cold handoffs commit them separately. A self-recursive function still forwards a ref parameter to itself because the argument is owned into the callee frame (through the call-argument path) before the commit. See Reference Ownership.

### Branch range validation

ARM64 conditional/compare/test branches (`B.cond`, `CBZ`/`CBNZ`, `TBZ`/`TBNZ`) encode a fixed-width signed PC-relative immediate — imm19 (±1MB) for `B.cond`/`CBZ`/`CBNZ`, imm14 (±32KB) for `TBZ`/`TBNZ`, imm26 (±128MB) for `B`/`BL`. `internal/asm/arm64.Encoder.Encode` validates every such offset is 4-byte aligned and fits its field, returning `asm.ErrBranchOutOfRange` instead of silently masking an out-of-range offset into a wrong target. `interp/jit.go` `publish` treats `ErrBranchOutOfRange` the same as `asm.ErrNoRegistersAvailable`: it aborts native lowering for that trace and falls back to threaded dispatch rather than emit a corrupt callable.

Before that fallback triggers, `asm.Assembler.encode` runs a branch relaxation fixpoint (`asm.Relaxer`, implemented by `internal/asm/arm64.arch.Relax`) between the draft and final encoding passes. Each pass drafts the current instruction list once, collects every `B.cond`/`CBZ`/`CBNZ` label branch whose imm19 displacement does not fit, and rewrites all of them together into an inverted-condition branch that skips a following unconditional `B` (imm26, ±128MB) to the original target; it then re-drafts and repeats until a pass finds nothing left to relax. Both replacement instructions are constructed to already be in range, so a given branch relaxes at most once and the loop always terminates, and batching every out-of-range branch within a pass keeps the number of drafts proportional to the number of passes rather than the number of branches; if the unconditional `B` itself would not reach the target (>±128MB), `Relax` returns `false` and `ErrBranchOutOfRange`/the JIT fallback still applies. `TBZ`/`TBNZ` never carry a `LabelOperand` in this codebase (their offset is always a caller-computed immediate — see `internal/asm/arm64/instr.go`), so they never reach `Relax` and the imm14 (±32KB) window has no relaxation path; architectures without a `Relaxer` (amd64) are unaffected — `encode` no-ops the pass.

## Loops

A loop root is the target of a backward branch. Backward branch handlers report hotness directly; profiler sampling is not involved.

Native loop entries run with the current frame and return to threaded execution through explicit safepoints or deoptimization. Loop-carried scalar locals may stay in registers when the plan can preserve their state safely; otherwise the loop uses VM stack slots.

Loop roots are compiled as separate native entries and installed at `i.code[addr][header]`. A loop root never tears down its frame.

## Suspension

`YIELD` and `RESUME` are suspension points. They cannot execute as normal linear native trace operations.

Suspended state is held by the private `coroutine` value; it is not a host
extension seam.

For anchor-frame suspension:

- tracer records the opcode as a terminal
- native code emits an unconditional fallback at the opcode IP
- threaded dispatch performs the real suspend or resume exactly once

The resume IP is the opcode itself, not the next instruction, because the threaded handler advances `ip`.

Suspension inside an inlined callee aborts the trace. Deoptimization can rebuild inlined frames, but it does not restore their coroutine handle. Only the anchor frame can safely keep its coroutine state across deoptimization.

## Values

Scalars stay unboxed between native trace operations.

| Kind | Native treatment |
|---|---|
| `i32` | low 32 bits |
| `i1` / `i8` | low 32 bits with narrow result kind preserved where required |
| `i64` | full signed register value when inline-boxable |
| `f32` / `f64` | IEEE bit representation |
| heap-promoted `i64` | deoptimize on load |

Narrow kinds share the `i32` representation. Kind checks compare representation, so `i1` and `i8` can flow into `i32.*` lowering.

Result kinds must match the interpreter:

- `i32.and`, `or`, and `xor` preserve a shared narrow kind
- mixed narrow operands widen to `i32`
- other arithmetic widens to `i32`
- comparisons and `eqz` produce `i1`

## Slots and Refs

`GLOBAL_*`, `LOCAL_*`, and `UPVAL_*` lower for in-range static slots.

Scalar slots load and store raw values directly.

A ref slot store releases the overwritten ref and transfers the stored ref, both guarded through `journalRC`. A ref `LOCAL_GET`/`GLOBAL_GET`/`UPVAL_GET` instead pushes a deferred operand and takes no retain (see Reference Ownership).

If a release may free the object (`rc == 1`), native code deoptimizes before the release. The interpreter owns recursive release and cleanup.

## Reference Ownership

A ref `value` carries an `backing` that records where its reference count lives.

| Backing | Retain location |
|---|---|
| `backingStack` | the operand-stack copy owns its own retain |
| `backingConst` | a compile-time constant marker; retain deferred |
| `backingLocal` / `backingGlobal` / `backingUpval` | deferred to the backing slot (`slot`) |

Only a `backingStack` ref carries a retain. Every other backing defers it to backing storage that already holds one, so the operand is a borrowed view until it transfers to interpreter-visible state.

Producers push deferred. `LOCAL_GET`, `GLOBAL_GET`, and `UPVAL_GET` of a ref take no retain and record its backing slot; const markers push `backingConst`; `DUP` of a deferred ref copies it deferred. Container consumers borrow the operand and elide their matching release when it is deferred: `ARRAY_GET`/`SET`, `STRUCT_GET`/`SET`, `ARRAY_LEN`, `REF_IS_NULL`, `DROP`, and the coroutine, error, and string reads all skip the container `guardRC`/release when the consumed operand is not `backingStack`. A ref element or payload result is still retained; only the container's own release is elided. This removes the per-element retain/release pair from primitive container loops.

A retain materializes at every point that hands a deferred value to storage the interpreter can see:

- `own` — storing into a local, global, or upval slot, transferring a ref into an array element or struct field, transferring a call argument through `locals()`, and boxing an entry-frame return in `ret`.
- `detach` — before a backing slot is overwritten (`LOCAL_SET`/`GLOBAL_SET`/`UPVAL_SET`) or a frame dies (`stitch`, tail dispatch), every live operand deferred to that slot is owned first.
- exit stubs — `emitExits` reloads each deferred operand from its flushed VM stack slot and retains it on the cold guard path.
- `retainDeferred` — a stub-less deopt that hands the flushed operand stack to the interpreter (a trap fallback, module completion) re-takes each deferred operand's retain from its VM stack slot.
- real calls — `directCall` and `selfCall` own every live deferred operand before the `BL`, because a callee trap adopts the caller's flushed stack.

A committing (loop back-edge) flush rejects any live deferred ref: owning it would retain once per iteration with no matching release, so a loop-carried deferred ref keeps the whole trace threaded instead. Standalone loop traces also remain threaded when their entry already has live operands because trace plans do not reconstruct loop-entry operand state.

## Heap Reads and Mutations

ARM64 supports selected heap fast paths.

Native full-trace reads include observed shapes for scalar `REF_GET`, selected `ARRAY_LEN`, selected `ARRAY_GET`, selected `STRUCT_GET`, `ERROR_GET`, `CORO_DONE`, and `CORO_VALUE`. `ARRAY_SET` and `STRUCT_SET` use the guarded fresh-register heap path for both primitive and ref stores; the former compile-time-constant-container restriction is removed.

Heap reads guard ref address, heap itab, array element kind, struct type pointer, struct field kind, index bounds, and release safety when needed.

`STRUCT_GET` and `STRUCT_SET` also lower against a `*HostStruct`, whose fields hold Go memory rather than VM words (see `docs/host-integration.md`). `hostGet` and `hostSet` (`interp/jit_arm64.go`) guard the heap itab against `heapHostStruct`, bounds-guard the field index against the compiled layout the view carries, and guard that field's Go kind against the one the trace recorded in `shape.field`, then load or store the Go field through the address the layout's offset names. Nothing about a host view is assumed from the itab alone: the same compiled access serves every `*HostStruct`, so a container whose field at that index has another Go kind exits at the kind guard rather than loading the wrong width.

`hostShapes` (`interp/jit.go`) is the one place a hosted Go field's layout is written down, indexed by the `reflect.Kind` the codec compiled the field through, and mirrors the codec's own `leaves` table. A kind with no row - `string`, a pointer, a nested container - publishes a heap reference rather than loading a word, so its access stays with the interpreter. A read reinterprets a field as wide as its VM slot and widens a narrower one by the field's own signedness, which is why an `int16`, an `int32`, and a `uint32` field all reach the guest as i32 but do not share a load. A write lowers only for a field as wide as its slot: a narrower one decodes through the range check `setSigned` and `setUnsigned` perform, and a check that can fail belongs with the interpreter that reports it.

Ref reads retain the loaded element or payload. A container consumer releases its container handle only when that operand owns its retain, eliding the release for a deferred operand (see Reference Ownership): `CORO_VALUE` still retains the value and releases the handle when the handle is `backingStack`. `CORO_DONE` keeps the handle.

Heap-promoted `i64` values fall back before boxing.

Primitive typed-array `ARRAY_SET` and scalar-field `STRUCT_SET` may continue through native execution when their guarded heap path fits the no-spill register budget. Guard failure resumes at the original opcode; success performs the store and continues to later operations or the loop back-edge.

Ref-element `ARRAY_SET` and ref-field `STRUCT_SET` continue natively like their scalar counterparts. Before the store, lowering owns a deferred element or field value so the transferred container edge carries exactly one retain, matching threaded execution. A replaced `BoxedNull` field/element has no heap ownership and is not released.

Both were terminal until the callee-frame defect below was found. Letting either continue drove refcounts negative against a threaded twin from self-recursion depth two upward, and two attempts to lift the rule were reverted on that evidence. The cause was never in the stores: a native callee began with non-parameter locals that no one had cleared, so its first `LOCAL_SET` released a stale boxed word it never owned. Lifting the store rule is merely what first admitted a function holding a ref local into native lowering, which is why the two appeared connected. `TestARM64_RefContainerStore` covers the shape that used to diverge.

Mutation plans are always no-spill. Stores use the common fresh-register heap path; if the physical register budget is exhausted, `asm.Build` rejects native compilation with `CompileReasonRegisterPressure` and threaded execution remains installed. Native compilation must never spill a store path across a back-edge.

Allocation and complex ref-bearing mutations either bridge (see Bridge) in a static plan or stay threaded/terminate the native trace in a trace plan.

## Bridge

A bridge deopts one opcode the backend cannot lower to the threaded interpreter and resumes native execution afterward, instead of ending the native entry outright. It generalizes the mechanism first built for `ARRAY_NEW_DEFAULT` alone.

`bridgeable` (`interp/jit_plan.go`) is the single predicate naming every opcode eligible: the allocation family (`ARRAY_NEW`, `ARRAY_NEW_DEFAULT`, `ARRAY_SLICE`, `ARRAY_DELETE`, `STRUCT_NEW`, `STRUCT_NEW_DEFAULT`, `MAP_NEW`, `MAP_NEW_DEFAULT`, `MAP_DELETE`, `MAP_CLEAR`, `REF_NEW`, `REF_SET`, `CLOSURE_NEW`, `STRING_NEW_UTF32`), the map/string/bulk-array opcodes jit_arm64.go otherwise lowers as an unconditional trap (`MAP_LEN`, `MAP_GET`, `MAP_LOOKUP`, `MAP_KEYS`, `MAP_ITER`, `STRING_ENCODE_UTF32`, `STRING_ITER`, `ARRAY_FILL`, `ARRAY_COPY`, `ARRAY_APPEND`, `MAP_SET`), structured errors (`ERROR_NEW`, `ERROR_CODE`, `THROW`), and `REF_TEST`/`REF_CAST`. An opcode already lowered natively (`ARRAY_GET`, `STRUCT_SET`, and so on) must never appear here: a bridge is strictly the fallback for opcodes with no native lowering. `YIELD`/`RESUME` are excluded even though the backend cannot lower them either — suspension cannot resume mid-frame into native code (see Suspension) — so they keep the unconditional terminal-fallback treatment in `arm64Lowerer.steps` instead.

The static planner (`staticPlan`) is the frontend that acts on `bridgeable`: walking a function's bytecode, an opcode it names ends the current plan block with a `terminateBridge` terminator instead of becoming an ordinary step, and the remaining source instructions continue into a fresh block anchored right after it, marked `block.bridge`, carrying the post-op dataflow state so lowering reloads it exactly like any other state-backed block. `applyStep` must still be able to model the opcode's stack effect for the plan to proceed: fixed-arity opcodes use `instr.TypeOf`'s `Pop`/`Push` directly; the dynamic-arity ones (`STRUCT_NEW`, `MAP_NEW`, `CLOSURE_NEW`, `ARRAY_NEW`, `ARRAY_APPEND`) derive their count from the instruction's own operand, a known compile-time constant on the stack (`slot.valKnown`), or a statically resolved callee, matching how `program/verify.go`'s `flow()` computes the same opcodes' effects for verification; when none of these resolve the effect, the plan is rejected exactly as before. A pushed slot produced by a bridged opcode's own effect (a fresh allocation, a resolved element/field value) must be a new `backingStack` slot, never a mutated copy of an operand that existed before the bridge: after the bridge, `retainDeferred` has already taken a real retain for every deferred operand handed to the threaded closure, so continuing to mark a survivor as deferred (`backingLocal`/`backingGlobal`/`backingUpval`/`backingConst`) makes a later consumer elide a release that must run, leaking the retain (see Reference Ownership). `REF_CAST` (identity pass-through: pop, then push the same kind, narrowing `styp` when the declared target is a struct type) and `ARRAY_APPEND` (its array operand is never popped, so it survives on the stack) both learned this the hard way and construct a fresh slot instead of reusing the pre-bridge one.

`arm64Lowerer.dispatch`, emitted once per callable, reads the journal's entry-IP cell at the top of the callable and, when it names a `block.bridge` anchor, branches directly to that block's label instead of falling into the normal anchor start; zero (every ordinary `Call`'s value) falls through unchanged. `arm64Lowerer.bridge` (`l.term`'s `terminateBridge` case) traps with `trapBridge` and the opcode's own IP, sharing `trapFallback`'s flush and `retainDeferred` handoff but carrying no exit descriptor — a bridge is productive continuation, not a give-up (see Retirement), and `watchdog.bridge` counts it on a separate counter so it can never inflate the give-up rate. `Interpreter.bridge` (`interp/interp.go`) is the Go-side half: it runs `i.code[f.addr][ip](i)` — the bridged opcode's own threaded closure — exactly once, then reports the IP native execution may resume at, or `ok=false` when it must not (the closure moved frame/function, made no forward progress, spent the wrapper's `loopBudget` of bridge cycles, or the new IP is not one the callable's `resumable` list carries an entry-dispatch label for). If the bridged opcode's own IP is 0 — the function's very first instruction — `i.code[f.addr][0]` is the native wrapper this call is already running inside (`install` overwrites only the anchor slot), so `Interpreter.bridge` runs the shadowed threaded handler (`i.stub`) instead of that wrapper, exactly as a `trapFallback` resuming at 0 already did (see the Loops section's header note).

A bridge cycle re-enters through a fresh external `Call`, which never runs the loop-carry prologue (see `arm64Lowerer.dispatch` above): a carried register would be uninitialized garbage on such a resume. A bridge a plan can reach therefore keeps every local slot-backed instead of loop-carried. The scope is the plan, not the function: each loop plan carries only the blocks its own root reaches (see Static Frontend), so a bridge this header cannot reach is not compiled into this callable, has no resume label here, and does not disable carrying. A bridge that the header does reach but that sits outside the loop's own back-edge range still disables it; narrowing that residual case to "a bridge inside the loop body" is unimplemented follow-up work, tracked because it would need the carry-load prologue to run on every re-entry path, not just the callable's own head.

`arrayKind` (`interp/jit_plan.go`) resolves an `ARRAY_GET`/`ARRAY_DELETE` element kind from a known constant container's concrete heap itab, matching `arrayGetKnown`'s native lowering, and otherwise from the declared array type — mirroring `structFieldKind`'s declared-struct-type resolution, which `STRUCT_GET` already relies on. The declared type answers only in a call-free plan (`callFree`). Lifting that gate lets `ARRAY_GET`'s general (non-constant) lowering path run alongside a native `CALL` in the same plan, and that combination corrupted native execution state on the next native call boundary (`runtime.mallocgc` SIGSEGV) instead of cleanly guarding and falling back the way `structFieldKind`'s equivalent case does. The `BLR` no longer clobbers the spill base (see Calls and Returns), so it no longer crashes, but three tests still diverge behaviourally, so the gate stands until that is understood. Until then a function containing a call keeps resolving `ARRAY_GET`, `ARRAY_LEN`, and `ARRAY_DELETE` only from a known constant container.

## Structured Errors

`ERROR_NEW`, `ERROR_CODE`, and `THROW` bridge in a static plan (see Bridge) and remain terminal fallback boundaries in a trace plan.

The tracer records them without stepping the clone. In a trace plan, native code deoptimizes at the opcode IP with no resume, and the threaded handler performs error allocation, code extraction, throw unwinding, and handler landing.

If any of these appears in an inlined callee frame, the trace aborts.

## Installation

Compiled modules install into the threaded dispatch table.

Entry wrappers and loop wrappers differ:

| Wrapper | Use | Frame behavior |
|---|---|---|
| `entry` | module/function entry | may complete or tear down frame |
| `loop` | loop header | re-enters live frame |

Install only accepted callables. Rejected roots leave the existing threaded closure intact.

Native wrappers must always leave the interpreter in a valid state for threaded redispatch.

### Cooling and retirement

Cooling is the compile-side half: a function whose entry root and every loop
header have been attempted without installing anything stops being sampled,
captured, and back-edge instrumented. `docs/profile.md` owns that rule.
Retirement below is the runtime half, for native code that is already
installed.

### Retirement

A trace can compile into a native entry that runs a few instructions and then always gives up instead of completing its job. A high exit rate alone is not a failure signal — a healthy kernel like Sieve or NQueens exits on nearly every entry, through `loop-exit`. A high *give-up* exit rate is, because the interpreter pays full bailout and re-entry cost for work the native code never finished. `givesUp` names the three ways that happens: `prof.ExitTraceCut` is native code that knowingly stops mid-function; `prof.ExitColdBranch` is a cold branch taken anyway, so the recording predicted the wrong path; and the four `prof.ExitGuard*` reasons are speculation the runtime refuted. `prof.ExitLoop` is how a loop normally ends and `prof.ExitTerminalOp` is a deopt the plan intended, so neither counts. "Unproductive" is cooling's word for a different thing (see `docs/profile.md`), so retirement says give-up throughout.

Each installed anchor gets a `watchdog`: two counters (entries, give-up exits) plus a `[]bool` precomputed at install time from the entry's exit descriptors, so the hot path never depends on the profiler being attached (unlike the Lifecycle Profiling counters above, which are no-ops when no profiler is set). `call`, `start`, and `loop` each count one entry per invocation and, on a fallback exit, one give-up exit when `givesUp` accepts the resolved descriptor's reason. Every 1024 entries, if at least a quarter gave up, the anchor retires: the shadowed threaded handler (saved at install time) replaces it in the local dispatch table, a function-entry anchor's `natives` call-fast-path slot is atomically cleared (a null slot already makes callers fall back at `CALL`), and the function is marked cold through the same `cool` a compile-side function that never installs anything uses, so it is neither re-instrumented nor recompiled. Otherwise the window resets and the entry keeps running.

Retirement only ever mutates the local interpreter's dispatch table (`i.code`, `i.natives`, `i.cold`), never a pool's shared published module, so it is safe to run from inside the very wrapper closure it replaces. A later publish that lands on a cold function still resumes it (see Installation above).

## Tests

Run focused tests after JIT changes:

```bash
go test ./internal/asm/... ./interp/...
```

Use this guide:

| Change | Test focus |
|---|---|
| ABI or callable behavior | `internal/asm/assembler_test.go` |
| trace recording | `interp/interp_test.go` |
| native lowering | `interp/interp_test.go` |
| install or wiring behavior | `interp/interp_test.go` |

## Maintenance Notes

When changing JIT internals:

- keep the threaded interpreter correct first
- keep native lowering speculative and guarded
- deoptimize before behavior the JIT cannot fully own
- prefer one simple terminal fallback over duplicated semantics
- keep architecture-neutral code in `jit.go`
- keep ARM64 lowering in `interp/jit_arm64.go`
- keep journal layout explicit and stable
- preserve interpreter/JIT stack and ref ownership symmetry
- keep shared cache, tracer, and coroutine state private behind `Pool` and `Interpreter`
- use short, standard names such as `trace`, `root`, `entry`, `loop`, `module`, `lowering`, `guard`, `exit`, `frame`, and `value`
- avoid adding an abstraction unless it removes real duplication or isolates real complexity

## Related Docs

- `docs/profile.md` — sampling, hotness thresholds, and JIT counters
- `docs/benchmarks.md` — benchmark results and methodology
- `docs/value-representation.md` — boxed values and kind semantics
- `docs/memory-model.md` — refs, ownership, and heap lifecycle
- `docs/instruction-set.md` — opcode semantics
- `docs/debugging.md` — bytecode-level mode that disables optimized execution
