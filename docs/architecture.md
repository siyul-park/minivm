# Architecture

This document describes minivm's package boundaries, ownership model, and execution flow.

## When to Read

Read this document when a change crosses package boundaries or touches runtime state, optimization, JIT, debugging, profiling, or bytecode verification.

For detailed behavior, follow the related topic docs instead of duplicating the same explanation here.

## Related Docs

| Area | Also read |
|---|---|
| Runtime state, heap ownership, refs | `memory-model.md`, `value-representation.md` |
| Opcode semantics | `instruction-set.md` |
| JIT internals | `jit-internals.md`, `profile.md` |
| Optimizer and analyses | `pass-system.md` |
| Static bytecode validation | `verification.md` |
| Host functions and marshaling | `host-integration.md` |
| Debugger and REPL | `debugging.md`, `guides/repl.md` |
| Platforms and build constraints | `compatibility.md` |

## Instruction Representations

An instruction exists in three forms, one per level. Each level owns exactly one complete operation vocabulary, and no operation is declared at two levels.

| Level | Form | Operation vocabulary |
|---|---|---|
| guest bytecode | `instr.Instruction` — bytes at an offset in a function body, decoded on demand and patched in place | `instr.Opcode`, owned by `instr`, which also owns every static fact about it |
| mid-level IR | `ssa.Operation` — a definition in a value graph, with no position, no width, and no encoding | `instr.Opcode` reused whole, carried in `Code`; `ssa.Op` adds only what the guest ISA cannot express |
| machine IR | `asm.Instruction` — a four-operand virtual-register row | the architecture package's own, opaque to `asm` (`arm64.Op`) |

`ssa.Op` earns an entry only when the guest ISA has no word for the operation:

- the opcode's identity is subsumed by the decoded operand the IR holds instead — `OpConst` by `Const`, `OpLoad` and `OpStore` by `Slot`
- the block graph's own control flow — `OpJump`, `OpBranch`, `OpTable`, `OpReturn`, `OpComplete`
- what only an optimizing compiler has — `OpGuardKind`, `OpGuardShape`, `OpGuardBounds`, `OpGuardValue`, `OpRetain`, `OpRelease`, `OpState`, `OpBridge`, `OpExit`, `OpSuspend`

Everything else is `OpExec` carrying an `instr.Opcode`, and `Code` is meaningful for exactly `OpExec` and `OpBridge`. Decoding a raw operand into a `Slot`, a `Const`, a `Shape`, or an edge belongs to the frontend that lowers bytecode into the IR, and nowhere else.

`OpComplete` carries the operand stack module code leaves behind in `Args`, exactly as `OpReturn` carries what a function hands back: running off the end of a module is how a program yields its results, so those operands are the module's result and not dead values. The IR does not carry an opcode's immediate operand, though: the frontend resolves what a declared-type index meant into a `Shape` or a fact, and a bridged opcode recovers its own operand from the bytecode at the IP its deopt state names. Nothing that reads the IR back out as bytecode can spell that operand again, so `transform.SSAPass` declines a function holding one.

## Boundary Rules

- `instr` should remain leaf-like: it owns opcode facts and learns nothing about SSA, guards, deoptimization, or the JIT.
- `internal/graph` must remain a leaf: no minivm imports at all.
- `internal/ssa` must not import `interp`, `internal/jit`, `internal/asm`, or any backend.
- `internal/ssa/transform` must not import `interp`, `internal/jit`, `internal/asm`, or any backend either: every pass it runs must stay correct over a function with no guard or deopt state at all, not only one a JIT frontend produced. It owns one transformation policy per pass, exactly as the top-level `transform` does for bytecode, and composes nothing itself; a caller builds its own `pass.Pipeline[*ssa.Function]`, and `optimize` owns the leveled, user-facing composition it runs over a program through `transform.SSAPass`.
- `internal/jit/frontend` must not import `interp`, `internal/asm`, or any backend: it reads a compile-time snapshot and emits SSA, and nothing else.
- `types` must not import `interp`.
- Optimizer code should flow through `pass.Pipeline` and `pass.Manager`.
- `program/verify.go` intentionally avoids importing `analysis` or `pass` to prevent dependency cycles.
- Architecture-specific native code should stay under `internal/asm/<arch>/` and `internal/jit/<arch>/`.

## Package Dependency Graph

Import direction: `A → B` means `A` imports `B`.

```text
types   → instr
program → instr, types
prof    → instr
internal/asm → internal/graph
internal/asm/amd64 → internal/asm
internal/asm/arm64 → internal/asm
internal/graph → (leaf)
internal/journal → (leaf)
internal/codegen → instr, types, jennifer, golang.org/x/tools/imports
internal/jit → instr, types, internal/asm, pass, analysis, prof
internal/jit/arm64 → instr, types, internal/asm, internal/asm/arm64, internal/jit, internal/journal, pass, analysis, prof
internal/jit/tier → internal/jit, prof
internal/ssa → instr, types, internal/graph
internal/ssa/transform → instr, types, internal/graph, internal/ssa, pass
internal/jit/frontend → instr, types, analysis, internal/jit, internal/ssa
internal/jit/compile → internal/asm, internal/jit, prof
interp  → program, instr, types, internal/asm, internal/asm/arm64, internal/jit, internal/jit/compile, internal/jit/tier, internal/journal, internal/jit/arm64, pass, analysis, prof
debug   → interp
analysis → pass, types, instr
transform → analysis, pass, types, instr, program, internal/ssa, internal/jit, internal/jit/frontend
optimize → transform, analysis, pass, program, internal/ssa, internal/ssa/transform
cli → debug, instr, interp, prof, program, types, cobra
cmd/minivm → cli
internal/cmd/codegen → internal/codegen
```

## Package Responsibilities

| Package | Responsibility |
|---|---|
| `program/` | bytecode, constants, types, handlers, builder, and verifier entry point |
| `instr/` | opcode definitions, encoding, parsing, formatting, and metadata |
| `types/` | VM values, type descriptors, boxed representation, arrays, structs, maps, strings, functions, closures, and errors |
| `interp/` | interpreter state, threaded dispatch, host APIs, coroutines, tracing, JIT arch selection, and pooling |
| `debug/` | bytecode-level debugger API |
| `prof/` | execution samples and JIT metrics |
| `internal/asm/` | architecture-neutral native-code interfaces, buffers, linking, and executable memory |
| `internal/graph/` | node-indexed directed-graph facts — dominance and natural loop headers — computed over a caller-supplied `Graph`, independent of any IR |
| `internal/asm/arm64/` | active ARM64 encoder, ABI bridge, and register conventions |
| `internal/asm/amd64/` | placeholder backend; does not emit native code yet |
| `internal/jit/` | architecture-neutral compiler: the plan graph, per-step dataflow facts, runtime layout tables, recorded-trace data, both frontends, and the driver that lowers a plan through a `Machine` into published native `Code` |
| `internal/jit/arm64/` | ARM64 `jit.Machine`: orchestration, opcode dispatch, control flow, numeric operations, calls and frames, deoptimization, heap access, and reference ownership |
| `internal/jit/frontend/` | the SSA frontends over one snapshot: `Static`, the forward fixpoint that resolves element kinds, field kinds, call targets, and dynamic arities from constants and declared types plus the block layout a bridged opcode splits; `Trace`, which resolves the same facts from what a recording observed, lays that recording out as a block chain, folds the continuations recorded at its hot exits in, and inlines the callees it entered; `Body`, the same bytecode translation over a whole function and against a `Module` alone, for the ahead-of-time optimizer, which holds no address to anchor a native entry at and no calling convention to honour; and the one operation-level translation all three drive |
| `internal/jit/tier/` | pure throughput/give-up retirement verdict for one installed native anchor (`Watchdog`); holds no interpreter state and never imports `interp` |
| `internal/ssa/` | SSA intermediate representation over minivm's value and opcode vocabulary: `Operation` nodes reusing `instr.Opcode`, block-parameter control flow, the interpreter-state value a deoptimization resumes into, speculation guards, explicit reference ownership, and the `Builder`, `Verify`, and `Format` that build, check, and print it; satisfies `internal/graph.Graph` |
| `internal/ssa/transform/` | one transformation policy per pass over `*ssa.Function` - constant folding, dominator-tree-scoped common subexpression elimination, redundant guard elimination, loop-invariant code motion over `internal/graph` dominance, and liveness-based dead code elimination; correct over a function with no guard or deopt state as well as one a JIT frontend produced, since it knows nothing of `internal/jit`; composes nothing itself, exactly as `transform/` does not compose for bytecode - a caller builds its own `pass.Pipeline[*ssa.Function]` |
| `internal/jit/compile/` | compile coordination shared by the interpreters running one program: the `Queue` that admits one build per function, coalesces the `Job`s raised for it, and decides whether that build runs on the claiming goroutine or on its own worker, plus the reference-counted `Store` of published `jit.Code` and its executable buffers; never imports `interp` |
| `internal/journal/` | frame-journal cell, record, and trap layout shared by the interpreter and native code |
| `internal/codegen/` | fusion pattern catalog, its validation, and the emitters that render `interp/threaded.go`; one file per opcode domain over a shared composition engine |
| `pass/` | generic analysis and transform infrastructure |
| `analysis/` | reusable static analyses |
| `transform/` | optimization transforms, including `SSAPass`, the bytecode-to-SSA-to-bytecode route that runs an SSA pipeline over each of a program's functions |
| `optimize/` | optimization pipeline wiring |
| `cli/` | command tree, run command, REPL, and value formatting |
| `cmd/minivm/` | executable entrypoint |

## Core Runtime Model

`program.Program` is the hand-off format between bytecode producers and the VM.

```go
type Program struct {
    Code      []byte
    Locals    []types.Type
    Constants []types.Value
    Types     []types.Type
}
```

`program.Builder` is the preferred construction API. It handles labels, branch offsets, constant and type interning, and stable pool indexes.

`interp.New` compiles bytecode to threaded dispatch closures. The threaded interpreter is the source of correctness. The JIT is an optimization layered on top of it and must always preserve threaded fallback behavior.

## Execution Flow

A typical execution follows this path:

```text
1. Build program
   └─ program.New(...) or program.Builder

2. Verify when input is not trusted
   └─ program.Verify(...)

3. Optimize when requested
   └─ optimize.New(level).Optimize(...)

4. Construct interpreter
   └─ interp.New(...)

5. Run
   ├─ threaded dispatch executes bytecode
   ├─ call and back-edge counters decide what is hot
   ├─ tick path handles context, fuel, hooks, samples, and a pool's shared-module
   │  handshake, and is skipped when none of them is attached
   └─ ARM64 JIT may compile hot traces

6. Close or reset
   └─ release runtime resources
```

`program.New` and `interp.New` trust their inputs. Use `program.Verify` before constructing an interpreter for bytecode loaded from external sources.

## Runtime State

`interp.Interpreter` owns execution state.

| State | Purpose |
|---|---|
| `instrs` | raw bytecode per function slot |
| `code` | threaded dispatch closures or native wrappers |
| `tracer` | trace recording |
| `entries` | hot-event count per function, the JIT tier-up trigger |
| `frames` | call stack |
| `stack` | operand stack |
| `heap`, `rc`, `free`, `trial`, `work` | heap storage, exact counts, reusable slots, and cycle-collection scratch |
| `globals` | global slots |
| `queue` | compile jobs, coalesced and admitted one build per function |
| `store` | published native code this interpreter installs from |
| `builds` | outcomes of the builds this interpreter claimed, parked until it adopts them |

Each `Interpreter` is single-goroutine-owned during use. A solo interpreter owns its `queue` and `store` privately and compiles inline, holding no extra goroutine and paying no poll for one; `Pool` lets multiple goroutines borrow separate interpreters that share one of each, and its queue serves every claimed build on a worker, so a build any of them wins runs off the interpreter that asked for it and is installed by all of them at a safepoint. Dispatch tables and installed wrappers stay interpreter-local either way.

## Key Invariants

### Heap and Values

- Heap index `0` is a permanent `Null` sentinel and is never reclaimed.
- Only `KindRef` values participate in reference counting.
- `release()` must stay iterative, never recursive.
- Heap indices are stable and must not move.
- Values that contain refs must implement `types.Traceable`.
- Large `i64` values may spill to the heap while preserving bytecode semantics.
- Strings carry no identity invariant: every comparison and every map keyed by a string compares content, so equal contents may occupy different refs. Only the constant pool deduplicates, at load time.
- A host value never owns a VM reference. Reading a field publishes one the caller owns, and writing one copies what the VM value holds into the Go field rather than storing the slot, so a host value implements no `types.Traceable` and reclaims nothing.
- A struct compiles to one VM struct type whatever form it takes, because a value and a pointer to it must report the same type. The pointer is what selects the live view; the value copies unless unexported fields make a copy unfaithful.
- A Go slice or map is a reference, so it is always a live view: an opcode that changes it writes through to Go memory, and an opcode that yields a new value rebuilds the view as the VM value a copy would have produced and works from that. A view addresses the Go variable, so a slice `append` reallocated is the one the next access reaches.

See `memory-model.md` and `value-representation.md` for the detailed rules.

### Frames

A frame separates the function/template address from the callable reference. `RETURN_CALL` replaces an activation only after the replacement owns forwarded arguments and the retiring activation has released all of its owned ref slots. JIT frame ownership is independent of register caching; uncached ref locals are still released from their VM stack slots.

| Field | Meaning |
|---|---|
| `addr` | function/template slot used for code, profiling, and JIT |
| `ref` | callable heap ref released on return |

For plain functions, `addr == ref`. For closures, `addr` points to the function template and `ref` points to the closure object. Every frame-creating `CALL` or fused path must set both fields, and non-closure paths must reset `upvals = nil`. `closure.new` takes the function ref from the stack top and transfers ownership of that ref plus the upvals into the closure.

### Threaded Dispatch

- Compile-time handlers advance `c.ip`.
- Runtime handlers advance `i.fr.ip`.
- Runtime traps panic internally and are recovered by `interp.Run`.
- Debugging with `WithDebugger` disables JIT and preserves bytecode instruction boundaries.

### JIT


The interpreter requests native compilation through `jit.Compiler.Compile(input, root)` only, handing it a snapshot it built rather than itself. The compiler runs the static and trace frontends internally; both produce the same flat, backend-neutral plan with block-ID edges, and installation depends only on the entry ABI kind.

- A compile reads only the snapshot, never live interpreter state: `jit.Input` carries constants, globals, and declared types that a loaded program fixes, immutable published traces, and `jit.Objects`, the heap addresses a plan can name resolved to immutable facts on the interpreter's own goroutine. That is what lets the compile itself run on a worker.
- Recording a trace, building the snapshot, and installing an entry stay on the interpreter's own goroutine; only `Compile` and the publish that follows it may run elsewhere.
- No build may still be running when the executable buffers it publishes into are freed: `Pool.Close` stops the worker before any holder detaches, and `Interpreter.Close` waits for the builds it claimed before dropping its own hold.
- Native code is speculative and guarded.
- Blocks with declared entry state carry no register state across edges; stack and dirty locals are materialized in VM memory.
- Native-call slots are fixed for an interpreter lifetime and published atomically on function-entry installation.
- Unsupported paths must fall back to threaded execution. A handler returns `true` only after lowering the opcode and advancing `s.ip` by its exact width; on type mismatch or unsupported lowering it returns `false` without mutating IR, stack, params, facts, or labels.
- JIT handlers must not duplicate complex interpreter behavior unless they can own all semantics.
- Guard failure materializes VM state and resumes threaded dispatch.
- ARM64 label branches are range-checked and relaxed only to replacements that are already in range; an unreachable target falls back to threaded execution.
- Spill frames use a stable base register, and every internal call resume point must restore the active spill-frame depth. The allocator (`internal/asm`) judges every spill by dominance plus two narrower rules — a loop-governed self-referencing redefinition, and a self-recursive call sharing the caller's frame instead of getting one of its own — rather than disabling the whole build's spill frame whenever a loop or a container store appears anywhere in it; see `jit-internals.md`'s Register Allocation section.
- A ref operand may be compiled deferred, borrowing its retain from backing storage. Every path handing the flushed operand stack to the interpreter must own or redeem it first, and a committing loop-backedge flush rejects any live deferred ref.

See `jit-internals.md` for trace recording, journal layout, calls, branches, loops, and fallback rules.

### Optimization

Optimization passes mutate `*program.Program` through the pass system.

Bytecode length changes must repair all position-sensitive data, including branch offsets and exception handler ranges. If repair cannot preserve behavior, the transform should leave the function unchanged.

`transform.SSAPass` re-emits a function outright rather than repairing it, which is the same rule taken to its limit: it lays the blocks out in the order the SSA holds them, computes every branch offset from the layout it produced, and declines the whole function - leaving it byte for byte as it was - when any offset no longer fits its signed 16-bit operand.

See `pass-system.md` for optimizer levels and rewrite rules.

## Focus Areas

| Area | Direction |
|---|---|
| JIT coverage | Expand reliable native lowering for hot numeric, call, and read-only heap paths |
| Architecture support | Keep ARM64 stable; add other backends only with clear user and benchmark value |
| Benchmarks | Keep benchmark claims tied to `docs/benchmarks.md` |
| Program format | Keep `instr` and `program` compact and Go-native |
| Host integration | Keep `HostFunction`, the host views, `Marshal`, and `Unmarshal` explicit about ownership |
| Resource policy | Keep context, fuel, hooks, stack, heap, frame limits, and host policy easy to reason about |

## Maintenance Notes

When changing architecture-level behavior:

- update the owning topic document rather than repeating details here
- keep package boundaries explicit
- preserve interpreter/JIT semantic parity
- keep public APIs small
- prefer local, simple ownership rules
- keep examples current with the code
