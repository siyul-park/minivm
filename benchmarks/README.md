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
| `BenchmarkMemory_PermutationFlips` | permutation flips | per-call boxed array allocation with in-place `array.get`/`array.set` |
| `BenchmarkMemory_StructTreeWalk` | struct tree walk | recursive `struct.new_default`/`struct.set` with `ref.cast` traversal |

Canonical fixture sizes are part of the benchmark contract: iterative Fibonacci 30, recursive Fibonacci 20 and 35, sieve 256, closure iterations 128, typed-array elements 256, allocation depth 128, branch-tree nodes 96 with input 37, permutation size 24 at depth 64, and struct-tree depth 9.

Every fixture uses fixed input and has a correctness test with an exact result or graph checksum. Program construction, verification, result checks, reset, and JIT warmup stay outside execution-only timers.

## Ported minipy Kernels

These translate `siyul-park/minipy`'s benchmark corpus (`conformance/testdata/benchmark/*.py`) instruction for instruction. minipy compiles Python to minivm bytecode, so its own numbers conflate its code generation with minivm's execution cost; hand-written kernels isolate the second. They are also the module's only f64, i64, and string coverage.

| Owner | minipy source | Signal |
|---|---|---|
| `BenchmarkNumeric_NBody` | `nbody.py` | pairwise f64 arithmetic, `f64.sqrt`, seven arrays through call parameters |
| `BenchmarkNumeric_SpectralNorm` | `spectralnorm.py` | f64 division, nested index loops, called evaluation functions |
| `BenchmarkNumeric_Mandelbrot` | `mandelbrot.py` | tight f64 loop with an early return |
| `BenchmarkNumeric_MatMul` | `matmul.py` | f64 multiply-accumulate over flat row-major arrays |
| `BenchmarkCall_NQueens` | `nqueens.py` | backtracking recursion over `i1` array state |
| `BenchmarkCall_Fannkuch` | `fannkuch.py` | recursive permutation search, `array.slice` copy per call, three i32 returns |
| `BenchmarkMemory_BinaryTrees` | `binarytrees.py` | struct allocation, recursive construction and checksum traversal |
| `BenchmarkMemory_SortStress` | `sortstress.py` | i64 modular arithmetic, in-place i32 array sort |
| `BenchmarkMemory_StringBuild` | `strbuild.py` | `string.new_utf32`, `string.concat`, `string.len`, `string.encode_utf32` |

`fib.py` needs no port: `BenchmarkCall_RecursiveFib` already is it.

Fixture sizes are part of the contract, reduced from minipy's ~1-3 s CPython target so each lands at 300-900 us/op under CPython 3.13: NBody 5 bodies over 100 `advance()` steps, spectral norm n=24 over 2 power iterations, Mandelbrot 16x16 at max_iter 50, matrix multiply n=16, N-Queens n=7, fannkuch n=6, binary trees min depth 4 and max depth 6, sort stress n=128 over 2 rounds, and string build 512 tokens.

Three of these produce a float checksum. Each projects it to an i32 by scaling and truncating, because Python's `int()`, Go's `int32()`, and `f64.to_i32_s` all truncate toward zero and agree exactly, so one number gates correctness across every compared implementation.

### Translation deviations

Two programs use minipy host builtins that minivm has no opcode for. Both are translated as the same computation rather than dropped, and both deviations are part of the kernel's contract:

- `sortstress` calls `xs.sort()`. The sort is written out in bytecode, so the kernel measures VM dispatch over an insertion sort rather than a builtin. **Every compared implementation runs that same insertion sort**; timing bytecode insertion sort against CPython's C Timsort would measure algorithm choice, not either interpreter. `n` is 128 rather than the source's 13000 to keep the quadratic sort inside the shared fixture band.
- `strbuild` ends with `upper`, `lower`, `replace`, `strip`, `count`, `find`, `startswith`, and `endswith`. minivm has none of them, so the port keeps token generation, the per-character checksum, and the concatenation accumulator, and drops the method chain. The expected checksum accounts for the omission.

## Modes

Every canonical kernel defines the same three minivm sub-benchmarks:

| Mode | Configuration | Boundary |
|---|---|---|
| `default` | no explicit interpreter options | standard adaptive runtime policy |
| `threaded` | `WithThreshold(-1)` | JIT disabled; pure threaded execution |
| `jit` | `WithThreshold(0)` | eager profiling/compilation policy |

Only the threshold changes between modes. `jit` does not assert that native code was emitted or entered; benchmarks that claim warmed native execution must prove that state separately with profiler metrics.

With the `compare` build tag, each kernel also adds the applicable external runtimes: native Go, wazero, Tengo, gopher-lua, Goja, gpython, CPython, and Yaegi. A runtime whose script is empty is skipped, so a kernel declares only the comparisons that answer a question about it; the ported minipy kernels declare `native`, `cpython`, and `gpython`, and wazero is omitted when no equivalent canonical WASM fixture exists.

CPython is not a Go library, so it runs as a subprocess: the driver spawns `python3.13` once, executes the workload `b.N` times inside a `time.perf_counter()` window, and reports that elapsed time, which excludes the interpreter's own ~13 ms startup. It checks the result against the expected checksum before timing, runs with `PYTHONHASHSEED=0`, and skips cleanly when `python3.13` is not on `PATH`. Because CPython is a pure interpreter, `threaded` is the minivm mode its ratio is meaningful against.

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

- `../docs/benchmarks.md` - current measurements, ownership, and methodology
- `../docs/instruction-set.md` - opcode semantics and JIT support
- `../docs/jit-internals.md` - trace and native execution lifecycle
