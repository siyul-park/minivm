# JIT Internals

Contracts for native JIT files in `interp/` and their interaction with `asm/`.
Read before editing `interp/jit.go`, `interp/jit_arm64.go`, `interp/trace.go`, or asm callable ABI code.

## Checklist

Before editing: check opcode width in `instr/type.go`; preserve threaded/JIT
parity; keep threaded fallback correct; read `profile.md` for ticks and
thresholds; read `value-representation.md` for boxing/unboxing; read
`memory-model.md` for refs and heap objects.

After editing: add or update tests in `asm/assembler_test.go` for callable ABI
changes and `interp/interp_test.go` for trace recording, native lowering, and
wiring changes; run `go test ./asm/... ./interp/...`.

## Execution Model

```text
program.Program
  -> threadedCompiler -> []func(*Interpreter)    always, portable fallback
  -> Tracer            -> trace tree snapshots     lazy runtime front-end
  -> compiler      -> *module               lazy ARM64 trace backend
```

The threaded compiler and native backend read the same bytecode. `i.code[addr][ip]`
is the primary dispatch table. A hot JIT attempt first records a runtime trace
from the current interpreter state, then the ARM64 backend publishes a native
callable for each usable root: the module or function entry (`ip == 0`) and any
hot loop header (`ip > 0`). A function entry callable installs at
`i.code[addr][0]` and tears the frame down on return; the top-level module entry
installs at `i.code[0][0]`, preserves its frame, and completes by advancing to
the end of the program code. A loop callable installs at `i.code[addr][header]`
and re-enters the live frame without unwinding it. Rejected traces emit nothing
and threaded dispatch remains installed.

Solo interpreters compile into a private `compiler` and private `asm.Buffer`.
Pool interpreters use a shared `Cache`: trigger counts live in atomics, the
winning member compiles with a throwaway compiler, and published modules share
immutable `asm.Callable`s. Each interpreter still binds those callables into its
own threaded closure table at a safepoint.

## compiler

`compiler` is private to `interp` and lives in `jit.go`. `Compile(i, addr, fn)`
is trace-only and emits every usable root anchored at `addr` (`emitRoot` per
anchor):

1. find the `Tracer` trace tree for each `(addr, ip)` anchor — module/function
   entry plus loop headers — skipping aborted roots, side-exit branches, and
   entry-anchored loops
2. build a `lowering` with typed symbolic operands, frames, constants,
   globals, heap view, and scratch registers (`loop` set for loop roots)
3. ask the architecture `lowerer` to emit the trace
4. link one callable into `module.entries[anchor]`, recording `module.loops[anchor]`
   so `install` chooses the entry vs loop wrapper

There is no static method pipeline, block planner, segment selection, or
boxed-shadow fallback lowerer. Runtime deoptimization still exists: guards
materialize VM state into the journal, return to the Go wrapper, and resume the
threaded interpreter at the reported fallback IP.

## Tracer

Trace recording and profile aggregation live in `interp/trace.go`; trace
compilation state lives in `interp/jit.go`. Recording clones the current
interpreter, points the clone at the requested `(addr, ip)` anchor, and executes
threaded closures until it reaches a return, loop, branch exit, unsupported
step, or trace limit. The live interpreter state is not mutated during
recording.

Each recorded instruction stores:

- opcode, function, IP, and inline depth
- observed call target box and callee address
- explicit branch target and branch-taken metadata
- selected observed heap values for guarded read-only heap fast paths

The `Tracer` aborts before host calls, allocation, and mutating heap operations.
Those operations remain interpreter-owned. Native guard fallbacks report their
live deopt state back to the local trace tree; after an exit counter reaches the
threshold, a solo interpreter recompiles the entry trace with the new branch
continuation.

## JIT Lowerer (`lowerer`)

`jit.go` is architecture-neutral. Its `lowerer` interface has one operation:

```go
type lowerer interface {
    lower(ctx *lowering) bool
}
```

`jit_arm64.go` provides ARM64 construction, constants, offsets, and the concrete
`arm64Lowerer.lower` implementation. On other architectures `jit_stub.go` returns a
nil `compiler`, so JIT is unavailable.

## Trace ABI

Native callables use a standard AAPCS64-shaped entry: `Callable.Call(ctx)` passes
`&i.journal[0]` in X0. External entries copy journal cells into pinned scratch
registers; recursive trace calls branch to an internal label after that reload
and keep the registers live. The Go trampoline preserves X19-X26 for native
register allocation. Native entry/return does not reserve its own SP frame; only
native self-calls save LR and the caller BP/SP around their internal `BL`, so the
assembler spill frame keeps a stable SP-relative base.

| Constant | ARM64 | Purpose |
| --- | --- | --- |
| `scratchStack` | X10 | `&i.stack[0]` input |
| `scratchGlobals` | X11 | `&i.globals[0]` input |
| `scratchBP` | X12 | current frame `bp` input |
| `scratchSP` | X13 | interpreter `sp` input |
| `scratchCtrl` | X14 | `&i.journal[0]` context pointer |

Native code loads locals and operands from `scratchStack`, keeps scalars in
registers, and writes stack results back on return or deopt. The `interp` entry
wrapper does not marshal params or returns; it calls `Callable.Call(ctx)` and
reads trap state from the journal.

## Frame Journal

`i.journal` is an Interpreter-owned `[]uint64` that both supplies entry context
and reports deopt state. Header cells precede fixed-stride frame records:

| Cell | Purpose |
| --- | --- |
| `journalStack` | `&i.stack[0]` or `0`; external entry in |
| `journalGlobals` | `&i.globals[0]` or `0`; external entry in |
| `journalBP` | current frame `bp`; external entry in |
| `journalSP` | interpreter `sp`; external entry in/out |
| `journalDepth` | trap-time frame records written; native read/write |
| `journalCap` | frame budget `len(i.frames)-i.fp`; read-only |
| `journalTrap` | `trapNone`, `trapFallback`, `trapOverflow`, or `trapYield` |
| `journalNextIP` | resume/fallback IP |
| `journalBudget` | back-edges left before the next safepoint |
| `journalActive` | active native call depth; X15 mirrors it |
| `journalRC` | `&i.rc[0]` or `0`; guarded refcount fast paths |
| `journalUpvals` | `&i.fr.upvals[0]` or `0`; closure-body upvalue fast paths |
| `journalHeap` | `&i.heap[0]` or `0`; read-only heap fast paths |
| `journalHead`... | records of `{addr, bp, ip, returns}`, stride 4 |

Guard fallbacks set `journalTrap = trapFallback`, write `journalSP`, append live
frame records, and return. The Go wrapper deopts from the journal, restores frame
metadata, and if the fallback IP is 0 runs the shadowed threaded entry handler
once to avoid immediate native re-entry.

## Calls And Returns

Recorded `CONST_GET function; CALL` and guarded function-value `CALL` sites can
lower to native `BL` when the observed target is a JIT-eligible `*types.Function`
with matching arity. Closure body calls can lower when the `Tracer` observed the
closure and the trace can recover its upvalue base. Host calls, allocation, heap
mutation, maps, and unsupported targets stay threaded through deopt.

Native calls are frame-aware. The call lowering checks frame budget against
`journalActive`, increments native depth, saves caller bp/sp on the host stack,
and enters the callee trace. Normal return restores bp/sp and receives return
values through the ABI registers and memory return slots. On deopt, each native
callee/caller appends enough frame state for the Go wrapper to rebuild the VM
call chain before threaded resume. Frame overflow reports `trapOverflow`, and the
wrapper panics with `ErrFrameOverflow`.

`RETURN` closes a function entry trace only when it returns from the outer
recorded frame. Top-level module code has no synthetic `RETURN`; falling off the
end of slot `0` closes the trace as a completed module entry and writes every
live operand back to the VM stack. Inlined callee returns stitch values back into
the caller's symbolic stack.

## Branches And Loops

Recorded forward branches become guarded exits or pending branch continuations.
`BR_IF` and `BR_TABLE` emit the recorded path and deopt on unrecorded targets.
When a side exit reaches the exit threshold, the tracer records that target and a
later compile folds the learned continuation into the same native callable as a
straight-line pending block. Pending blocks reload from VM stack homes written at
the branch, can enqueue further learned continuations, and reuse one native label
per learned target IP in the root. Targets that are still unknown, have unsafe
stack/frame shape, or touch unsupported operations continue to deopt through the
journal. This progressively widens branch-heavy traces without adding a separate
static method compiler.

A loop is anchored at its header — the target of a backward `BR`/`BR_IF`. The
safepoint discovers headers statically (`Tracer.headers` scans for back-edge
targets) and, once the module or function is hot, records one iteration from the
live state at the header (a stack-consistent point); the recorded trace's kind is
`loop`.
`emitRoot` lowers it with `loop` set: after the prologue it binds a back-edge
label, walks the body, and at the recorded back-edge commits loop-carried locals
to the VM stack, decrements `journalBudget`, and branches back while budget
remains. The loop-exit edge (a `BR_IF` falling through, or any guard) is a normal
side exit. Loop-carried locals round-trip through the VM stack each iteration —
the body reloads them at the header — so no cross-back-edge register fixpoint is
needed. Loops whose header is the function entry (`ip == 0`) are left to threaded
dispatch.

A loop callable installs at `i.code[addr][header]` behind the `i.loop` wrapper,
which (unlike `i.entry`) never tears the frame down: the header is reached
mid-function with the frame live. `i.loop` seeds `journalBudget` with `loopBudget`
(decoupled from `tick`, so a native iteration is not drowned in per-iteration
yields), runs the native loop, then on `trapYield` deopts to the header and runs
one safepoint before the Run loop re-dispatches the header callable; on a
`trapFallback` side exit it deopts to the resume IP for threaded dispatch.

## Coroutine Suspension

`YIELD` and `RESUME` are true suspension points: `suspend` snapshots the frame
into the `Coroutine` heap handle and unwinds to the caller, while `resume`
rebuilds a frame from a runtime-only stack image. Neither is representable in a
linear trace, so the JIT does not execute them natively. Instead the tracer
records a `YIELD`/`RESUME` reached in the trace's **anchor frame** as the trace's
terminal (`kind = returned`) without stepping it, and `walk` lowers it to an
unconditional `trapFallback` deopt at the op's own IP. After deopt the Run loop
re-dispatches that IP, so the threaded handler performs the real suspend/resume
exactly once (each handler advances `ip` itself, which is why the resume IP is
the op, not the next instruction). Native emits only the deopt — it never
touches the coroutine handle — so refcounts match threaded execution.

A suspension reached inside an **inlined callee** frame aborts the trace rather
than compiling: `deopt` rebuilds inlined frames from the journal records
(`addr`, `bp`, `ip`, `returns`) and never restores their `coro` field, so a
threaded `suspend` there would look up `heap[0]` and trap. Only the outermost
(anchor) frame keeps its `coro` across deopt, so only an anchor-frame suspension
is safe. This lifts the former blanket rejection: a hot loop whose hot back-edge
never yields now compiles, with the rare `RESUME`/`YIELD` as a clean side exit.

## Values, Refs, And Heap Reads

Scalars stay unboxed between trace opcodes. `i32` values use low 32 bits, `f32`
and `f64` use IEEE bits, and inline `i64` values use the full signed register
value while guards enforce the boxed 49-bit range before materialization. Heap
promoted `i64` values deopt on load.

`ARRAY_GET` and `STRUCT_GET` lower on ARM64 as full-trace heap reads for the
observed shape, so scalar/ref reads can feed later native ops instead of forcing
an immediate threaded resume. Native code guards the heap itab for that single
typed primitive-array, generic ref-array, or guest struct shape, checks the
index and field kind, performs the load, and continues through the trace.
Guard failures branch to an out-of-line side exit that resumes threaded dispatch
at the original opcode with the pre-op stack state flushed. Primitive array read
fast paths cover `i1`, `i8`, `i32`, `f32`, and `f64`; `ref` array/field reads
retain the loaded ref. `i64` reads remain terminal because heap-promoted i64
fallback needs the interpreter-owned boxing path.

Primitive `ARRAY_SET` and `STRUCT_SET` still lower as terminal heap mutations:
the hot path performs the store, flushes the result state, and resumes threaded
dispatch at the next instruction. Shape, bounds, field-kind, or refcount-release
failures deopt at the opcode so the interpreter owns the full handler semantics.

The narrow kinds `i1`/`i8` share the i32 representation, so they ride in the low
32 bits exactly like `i32` and `loadLocal`/`constGet` materialize them raw. Kind
checks are by representation (`kinds` compares `Kind.Repr`), so an `i1`/`i8`
operand flows into any `i32.*` lowering. Result kinds match the interpreter:
`i32.and`/`or`/`xor` use `i32Bitwise`, which keeps a shared narrow kind
(`i8 & i8 → i8`, `i1 ^ i1 → i1`) and widens a mixed pair to `i32`; other
arithmetic widens to `i32`; comparisons/`eqz`/`ref.test`/`ref.eq` go through
`setBool`, which pushes `KindI1`. `box` then tags each result `i8`/`i1`/`i32`
after masking the low lane.

`GLOBAL_*`, `LOCAL_*`, and `UPVAL_*` lower for in-range static slots. Scalar
slots load/store raw values directly; ref-bearing slots use `journalRC` guarded
retain/release. If a release might free (`rc == 1`), native deopts so the
interpreter owns recursive release.

Heap fast paths cover observed scalar `REF_GET`, selected typed
`ARRAY_LEN`/`ARRAY_GET`/`ARRAY_SET`, selected `STRUCT_GET`/`STRUCT_SET` shapes,
`ERROR_GET`, and the coroutine reads `CORO_DONE`/`CORO_VALUE`. They guard the
ref, heap itab, element/field kind, index, and release safety as needed;
`ERROR_GET` guards `*types.Error`, loads the boxed payload, retains a ref
payload, and releases the error handle. The coroutine reads guard the handle's
itab and load the `done` byte or the boxed `value` directly (`CORO_VALUE` retains
the value and releases the handle, `CORO_DONE` keeps it, matching the threaded
handlers). Heap allocation and complex ref-bearing mutations remain threaded.

`ERROR_NEW`, `ERROR_CODE`, and `THROW` are terminal fallback boundaries like
anchor-frame `YIELD`/`RESUME`: the tracer records them without stepping the
clone, and native code deopts at the opcode's own IP so the threaded handler
performs allocation, code extraction, throw unwinding, and handler landing. The
trace aborts if any appears in an inlined callee frame.
