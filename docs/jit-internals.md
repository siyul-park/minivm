# JIT Internals

Contracts for the ARM64 JIT in `interp/` and its interaction with `asm/`.

## When to Read

Use this document before changing `interp/jit.go`, `interp/jit_arm64.go`, `interp/trace.go`, `asm` callable ABI code, trace recording, lowering, deoptimization, loop safepoints, or JIT installation.

For user-facing performance results, see `docs/benchmarks.md`. For sampling and hotness thresholds, see `docs/profile.md`.

## Source of Truth

| Concern | File or doc |
|---|---|
| opcode semantics | `docs/instruction-set.md`, `instr/type.go` |
| threaded behavior | `interp/threaded.go` |
| trace recording | `interp/trace.go` |
| architecture-neutral compiler | `interp/jit.go` |
| ARM64 lowering | `interp/jit_arm64.go` |
| callable ABI | `asm/` |
| value layout | `docs/value-representation.md` |
| heap ownership | `docs/memory-model.md` |
| ticks and thresholds | `docs/profile.md` |

## Summary

minivm always compiles bytecode to threaded closures first. The JIT is a lazy ARM64 trace backend layered on top of that portable threaded runtime.

```text
program.Program
  -> threader -> []func(*Interpreter)   always available
  -> Tracer           -> trace snapshots        lazy runtime recording
  -> compiler         -> *module                lazy ARM64 backend
```

The threaded interpreter is the source of correctness. Native code is an optimization and must always have a correct threaded fallback.

Default rules:

- preserve threaded and JIT semantic parity
- prefer simple trace lowering over broad static compilation
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

The published native code is shared. The dispatch table remains interpreter-local.

## Compiler

`compiler` is private to `interp` and lives in `jit.go`.

`Compile(i, addr, fn)` is trace-only. It does not run a static method compiler or block planner.

For each usable root, it:

1. finds the trace tree for `(addr, ip)`
2. skips aborted roots, side exits, and entry-anchored loops
3. builds a `lowering`
4. calls the architecture `lowerer`
5. links the callable into `module.entries`
6. records loop roots in `module.loops`

General behavior belongs to the threaded interpreter. Native guard failures materialize VM state into the journal and resume threaded execution.

## Whole-CFG Baseline

Hot non-module functions first attempt a conservative whole-CFG compilation. Each verified basic block reloads its entry operands from canonical VM stack slots, keeps registers block-local, and flushes values before every edge. Native branches connect blocks directly; backward edges spend the journal safepoint budget. Unsupported opcodes return through an exact-IP fallback, while structural uncertainty rejects the baseline and leaves trace compilation available. A deoptimizing installed CFG is never rebuilt as the same CFG because the existing entry stub makes subsequent exit-triggered attempts trace-only.

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

The tracer aborts before host calls, allocation, and mutating heap operations. These remain interpreter-owned.

Every recorded `trace` carries a `kind outcome`: `loop`, `returned`, `completed`, `partial`, or `aborted`. `completed` and `returned` both mark a trace that ended cleanly (reached the top-level function's end, or returned from its recorded frame) and may lower as a normal native completion. `partial` always ends in its own recorded cut step, which the lowerer's `walk` handles explicitly before it can fall off the end of the fragment's ops. `aborted` means recording stopped on an unsupported op with no terminal step recorded at all; `tree.branchIPs()` excludes `aborted` branches from the set eligible for inlining as a continuation, and `arm64Lowerer.continuation` repeats that check as a direct safeguard. When `arm64Lowerer.walk` lowers a fragment (the root, or a pending branch continuation) and its ops run out without hitting an explicit terminal case, it checks that *fragment's own* `kind` — never the root's — before falling through as a normal completion; checking the root's kind instead would let a branch continuation that itself aborted after recording a few ops be lowered as if it had completed normally.

Both `observe` (the sampling safepoint) and `compile` (the one-shot JIT trigger) need an entry trace captured before compiling. They both route through `Tracer.ensureEntry`, the single owner of "capture the entry trace for this addr unless one is already recorded" — this avoids the entry being captured twice (once from each trigger, when both land close together), which would otherwise re-walk the function and inflate `tree.attempts`/blacklist counters with duplicate work.

`Tracer.headers` (the static loop-header scan) uses `instr.Targets(code, ip)` rather than switching on `BR`/`BR_IF` directly, so a loop formed only through a backward `BR_TABLE` case target is recognized as a header too.

## Trace Snapshots

A pool shares one `Tracer`. Tree mutations are locked.

The compiler must not lower from a live mutable tree. `rootAt` returns `tree.snapshot()`, a shallow immutable view containing root pointer, copied branches, and copied hit counters.

Published traces are fully built before they are installed. Sharing trace pointers across the snapshot boundary is safe.

## Lowerer

`jit.go` is architecture-neutral.

The architecture hook is intentionally small:

```go
type lowerer interface {
    lower(ctx *lowering) bool
}
```

`jit_arm64.go` provides ARM64 construction, constants, offsets, opcode lowering, and `arm64Lowerer.lower`. Short ARM64 trace fusions are written directly in that file beside ordinary lowering; `internal/cmd/geninterp` generates threaded-interpreter fusion only.

Other architectures use stubs, so JIT is unavailable.

Keep the lowerer interface small. Add architecture hooks only when the architecture-neutral lowering context cannot express the behavior cleanly.

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

Linear spill state is still unsafe across a loop back-edge, and terminal `ARRAY_SET`/`STRUCT_SET` traces can combine multiple branch paths before their exit. Two independent layers disable spilling for these cases, and both are needed:

- `asm/rewriter.go` disables spilling generically for any `Code` containing an intra-Code backward label branch (a loop back-edge), as a safety net that has no notion of VM opcodes. It cannot see terminal `ARRAY_SET`/`STRUCT_SET` mutations, which are a VM-opcode concept.
- `interp/jit.go`'s `spillSafe(tree *tree) bool` disables spilling for VM-opcode reasons the assembler cannot know about: it scans the *whole* trace tree — the root trace and every branch the tree may inline as a learned continuation, not just the root's own last op — for a terminal `ARRAY_SET`/`STRUCT_SET`. A learned continuation that itself ends in a mutation would otherwise escape a root-only check. `spillSafe` does not need to check for loop back-edges itself; the assembler's generic scan already covers that.

When `spillSafe` returns false, `emitRoot` wraps `c.arch` in a private `noSpillArch` (`interp/jit.go`) whose `Frame()` returns `nil` — the assembler's own documented contract for "no spilling" (`asm.Frame`'s doc comment) — rather than calling a dedicated `Assembler.DisableSpilling` API; no such API exists in `asm` because disabling spilling for this reason is purely an interp-side JIT policy decision. A root that exhausts physical registers then returns `asm.ErrNoRegistersAvailable` and keeps threaded dispatch installed; acyclic entry traces can still spill and emit native code.

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
| `journalHead...` | frame records `{addr, bp, ip, returns}` |

On guard failure, native code writes live stack state, appends frame records, sets trap state, sets the resume IP, and returns to Go.

The Go wrapper rebuilds the VM state and resumes threaded execution.

If the fallback IP is `0`, the wrapper runs the shadowed threaded entry handler once to avoid immediate native re-entry.

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

Unsupported targets fall back, including host calls, allocation, heap mutation, maps, unsupported functions, and unsupported closures.

Whole-CFG call sites recognize direct `CONST_GET function; CALL` pairs. Each interpreter owns a fixed-size `natives` slot array; installing or synchronizing a CFG entry publishes its executable address atomically. The caller loads the slot at runtime and uses `BLR`, so compile order does not matter: a null slot falls back at the CALL, while a later callee installation is visible without recompiling the caller. Self-recursion remains on the established trace self-call path, and `RETURN_CALL` remains threaded.

Native calls are frame-aware. The lowering checks frame budget, increments native depth, saves caller state, enters the callee trace, and restores caller state on return.

On deoptimization, native frames append enough journal records for Go to rebuild the VM call chain.

`RETURN` closes a function entry trace only when it returns from the outer recorded frame. Inlined callee returns stitch values back into the caller's symbolic stack.

Top-level module code has no synthetic `RETURN`. Falling off the end closes the module trace and writes live operands back to the VM stack.

## Branches

Recorded forward branches become guarded exits or learned branch continuations.

`BR_IF` and `BR_TABLE` emit the recorded path. Unrecorded targets deoptimize.

When a side exit becomes hot, the tracer records that target. A later compile may fold it into the same native callable as a pending block. Loop roots are never folded as ordinary continuations: they deoptimize at the header and use their standalone loop entry, which preserves back-edge and safepoint semantics.

Pending blocks reload from VM stack homes, compile hotter exits first, and stop at a bounded pending cap.

A branch pending block may carry the caller tail that remains after an inlined callee returns. The side trace body lowers first; on callee `RETURN`, lowering stitches the result into the caller frame and jumps to a shared pending suffix for that tail. The suffix reloads from VM stack homes before continuing, so different side traces can reuse the caller remainder without sharing registers.

Labels are reused for learned `(function, IP)` targets with no caller tail, and for shared caller-tail suffixes. Caller-tailed side traces keep distinct pending labels because the same bytecode target can need different caller remainders.

Solo interpreters recompile a side exit when its hit count first reaches the hot-exit threshold. Pooled interpreters also rearm on later threshold multiples so a peer can recover a missed shared-cache publication.

Targets still deoptimize when they are unknown or unsupported.

Branch lowering may skip hot-path flushes only when the branch state is clean. If locals or operands are dirty, flush first. Learned continuations and side exits must see the same stack image as threaded dispatch.

### Branch range validation

ARM64 conditional/compare/test branches (`B.cond`, `CBZ`/`CBNZ`, `TBZ`/`TBNZ`) encode a fixed-width signed PC-relative immediate — imm19 (±1MB) for `B.cond`/`CBZ`/`CBNZ`, imm14 (±32KB) for `TBZ`/`TBNZ`, imm26 (±128MB) for `B`/`BL`. `asm/arm64.Encoder.Encode` validates every such offset is 4-byte aligned and fits its field, returning `asm.ErrBranchOutOfRange` instead of silently masking an out-of-range offset into a wrong target. `interp/jit.go` `emitRoot` treats `ErrBranchOutOfRange` the same as `asm.ErrNoRegistersAvailable`: it aborts native lowering for that trace and falls back to threaded dispatch rather than emit a corrupt callable. `asm.Link` can also return `ErrBranchOutOfRange` (from `asm/link.go`'s `patchExternalRelocs`, which re-encodes relocations whose target lands outside the branch's field once resolved against another `Code`'s address) — `emitRoot` checks `errors.Is(err, asm.ErrBranchOutOfRange)` at the `Link` call site too and routes it through the same clean fallback, rather than letting it propagate as a hard error the way an unrelated `Link` failure does.

Before that fallback triggers, `asm.Assembler.encode` runs a branch relaxation fixpoint (`asm.Relaxer`, implemented by `asm/arm64.arch.Relax`) between the draft and final encoding passes. Each pass drafts the current instruction list once, collects every intra-Code `B.cond`/`CBZ`/`CBNZ` label branch whose imm19 displacement does not fit, and rewrites all of them together into an inverted-condition branch that skips a following unconditional `B` (imm26, ±128MB) to the original target; it then re-drafts and repeats until a pass finds nothing left to relax. Both replacement instructions are constructed to already be in range, so a given branch relaxes at most once and the loop always terminates, and batching every out-of-range branch within a pass keeps the number of drafts proportional to the number of passes rather than the number of branches; if the unconditional `B` itself would not reach the target (>±128MB), `Relax` returns `false` and `ErrBranchOutOfRange`/the JIT fallback still applies. `TBZ`/`TBNZ` never carry a `LabelOperand` in this codebase (their offset is always a caller-computed immediate — see `asm/arm64/instr.go`), so they never reach `Relax` and the imm14 (±32KB) window has no relaxation path; architectures without a `Relaxer` (amd64) are unaffected — `encode` no-ops the pass.

## Loops

A loop root is anchored at a loop header: the target of a backward branch.

The tracer discovers headers statically. Once the function or module is hot, it records one iteration from the live state at the header.

Loop lowering builds the normal native prologue, binds a back-edge label, lowers the loop body, commits loop-carried locals to VM stack homes, decrements `journalBudget`, and branches back while budget remains.

Loop-carried locals round-trip through the VM stack each iteration. This avoids a cross-back-edge register fixpoint.

Loops whose header is the function entry (`ip == 0`) remain threaded.

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

Primitive `ARRAY_SET` and `STRUCT_SET` are terminal mutations. The hot path may perform the store, flush state, and resume threaded execution at the next instruction. Shape, bounds, field-kind, or release failure deoptimizes at the original opcode so the interpreter owns full semantics.

A loop body that inlines two or more branchy calls before a terminal
`ARRAY_SET`/`STRUCT_SET` can push register pressure past the physical bank.
The allocator rejects spilling across backward edges, and the compiler disables
spilling for loop and terminal-mutation roots; register exhaustion then keeps
threaded dispatch installed (regression test:
`interp.TestInterpreter_JITArraySetAfterBranchyCallsInLoop`).

Allocation and complex ref-bearing mutations stay threaded.

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
- keep ARM64 details in `jit_arm64.go`
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
