# VM Kernel Benchmarks

Runtime-neutral workloads for measuring minivm programs that combine multiple instructions.

## When to Read

Read when adding a VM workload, comparing execution tiers, or running cross-runtime measurements.

## Canonical Kernels

| Owner | Kernel | Signal |
|---|---|---|
| `BenchmarkControl_IterativeFib` | iterative Fibonacci | integer arithmetic, locals, loops, conditional branches |
| `BenchmarkControl_Sieve` | prime sieve | typed-array allocation, indexed mutation, nested loops, branches |
| `BenchmarkCall_RecursiveFib` | recursive Fibonacci | call frames, recursion, returns, stack growth |
| `BenchmarkCall_IndirectRecursiveFib` | indirect recursive Fibonacci | first-class function refs and recursive indirect calls |
| `BenchmarkCall_ClosureCounter` | closure counter | closure creation, captures, mutation, repeated calls |
| `BenchmarkMemory_TypedArraySum` | typed-array sum | array loads, accumulation, loop guards |
| `BenchmarkMemory_AllocationGraph` | allocation graph | reference allocation, linking, traversal, release, reuse |
| `BenchmarkNumeric_BranchTree` | branch tree | comparisons, skewed control flow, JIT guards |

Canonical fixture sizes are part of the benchmark contract: iterative Fibonacci 30, recursive Fibonacci 20 and 35, sieve 256, closure iterations 128, typed-array elements 256, allocation depth 128, and branch-tree nodes 96 with input 37.

Every fixture uses fixed input and has a correctness test with an exact result or graph checksum. Program construction, verification, result checks, reset, and JIT warmup stay outside execution-only timers.

## Modes

Every canonical kernel defines the same three minivm sub-benchmarks:

| Mode | Configuration | Boundary |
|---|---|---|
| `default` | no explicit interpreter options | standard adaptive runtime policy |
| `threaded` | `WithThreshold(-1)` | JIT disabled; pure threaded execution |
| `jit` | `WithThreshold(0)` | eager profiling/compilation policy |

Only the threshold changes between modes. `jit` does not assert that native code was emitted or entered; benchmarks that claim warmed native execution must prove that state separately with profiler metrics.

With the `compare` build tag, each kernel also adds the applicable external runtimes: native Go, wazero, Tengo, gopher-lua, Goja, gpython, and Yaegi. Wazero is omitted when no equivalent canonical WASM fixture exists.

## Commands

Canonical minivm kernels:

```bash
go test -run '^$' -bench='^(BenchmarkControl|BenchmarkCall|BenchmarkMemory|BenchmarkNumeric)' -benchmem ./...
```

Correctness:

```bash
go test ./...
```

Complete external comparison with three samples:

```bash
go test -tags=compare -run '^$' -bench='.' -benchmem -benchtime=300ms -count=3 ./...
```

External comparisons are informational. Parsing, compilation, module creation, and function lookup stay outside the timed loop where supported. They are excluded from canonical regression gates because runtime initialization, value models, and reset policies differ.

## Maintenance Notes

Keep inputs deterministic. Add a kernel only when it exposes a distinct VM signal. Do not add service-domain models, network state, mutable files, random seeds, or aggregate scores.

## Related Docs

- `../docs/benchmarks.md` - benchmark ownership, methodology, and historical measurements
- `../docs/instruction-set.md` - opcode semantics and JIT support
- `../docs/jit-internals.md` - trace and native execution lifecycle
