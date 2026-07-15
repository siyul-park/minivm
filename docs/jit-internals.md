# JIT Internals

Contracts for the ARM64 JIT in `interp/` and its interaction with `asm/`.

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
| callable ABI | `asm/` |
| value layout | `docs/value-representation.md` |
| heap ownership | `docs/memory-model.md` |
| ticks and thresholds | `docs/profile.md` |

## Summary

minivm always compiles bytecode to threaded closures first. The JIT is a lazy ARM64 plan backend layered on top of that portable threaded runtime.

```text
program.Program
  -> threader -> []func(*Interpreter)   always available
  -> Tracer           -> trace snapshots        lazy runtime recording
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

Solo interpreters own a private `compiler` and `asm.Buffer`.

Pool interpreters use a shared `Cache`:

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

1. `staticPlan` constructs a complete plan from verified bytecode and dataflow when no function entry is installed.
2. `tracePlan` constructs plans from immutable runtime trace snapshots.
3. If neither frontend produces a lowerable plan, threaded execution remains installed.

Both frontends return the same private `plan` model: ABI kind, a root block ID, flat blocks, entry states, ordinary steps, explicit edges, and spill policy. Every internal edge carries a block ID; unresolved edges retain only their threaded fallback anchor. Build, link, validation, accounting, and publication are centralized in the compiler.

## Static Frontend

The static frontend analyzes basic blocks with one forward fixpoint that tracks stack kind, constant-ref provenance, and direct-call targets. It emits plan blocks with explicit entry state, decoded operands, and block-ID edges. Unsupported instructions become exact-IP fallback boundaries when the surrounding function remains structurally valid.

Top-level modules containing `CALL` or `RETURN_CALL` are rejected because module entry does not implement the framed native-call ABI. Primitive typed-array constants remain ownership-neutral markers until `ARRAY_GET`; native code reloads the current heap cell, guards its shape and index, and retains the marker only on a cold fallback.

## Tracer

Trace recording lives in `interp/trace.go`.

Recording clones the interpreter, starts the clone at the requested `(addr, ip)`, and executes threaded closures until it reaches return, loop back-edge, branch exit, unsupported operation, trace limit, or abort condition. A backward edge to a different header cuts the linear prefix so that header can become a standalone loop trace with a native safepoint budget. Reaching the trace limit records a partial trace with a resumable cut instead of aborting. Native execution deoptimizes at that cut; when the exit becomes hot, the existing side-exit machinery records and compiles the next bounded continuation.

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

The tracer aborts before host calls and allocation. It records ref-bearing array writes and struct writes only as terminal fallback boundaries. A primitive typed-array write may remain inside the trace when it occurs in the anchor frame before any inlined call. Capture clones every overlapping visible range of aliased primitive typed arrays into one replacement backing store, preserving slice offsets while leaving the live heap unchanged. Boxed arrays and structs are copied before their terminal mutation. The clone also owns mutable dispatch metadata and suppresses external finalizers, so speculative reference reclamation cannot alter live functions, trace trees, or host resources.

Every recorded `trace` has one outcome: `loop`, `returned`, `completed`, `partial`, or `aborted`. The trace frontend maps usable outcomes to plan terminators and excludes aborted fragments from learned continuations. When an observed block runs out of steps, lowering decides completion from that block's terminator, never from the root trace. This prevents an unsupported side fragment from being mistaken for normal completion.

`Tracer.capture` serializes recording and returns an already-published root when one exists, so sampling and compilation cannot record the same entry concurrently. A tracer is bound to one `program.Program`; binding it to a different program clears exact bytecode, tree, and loop caches before reuse.

`Tracer.headers` (the static loop-header scan) uses `instr.Targets(code, ip)` rather than switching on `BR`/`BR_IF` directly, so a loop formed only through a backward `BR_TABLE` case target is recognized as a header too.

Non-eager functions initially keep the ordinary generated `BR` handler. When periodic sampling first reaches the rounded threshold, the interpreter rethreads that function once with an exact unconditional-backedge callback, preserving installed native handlers and replacing only their threaded fallbacks. Eager mode enables the callback from construction. Cold loops therefore pay no callback, mutex, or header-scan cost.

## Trace Snapshots

A pool shares one `Tracer`. Tree mutations are locked. `rootAt` returns a stable snapshot containing immutable trace pointers plus copied branch and hit containers.

`tracePlan` converts that snapshot into flat plan blocks. It excludes aborted fragments and standalone loop roots from learned continuations, sorts continuation roots deterministically, connects internal paths by block ID, and derives spill policy from the final plan rather than exposing the trace tree to lowering.

## Backend

`jit.go` is architecture-neutral. Build-tagged `lower(*lowering, plan)` implementations consume the normalized plan directly; unsupported architectures never construct a compiler.

`jit_arm64.go` owns all ARM64 lowering: orchestration, the single opcode dispatcher, control flow, numeric operations, calls, frames, deoptimization, heap access, and reference ownership.

Every plan block passes through one `emitBlock` path and every edge carries an explicit block ID or an unresolved threaded-fallback anchor. Bytecode locations describe source positions only; block IDs preserve distinct inlined contexts even when they share the same `(function, IP)`. A state-backed block reloads VM homes, while a profiled successor may continue with the current symbolic state.

Caller continuations are ordinary blocks in the same flat block pool. A cold edge carries the continuation block IDs that must run after an inlined callee returns. Deferred edges always receive an independent label and symbolic snapshot; the backend does not merge states by bytecode anchor.

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
the allocator limit, native frame limit, and `asm/arm64/abi_arm64.s` reserve in
sync: `interp.TestNativeStackReserve` (`interp/jit_arm64_test.go`) asserts
`asm.MaxSpillSlots*8 + nativeFrameLimit*journalStride*8` equals the reserve
literal parsed out of `abi_arm64.s`, and that the reserve plus the 80-byte
callee-saved save area equals the trampoline's total Go frame size, so an
edit to any one constant without the others fails a test instead of
corrupting the native stack at runtime.

Register allocation (`asm/rewriter.go`) is a single linear-scan pass: it spills the vreg with the farthest-away next use to a stack slot at the stream position where pressure was observed. Rewritten labels target the start of any inserted reload/store prefix, and labels on a return target its inserted frame epilogue. Only a call whose target label is bound inside the same `Code` (an intra-Code self-call, which runs through the shared epilogue on return) reserves the caller's spill area again before continuing; a call to a label resolved externally by `Link` never runs this `Code`'s epilogue, so the rewriter must not re-reserve after it. `Assembler.Entry` documents the matching limitation for non-primary entries: only the primary entry (offset 0) runs the frame prologue, so `Build` returns `asm.ErrEntryRequiresFrame` when spilling occurs and a non-primary entry exists.

Linear spill state is unsafe across a loop back-edge, and mutation blocks can combine paths around state materialization. Two layers enforce safety:

- `asm/rewriter.go` rejects spilling for code containing an intra-code backward branch.
- `noSpill` scans every step in the completed plan, including learned continuations, and forbids spilling whenever `ARRAY_SET` or `STRUCT_SET` is present.

When a plan forbids spilling, the compiler wraps the target architecture in `noSpillArch`. Its `Frame()` returns `nil` according to the assembler contract, so register exhaustion rejects native compilation cleanly and threaded dispatch remains installed. Continuable primitive array stores therefore lower through an explicit state barrier and reuse dead registers rather than depending on spill slots.

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

Static plans recognize direct `CONST_GET function; CALL` pairs. Each interpreter owns a fixed-size `natives` slot array; installing or synchronizing a function entry publishes its executable address atomically. The caller loads the slot at runtime and uses `BLR`, so compile order does not matter: a null slot falls back at the CALL, while a later callee installation is visible without recompiling the caller. Self-recursion remains on the established trace self-call path, and `RETURN_CALL` remains threaded.

Native calls are frame-aware. The lowering checks frame budget, increments native depth, saves caller state, enters the callee trace, and restores caller state on return.

On deoptimization, native frames append enough journal records for Go to rebuild the VM call chain.

`RETURN` closes a function entry trace only when it returns from the outer recorded frame. Inlined callee returns stitch values back into the caller's symbolic stack.

Top-level module code has no synthetic `RETURN`. Falling off the end closes the module trace and writes live operands back to the VM stack.

## Branches

Recorded forward branches become guarded exits or learned branch continuations.

`BR_IF` and `BR_TABLE` emit the recorded path. Unrecorded targets deoptimize.

When a side exit becomes hot, the tracer records that target. A later compile may fold it into the same native callable as a pending block. Loop roots are never folded as ordinary continuations: they deoptimize at the header and use their standalone loop entry, which preserves back-edge and safepoint semantics.

Pending blocks reload from VM stack homes, run through a FIFO worklist, and stop at a bounded pending cap. The trace frontend orders learned roots once; the backend does not repeatedly sort pending work.

A cold branch edge may carry caller-continuation block IDs. The side trace body lowers first; on callee `RETURN`, lowering stitches the result into the caller frame and follows those IDs. The continuation reloads from VM stack homes before continuing.

Each deferred profiled edge gets its own label and symbolic snapshot. Static state-backed blocks share labels only through explicit block IDs, never through bytecode-anchor equality.

Solo interpreters recompile a side exit when its hit count first reaches the hot-exit threshold. Pooled interpreters also rearm on later threshold multiples so a peer can recover a missed shared-cache publication.

Targets still deoptimize when they are unknown or unsupported.

Branch lowering may skip hot-path flushes only when the branch state is clean. If locals or operands are dirty, flush first. Learned continuations and side exits must see the same stack image as threaded dispatch.

A committing flush (`selfCall`, `tailLoop`) transfers operand ownership to the VM stack, so it accepts a live non-raw ref: that ref already carries the retain taken when it was pushed, and committing hands the same edge to the stack, exactly as the inlined call path does when it stores arguments and drops them from the operand stack. A raw ref marker instead retains inside `box()` for an interpreter frame to release, and a commit rebuilds no frame, so a raw ref stays rejected. This is what lets a self-recursive function forward a ref parameter to itself on the native self-call path.

### Branch range validation

ARM64 conditional/compare/test branches (`B.cond`, `CBZ`/`CBNZ`, `TBZ`/`TBNZ`) encode a fixed-width signed PC-relative immediate — imm19 (±1MB) for `B.cond`/`CBZ`/`CBNZ`, imm14 (±32KB) for `TBZ`/`TBNZ`, imm26 (±128MB) for `B`/`BL`. `asm/arm64.Encoder.Encode` validates every such offset is 4-byte aligned and fits its field, returning `asm.ErrBranchOutOfRange` instead of silently masking an out-of-range offset into a wrong target. `interp/jit.go` `publish` treats `ErrBranchOutOfRange` the same as `asm.ErrNoRegistersAvailable`: it aborts native lowering for that trace and falls back to threaded dispatch rather than emit a corrupt callable. `asm.Link` can also return `ErrBranchOutOfRange` (from `asm/link.go`'s `patch`, which re-encodes relocations whose target lands outside the branch's field once resolved against another `Code`'s address) — `publish` checks `errors.Is(err, asm.ErrBranchOutOfRange)` at the `Link` call site too and routes it through the same clean fallback, rather than letting it propagate as a hard error the way an unrelated `Link` failure does.

Before that fallback triggers, `asm.Assembler.encode` runs a branch relaxation fixpoint (`asm.Relaxer`, implemented by `asm/arm64.arch.Relax`) between the draft and final encoding passes. Each pass drafts the current instruction list once, collects every intra-Code `B.cond`/`CBZ`/`CBNZ` label branch whose imm19 displacement does not fit, and rewrites all of them together into an inverted-condition branch that skips a following unconditional `B` (imm26, ±128MB) to the original target; it then re-drafts and repeats until a pass finds nothing left to relax. Both replacement instructions are constructed to already be in range, so a given branch relaxes at most once and the loop always terminates, and batching every out-of-range branch within a pass keeps the number of drafts proportional to the number of passes rather than the number of branches; if the unconditional `B` itself would not reach the target (>±128MB), `Relax` returns `false` and `ErrBranchOutOfRange`/the JIT fallback still applies. `TBZ`/`TBNZ` never carry a `LabelOperand` in this codebase (their offset is always a caller-computed immediate — see `asm/arm64/instr.go`), so they never reach `Relax` and the imm14 (±32KB) window has no relaxation path; architectures without a `Relaxer` (amd64) are unaffected — `encode` no-ops the pass.

## Loops

A loop root is anchored at a loop header: the target of a backward branch.

The tracer discovers headers statically. Periodic samples still drive normal hotness, while JIT-enabled unconditional backward `BR` handlers also notify the interpreter after moving the live frame to the exact target header. This prevents a deterministic tick phase from permanently missing those loops. Threshold-zero mode waits for eight exact hits on those headers before capture so the first iteration does not over-specialize the recorded branch path.

Loop lowering builds the normal native prologue, binds a back-edge label, lowers the loop body, commits loop-carried locals to VM stack homes, decrements `journalBudget`, and branches back while budget remains.

Loop-carried locals round-trip through the VM stack each iteration. This avoids a cross-back-edge register fixpoint.

Module loops at `addr == 0` are valid when their header IP is positive. Only loops whose header is the module or function entry (`ip == 0`) remain threaded.

Loop callables install at:

```go
i.code[addr][header]
```

The loop wrapper does not tear down the frame. It runs with the current frame live.

On yield trap, the wrapper deoptimizes to the header and runs one safepoint before redispatch. On fallback trap, it deoptimizes to the reported resume IP.

## Coroutine Suspension

`YIELD` and `RESUME` are suspension points. They cannot execute as normal linear native trace operations.

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

Ref-bearing slots use guarded retain/release through `journalRC`.

If a release may free the object (`rc == 1`), native code deoptimizes before the release. The interpreter owns recursive release and cleanup.

## Heap Reads and Mutations

ARM64 supports selected heap fast paths.

Native full-trace reads include observed shapes for scalar `REF_GET`, selected `ARRAY_LEN`, selected `ARRAY_GET`, selected `STRUCT_GET`, `ERROR_GET`, `CORO_DONE`, and `CORO_VALUE`.

Heap reads guard ref address, heap itab, array element kind, struct type pointer, struct field kind, index bounds, and release safety when needed.

Ref reads retain loaded refs. `CORO_VALUE` retains the value and releases the handle. `CORO_DONE` keeps the handle.

Heap-promoted `i64` values fall back before boxing.

A primitive typed-array `ARRAY_SET` may continue through native execution when it occurs in the anchor frame before any inlined call. Lowering first materializes one resumable pre-op snapshot, clears the local register cache, and lets shape, bounds, and release guards share that snapshot. Guard failure resumes at the original opcode; success performs the primitive store and continues to later operations or the loop back-edge.

Ref-bearing `ARRAY_SET` and `STRUCT_SET` remain terminal mutations. Their hot path may perform the store and resume threaded execution at the next instruction, while recursive release or any failed guard remains interpreter-owned.

Mutation plans are always no-spill. Leaf traces reuse pinned and dead registers for stack homes, heap cells, refcounts, and boxing scratch so primitive mutation loops fit the physical register bank. A mutation after inlined calls retains the terminal boundary; register exhaustion still rejects compilation cleanly instead of spilling across a back-edge (regression test: `interp.TestARM64_ArraySetAfterNestedCalls`).

Allocation and complex ref-bearing mutations stay threaded or terminate the native trace.

## Structured Errors

`ERROR_NEW`, `ERROR_CODE`, and `THROW` are terminal fallback boundaries.

The tracer records them without stepping the clone. Native code deoptimizes at the opcode IP, and the threaded handler performs error allocation, code extraction, throw unwinding, and handler landing.

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

## Tests

Run focused tests after JIT changes:

```bash
go test ./asm/... ./interp/...
```

Use this guide:

| Change | Test focus |
|---|---|
| ABI or callable behavior | `asm/assembler_test.go` |
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
- use short, standard names such as `trace`, `root`, `entry`, `loop`, `module`, `lowering`, `guard`, `exit`, `frame`, and `value`
- avoid adding an abstraction unless it removes real duplication or isolates real complexity

## Related Docs

- `docs/profile.md` — sampling, hotness thresholds, and JIT counters
- `docs/benchmarks.md` — benchmark results and methodology
- `docs/value-representation.md` — boxed values and kind semantics
- `docs/memory-model.md` — refs, ownership, and heap lifecycle
- `docs/instruction-set.md` — opcode semantics
- `docs/debugging.md` — bytecode-level mode that disables optimized execution
