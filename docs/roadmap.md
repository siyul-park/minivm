# Roadmap

minivm is a Go-native programmable runtime for scripting, rules, DSLs, and plugin-style execution inside Go services.

The near-term focus is simple embedding, bounded execution, clear evaluation, and measured JIT growth. Compatibility and backend expansion should follow real users, tests, and benchmarks.

## Direction

minivm is not trying to replace WebAssembly. It is a custom bytecode VM for Go services, with a WebAssembly-inspired instruction set and Go-first host integration.

Product and engineering principles:

- keep embedding simple
- keep runtime behavior safe and predictable
- keep interpreter and JIT semantics aligned
- expand JIT coverage only with tests and benchmark evidence
- prefer small, clear APIs over broad surfaces
- prefer simple designs when behavior is equivalent
- use short, standard, consistent names

## Current Focus

| Area | Focus |
|---|---|
| Positioning | Explain minivm as a Go-native bytecode VM for embedded scripting, rules, DSLs, and plugins |
| Host embedding | Improve function registration, value conversion, error behavior, and runtime options |
| Execution bounds | Strengthen cancellation, fuel, heap limits, and failure reporting |
| Benchmarks | Broaden coverage beyond numeric loops to host calls, heap objects, maps, strings, and mixed workloads |
| JIT | Grow ARM64 trace coverage only where correctness and benchmark value are clear |
| Docs | Keep architecture, instruction semantics, memory rules, and embedding guidance connected and current |

## Completed

- Added static bytecode verification with `program.Verify`.
- Added safer embedded execution controls such as `WithHeapLimit` and `ErrHeapExhausted`.
- Added structured runtime failures with `RuntimeError`, VM call frames, and preserved `errors.Is` / `errors.As` cause checks.
- Added shared execution infrastructure for pools, including shared JIT cache and aggregate profiling.
- Expanded ARM64 JIT coverage for direct calls, selected indirect calls, ref-bearing slots, closure-body upvalues, and selected heap reads.
- Improved coroutine/JIT interaction for `CORO_DONE`, `CORO_VALUE`, and anchor-frame suspension fallback.
- Replaced the method JIT with a trace JIT, including loop-anchored compilation and native back-edge safepoints.

Detailed implementation behavior belongs in the topic docs, especially `jit-internals.md`, `profile.md`, and `benchmarks.md`.

## Near-Term Work

| Priority | Work | Why |
|---|---|---|
| P0 | Clarify positioning and execution boundaries | Set correct expectations for Go-native embedding use cases |
| P1 | Broaden benchmark scenarios | Validate JIT thresholds and runtime tradeoffs on realistic workloads |
| P1 | Improve host embedding examples | Make adoption easier for Go services |
| P2 | Refine runtime control APIs | Keep cancellation, fuel, heap limits, and errors consistent |
| P2 | Decide x86-64 JIT strategy | `asm/amd64` is currently a placeholder; backend work needs users and benchmarks |

## Benchmark Priorities

Add repeatable workloads for:

- host function calls
- Go value marshal/unmarshal
- arrays, structs, maps, and strings
- coroutine and iterator usage
- mixed interpreter/JIT execution
- small scripts typical of rules or DSL engines
- long-running loops with cancellation and fuel checks

Performance claims should include:

```text
before: ...
after:  ...
conclusion: ...
```

## JIT Expansion Rules

Expand native JIT coverage only when all are true:

1. threaded behavior is already correct and tested
2. verifier and instruction semantics are clear
3. native fallback behavior is safe
4. ref ownership matches the interpreter
5. benchmarks show the workload matters
6. tests cover success, guard failure, and fallback paths

Prefer one guarded native fast path over duplicated partial semantics.

If the JIT cannot fully own an operation, deopt before the operation and let the threaded handler run it.

## Future Expansion

- Add an adapter example showing how to pair minivm with an external Wasm runtime for teams that need standard `.wasm`, WASI, or component-model workflows.
- Extend ARM64 JIT coverage for new workload shapes when correctness tests and benchmarks justify it.
- Add architecture-specific backends only when target users and benchmark coverage are clear.
- Improve documentation and examples around embedding, execution policy, and host integration.
- Keep public APIs small and stable as the project matures.

## Maintenance Notes

When updating the roadmap:

- keep it short and decision-oriented
- separate completed work from planned work
- avoid promising backend work without users and benchmarks
- prefer concrete priorities over broad aspirations
- keep embedding and measurement ahead of speculative JIT expansion
- preserve the project rule that equivalent behavior should use the simplest clear design

## Related Docs

- `docs/benchmarks.md` — current benchmark evidence
- `docs/jit-internals.md` — JIT implementation status and constraints
- `docs/host-integration.md` — embedding guidance
- `docs/compatibility.md` — platform and backend support
