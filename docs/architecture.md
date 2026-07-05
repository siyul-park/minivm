# Architecture

This document describes minivm’s main components, ownership boundaries, and execution flow.
Use it when a change crosses package boundaries or touches runtime state, optimization, JIT, debugging, or bytecode validation.

## Quick Map

| Area                                            | Also read                                    |
| ----------------------------------------------- | -------------------------------------------- |
| `interp/` runtime state, frames, globals        | `memory-model.md`, `value-representation.md` |
| `interp/` debugger API or bytecode stepping     | `debugging.md`, `profile.md`                 |
| `interp.Tracer` or profile options              | `profile.md`                                 |
| `interp/threaded.go` or `interp/jit*.go`        | `jit-internals.md`, `instruction-set.md`     |
| `analysis/`, `transform/`, `optimize/`, `pass/` | `pass-system.md`                             |
| `program/verify.go` or untrusted bytecode       | `verification.md`                            |
| `cli/` or `cmd/minivm/`                         | `guides/repl.md`                             |

Core boundary rules:

* `instr` should remain leaf-like.
* `types` must not import `interp`.
* Optimizer code should flow through `pass.Pipeline` and `pass.Manager`.
* `program/verify.go` intentionally avoids importing `analysis` or `pass` to prevent dependency cycles.

## Package Dependency Graph

Import direction: `A → B` means `A` imports `B`.

```text
types   → instr
program → instr, types
prof    → instr
asm/amd64 → asm
asm/arm64 → asm
interp  → program, instr, types, asm, asm/arm64, pass, analysis, prof
debug   → interp
analysis → pass, types, instr
transform → analysis, pass, types, instr, program
optimize → transform, analysis, pass, program
cli → debug, instr, interp, prof, program, types, cobra
cmd/minivm → cli
```

## Component Responsibilities

### `program/`

`program.Program` is the hand-off format between bytecode producers and the VM.

```go
type Program struct {
    Code      []byte
    Locals    []types.Type
    Constants []types.Value
    Types     []types.Type
}
```

Its fields define the executable bytecode, entry-frame locals, constant pool, and type descriptors used by runtime allocation instructions.

`program.Builder` is the preferred high-level API for constructing programs. It handles label patching, branch offsets, constant/type interning, and stable pool indices so callers do not need to compute byte offsets or pool positions manually.

### `instr/`

`instr` defines the instruction set and bytecode encoding.

It provides utilities to:

* serialize instructions with `Marshal`
* deserialize bytecode with `Unmarshal`
* format bytecode for debugging with `Format`
* parse textual instructions with `Parse` and `ParseAll`

Each opcode has metadata such as its mnemonic and operand widths.

### `types/`

`types` defines the VM’s value and type model.

The main abstractions are:

* `types.Value`: runtime values
* `types.Type`: type descriptors
* `types.Boxed`: the compact representation used by the VM stack and globals

Heap objects are represented as references. Objects that may contain nested references implement `types.Traceable`, allowing the collector to walk arrays, structs, maps, host objects, and other reference-carrying values.

### `interp/`

`interp` owns VM execution state.

Key state includes:

| Field      | Purpose                                          |
| ---------- | ------------------------------------------------ |
| `instrs`   | raw bytecode per function slot                   |
| `code`     | threaded dispatch closures                       |
| `tracer`   | trace and profile collection                     |
| `frames`   | call stack                                       |
| `stack`    | VM value stack                                   |
| `heap`     | heap object storage                              |
| `rc`       | reference counts                                 |
| `free`     | reusable heap indices                            |
| `globals`  | global slots                                     |
| `compiler` | private JIT compiler for standalone interpreters |
| `cache`    | shared native-code cache used by pools           |

The threaded compiler converts bytecode into dispatch closures. Common instruction patterns can be fused into superinstructions to reduce dispatch overhead, such as local/constant numeric operations or comparison followed by `BR_IF`.

The tracer records hot execution paths and profile data. The JIT uses this information to compile supported traces into native code.

The ARM64 JIT can lower numeric traces, direct calls, selected indirect calls, local/global accesses, and limited heap reads. Unsupported paths fall back to threaded execution through guards or deoptimization.

`HostFunction` wraps Go functions so they can be called from VM code. Go values are converted through `Marshal` and `Unmarshal`.

`Coroutine` implements `yield`/`resume` semantics. Entry-frame yields escape to the interpreter host, while function-local yields suspend and resume through coroutine handles.

`Pool` allows multiple goroutines to borrow interpreters while sharing JIT cache and profile data. Each individual `Interpreter` remains single-goroutine-owned.

### `debug/`

`debug` provides instruction-accurate execution control.

`Debugger` manages breakpoints, stepping, next, and finish behavior. It is installed through `interp.WithDebugger`. Debug mode disables JIT and uses tick-based hooks so execution stops on exact bytecode boundaries.

### `prof/`

`prof` collects execution samples and metrics.

* `Collector` stores interpreter-local samples.
* `Profiler` aggregates samples across shared tracers or pools.

Function, instruction pointer, opcode, and JIT metrics are exposed through the same reporting surface.

### `asm/`

`asm` is the low-level native code emission layer. It is independent of VM stack semantics and handles virtual registers, instruction emission, ABI boundaries, relocation, and linking.

The general flow is:

1. emit virtual-register instructions
2. allocate physical registers
3. encode machine code
4. patch relocations
5. link executable code

### `asm/arm64/`

`asm/arm64` provides the active native backend for minivm’s JIT.

It supplies the ARM64 encoder, ABI adapter, trampoline, and register conventions required to call generated native code from Go.

### `asm/amd64/`

`asm/amd64` is currently a placeholder. It preserves the generic `asm` API shape, but native encoding and ABI support are not implemented yet.

### `pass/`

`pass` provides the analysis and transform infrastructure, modeled after LLVM’s new pass manager.

The main pieces are:

* `pass.Manager`: lazily computes and caches analysis results
* `pass.Pipeline`: runs transform passes in order
* `Preserved`: describes which analysis results remain valid after a transform

Transforms request analyses through the manager, and invalidation happens after code mutation.

### `analysis/`, `transform/`, `optimize/`

These packages implement static analyses and optimization passes.

`BasicBlocksAnalysis` is the foundation for both the optimizer and JIT. Basic block boundaries are created at function entry, branch targets, after branch-like instructions, and after terminating instructions.

Optimization levels are cumulative:

```text
O1: ConstantFolding → ConstantDeduplication
O2: ConstantFolding → AlgebraicSimplification → ConstantDeduplication → DeadCodeElimination
O3: ConstantFolding → AlgebraicSimplification → GlobalValueNumbering → ConstantDeduplication → DeadCodeElimination
```

Transform passes mutate `*program.Program` directly and use `pass.Manager` for analysis results.

### `program/verify.go`

`program.Verify` validates bytecode before execution.

It checks opcode validity, operand ranges, branch targets, pool/local/upvalue indices, termination, stack balance, and type consistency where it can be determined statically.

Verification is separate from `interp.New`. Callers should explicitly verify untrusted bytecode before constructing an interpreter.

### `cli/`

`cli` contains the command-line interface, run command, REPL, and shared value formatting.

The REPL stores instruction history, constants, type descriptors, and filesystem access for `.load` and `.save`. Each step rebuilds a fresh `Program` and `Interpreter`, so heap references do not persist across REPL executions.

### `cmd/minivm/`

`cmd/minivm` is the thin executable entrypoint around `cli.Root().Execute()`.

## Execution Flow

A typical minivm execution follows this path:

```text
1. program.New(...)
   └─ build bytecode through instr.Marshal

2. program.Verify(...) [optional]
   └─ validate untrusted bytecode before execution

3. optimize.Optimize(...) [optional]
   └─ run AOT optimization passes

4. interp.New(...)
   ├─ compile top-level bytecode into threaded closures
   └─ compile function constants into function slots

5. interp.Run(ctx)
   ├─ execute the threaded dispatch loop
   ├─ periodically check context, fuel, hooks, and profiling
   └─ when a path becomes hot, tracer/JIT may compile it to native code

6. interp.Close()
   └─ release native buffers and runtime resources
```

`WithThreshold(0)` enables JIT on the first sample.
Negative thresholds disable JIT.
Debugging disables JIT to preserve exact bytecode-boundary behavior.

## Focus Areas

| Area                 | Direction                                                                                      |
| -------------------- | ---------------------------------------------------------------------------------------------- |
| JIT coverage         | Expand reliable native lowering for numeric traces, calls, guarded reads, and common hot paths |
| Architecture support | Keep ARM64 optimized; add AMD64 JIT only when justified by users and benchmarks                |
| Benchmarks           | Broaden coverage for numeric loops, host calls, and heap-object workloads                      |
| Program format       | Keep `instr` and `program` as a compact Go-native bytecode surface                             |
| Host integration     | Keep `HostFunction`, `Marshal`, and `Unmarshal` as the main Go integration path                |
| Resource policy      | Clarify how context, fuel, hooks, stack/heap/frame limits, and host policy work together       |

See `docs/roadmap.md` for current priorities.
