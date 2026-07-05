# JIT Internals

Contracts for the native JIT in `interp/` and its interaction with `asm/`.

Read this before editing:

* `interp/jit.go`
* `interp/jit_arm64.go`
* `interp/trace.go`
* `asm` callable ABI code

## Summary

minivm always compiles bytecode to threaded closures first. The JIT is a lazy ARM64 trace backend layered on top of that portable threaded runtime.

```text
program.Program
  → threadedCompiler → []func(*Interpreter)   always available
  → Tracer           → trace snapshots        lazy runtime recording
  → compiler         → *module                lazy ARM64 native backend
```

The threaded interpreter is the source of correctness. Native code is an optimization and must always have a correct threaded fallback.

Default design rule:

* preserve threaded/JIT semantic parity
* prefer simple trace lowering over broad static compilation
* keep fallback behavior explicit
* keep architecture-specific code isolated
* use short, standard names
* if two designs behave the same, choose the simpler one

## Agent Fast Path

Before editing:

| Area                    | Check                     |
| ----------------------- | ------------------------- |
| Opcode shape            | `instr/type.go`           |
| Threaded behavior       | `interp/threaded.go`      |
| Boxing and value layout | `value-representation.md` |
| Refs and heap ownership | `memory-model.md`         |
| Ticks and thresholds    | `profile.md`              |
| Instruction semantics   | `instruction-set.md`      |

After editing:

| Change                   | Test                    |
| ------------------------ | ----------------------- |
| ABI or callable behavior | `asm/assembler_test.go` |
| trace recording          | `interp/interp_test.go` |
| native lowering          | `interp/interp_test.go` |
| install/wiring behavior  | `interp/interp_test.go` |

Run:

```bash
go test ./asm/... ./interp/...
```

## Execution Model

The dispatch table is:

```go
i.code[addr][ip]
```

Where:

* `addr` is the function slot
* `ip` is the bytecode offset
* each entry is a threaded closure or a wrapper around a native callable

A hot JIT attempt records a runtime trace from the current interpreter state. The ARM64 backend then emits native callables for usable roots:

| Root           | Meaning                    | Install point          |
| -------------- | -------------------------- | ---------------------- |
| module entry   | top-level program start    | `i.code[0][0]`         |
| function entry | function start             | `i.code[addr][0]`      |
| loop header    | hot backward-branch target | `i.code[addr][header]` |

Rejected traces emit nothing. The threaded closure remains installed.

Function entry callables tear down their frame on return. Module entry callables preserve the top-level frame and complete by advancing to the end of program code. Loop callables re-enter a live frame and must not unwind it.

## Solo and Pool JIT

Solo interpreters own a private `compiler` and `asm.Buffer`.

Pool interpreters use a shared `Cache`:

* trigger counts are atomic
* one winning interpreter compiles
* compiled modules publish immutable `asm.Callable`s
* each interpreter installs those callables into its own dispatch table at a safepoint

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

There is no boxed-shadow fallback lowerer. General behavior belongs to the threaded interpreter. Native guard failures materialize VM state into the journal and resume threaded execution.

## Tracer

Trace recording lives in `interp/trace.go`.

Recording clones the interpreter, starts the clone at the requested `(addr, ip)`, and executes threaded closures until one of these happens:

* return
* loop back-edge
* branch exit
* unsupported operation
* trace limit
* abort condition

The live interpreter is not mutated while recording.

Each recorded step stores the data needed for safe speculative lowering:

* opcode
* function and IP
* inline depth
* observed call target
* observed callee address
* observed guard values
* observed heap shape
* branch target and taken state
* selected heap values for read-only fast paths

The tracer aborts before host calls, allocation, and mutating heap operations. These remain interpreter-owned.

## Trace Snapshots

A pool shares one `Tracer`. Tree mutations are locked.

The compiler must not lower from a live mutable tree. `rootAt` returns `tree.snapshot()`, a shallow immutable view containing:

* root pointer
* copied branches
* copied hit counters

Published traces are fully built before they are installed. Sharing trace pointers across the snapshot boundary is safe.

## Lowerer

`jit.go` is architecture-neutral.

The architecture hook is intentionally small:

```go
type lowerer interface {
    lower(ctx *lowering) bool
}
```

`jit_arm64.go` provides:

* ARM64 construction
* constants
* offsets
* opcode lowering
* `arm64Lowerer.lower`

Other architectures use stubs, so JIT is unavailable.

Design rule: keep the lowerer interface small. Add architecture hooks only when the architecture-neutral lowering context cannot express the behavior cleanly.

## Trace ABI

Native callables use an AAPCS64-shaped entry.

`Callable.Call(ctx)` passes:

```text
&i.journal[0] in X0
```

Native code loads VM state from the journal into pinned scratch registers.

| Name             | ARM64 | Purpose               |
| ---------------- | ----- | --------------------- |
| `scratchStack`   | X10   | `&i.stack[0]`         |
| `scratchGlobals` | X11   | `&i.globals[0]`       |
| `scratchBP`      | X12   | current frame base    |
| `scratchSP`      | X13   | current stack pointer |
| `scratchCtrl`    | X14   | journal pointer       |

The Go trampoline preserves X19-X26 for native register allocation.

Native code does not marshal parameters or returns. It writes results and trap state into the journal, and the Go wrapper restores interpreter state from there.

## Frame Journal

`i.journal` is owned by `Interpreter`.

It is both:

1. input context for native entry
2. output state for deoptimization

Header cells come before fixed-stride frame records.

| Cell             | Purpose                                                    |
| ---------------- | ---------------------------------------------------------- |
| `journalStack`   | stack base pointer                                         |
| `journalGlobals` | globals base pointer                                       |
| `journalBP`      | current frame base                                         |
| `journalSP`      | stack pointer                                              |
| `journalDepth`   | number of written frame records                            |
| `journalCap`     | available frame record capacity                            |
| `journalTrap`    | `trapNone`, `trapFallback`, `trapOverflow`, or `trapYield` |
| `journalNextIP`  | fallback or resume IP                                      |
| `journalBudget`  | native loop back-edge budget                               |
| `journalActive`  | active native call depth                                   |
| `journalRC`      | refcount base pointer                                      |
| `journalUpvals`  | closure upvalue base pointer                               |
| `journalHeap`    | heap base pointer                                          |
| `journalHead...` | frame records `{addr, bp, ip, returns}`                    |

On guard failure, native code:

1. writes live stack state
2. appends frame records
3. sets `journalTrap = trapFallback`
4. sets `journalNextIP`
5. returns to Go

The Go wrapper rebuilds the VM state and resumes threaded execution.

If the fallback IP is `0`, the wrapper runs the shadowed threaded entry handler once to avoid immediate native re-entry.

## Speculation

Observed numeric and heap facts are speculative unless they come from bytecode constants.

Native code may specialize on observed values, but a mismatch must exit before the opcode executes. The threaded handler owns the general case.

This rule keeps native lowering small and safe.

## Calls and Returns

Native lowering supports selected calls:

* direct `CONST_GET function; CALL`
* guarded function-value calls
* eligible closure-body calls

A call may lower to native `BL` when the observed target is a JIT-eligible `*types.Function` with matching arity.

Unsupported targets fall back:

* host calls
* allocation
* heap mutation
* maps
* unsupported functions or closures

Native calls are frame-aware. The lowering:

1. checks frame budget
2. increments native depth
3. saves caller `bp` and `sp`
4. enters the callee trace
5. restores caller state on return

On deopt, native frames append enough journal records for Go to rebuild the VM call chain.

Frame overflow reports `trapOverflow`; the wrapper panics with `ErrFrameOverflow`.

`RETURN` closes a function entry trace only when it returns from the outer recorded frame. Inlined callee returns stitch values back into the caller’s symbolic stack.

Top-level module code has no synthetic `RETURN`. Falling off the end closes the module trace and writes live operands back to the VM stack.

## Branches

Recorded forward branches become either:

* guarded exits
* learned branch continuations

`BR_IF` and `BR_TABLE` emit the recorded path. Unrecorded targets deopt.

When a side exit becomes hot, the tracer records that target. A later compile may fold it into the same native callable as a pending block.

Pending blocks:

* reload from VM stack homes
* compile hotter exits first
* reuse one native label per learned `(function, IP)` target when safe
* stop at a bounded pending cap

Targets still deopt when they are unknown, unsafe, caller-tailed, or unsupported.

Branch lowering may skip hot-path flushes only when the branch state is clean: after consuming the condition, there must be no live operands and no dirty locals needed by later continuations.

If locals or operands are dirty, flush first. Learned continuations and side exits must see the same stack image as threaded dispatch.

## Loops

A loop root is anchored at a loop header: the target of a backward branch.

The tracer discovers headers statically. Once the function or module is hot, it records one iteration from the live state at the header.

Loop lowering:

1. builds the normal native prologue
2. binds a back-edge label
3. lowers the loop body
4. commits loop-carried locals to VM stack homes
5. decrements `journalBudget`
6. branches back while budget remains

Loop-carried locals round-trip through the VM stack each iteration. This avoids a cross-back-edge register fixpoint.

Loops whose header is the function entry (`ip == 0`) remain threaded.

Loop callables install at:

```go
i.code[addr][header]
```

The loop wrapper does not tear down the frame. It runs with the current frame live.

On `trapYield`, the wrapper deopts to the header and runs one safepoint before redispatch. On `trapFallback`, it deopts to the reported resume IP.

## Coroutine Suspension

`YIELD` and `RESUME` are true suspension points. They cannot execute as normal linear native trace operations.

For anchor-frame suspension:

* tracer records the opcode as a terminal
* native code emits an unconditional fallback at the opcode IP
* threaded dispatch performs the real suspend or resume exactly once

The resume IP is the opcode itself, not the next instruction, because the threaded handler advances `ip`.

Suspension inside an inlined callee aborts the trace. Deopt can rebuild inlined frames, but it does not restore their coroutine handle. Only the anchor frame can safely keep its coroutine state across deopt.

## Values

Scalars stay unboxed between native trace operations.

| Kind                | Native treatment                                             |
| ------------------- | ------------------------------------------------------------ |
| `i32`               | low 32 bits                                                  |
| `i1` / `i8`         | low 32 bits with narrow result kind preserved where required |
| `i64`               | full signed register value when inline-boxable               |
| `f32` / `f64`       | IEEE bit representation                                      |
| heap-promoted `i64` | deopt on load                                                |

Narrow kinds share the `i32` representation. Kind checks compare representation, so `i1` and `i8` can flow into `i32.*` lowering.

Result kinds must match the interpreter:

* `i32.and`, `or`, and `xor` preserve a shared narrow kind
* mixed narrow operands widen to `i32`
* other arithmetic widens to `i32`
* comparisons and `eqz` produce `i1`

## Slots and Refs

`GLOBAL_*`, `LOCAL_*`, and `UPVAL_*` lower for in-range static slots.

Scalar slots load and store raw values directly.

Ref-bearing slots use guarded retain/release through `journalRC`.

If a release may free the object (`rc == 1`), native code deopts before the release. The interpreter owns recursive release and cleanup.

## Heap Reads and Mutations

ARM64 supports selected heap fast paths.

Native full-trace reads include observed shapes for:

* scalar `REF_GET`
* selected `ARRAY_LEN`
* selected `ARRAY_GET`
* selected `STRUCT_GET`
* `ERROR_GET`
* `CORO_DONE`
* `CORO_VALUE`

Heap reads guard:

* ref address
* heap itab
* array element kind
* struct type pointer
* struct field kind
* index bounds
* release safety when needed

Ref reads retain loaded refs. `CORO_VALUE` retains the value and releases the handle. `CORO_DONE` keeps the handle.

Heap-promoted `i64` values fall back before boxing.

Primitive `ARRAY_SET` and `STRUCT_SET` are terminal mutations. The hot path may perform the store, flush state, and resume threaded execution at the next instruction. Shape, bounds, field-kind, or release failure deopts at the original opcode so the interpreter owns full semantics.

Allocation and complex ref-bearing mutations stay threaded.

## Exceptions

`ERROR_NEW`, `ERROR_CODE`, and `THROW` are terminal fallback boundaries.

The tracer records them without stepping the clone. Native code deopts at the opcode IP, and the threaded handler performs:

* error allocation
* code extraction
* throw unwinding
* handler landing

If any of these appears in an inlined callee frame, the trace aborts.

## Installation

Compiled modules install into the threaded dispatch table.

Entry wrappers and loop wrappers differ:

| Wrapper | Use                   | Frame behavior                  |
| ------- | --------------------- | ------------------------------- |
| `entry` | module/function entry | may complete or tear down frame |
| `loop`  | loop header           | re-enters live frame            |

Install only accepted callables. Rejected roots leave the existing threaded closure intact.

Native wrappers must always leave the interpreter in a valid state for threaded redispatch.

## Agent Notes

When changing JIT internals:

* keep the threaded interpreter correct first
* keep native lowering speculative and guarded
* deopt before executing behavior the JIT cannot fully own
* prefer one simple terminal fallback over partial duplicated semantics
* keep architecture-neutral code in `jit.go`
* keep ARM64 details in `jit_arm64.go`
* keep journal layout explicit and stable
* preserve interpreter/JIT stack and ref ownership symmetry
* use short, standard names such as `trace`, `root`, `entry`, `loop`, `module`, `lowering`, `guard`, `exit`, `frame`, and `value`
* avoid adding an abstraction unless it removes real duplication or isolates real complexity

The best JIT change is usually the smallest guarded native path that preserves exact threaded fallback semantics.
