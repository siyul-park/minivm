# Roadmap

minivm is a Go-native programmable runtime for scripting, rules, DSLs, and plugin-style execution inside Go services.

The near-term focus is simple embedding, safe execution, clear evaluation, and measured JIT growth. Compatibility and backend expansion should follow real users, tests, and benchmarks.

## Direction

minivm is not trying to replace WebAssembly. It is a custom bytecode VM for Go services, with a WebAssembly-inspired instruction set and Go-first host integration.

Default product and engineering principles:

* keep embedding simple
* keep runtime behavior safe and predictable
* keep interpreter and JIT semantics aligned
* expand JIT coverage only with tests and benchmark evidence
* prefer small, clear APIs over broad surfaces
* prefer simple designs when behavior is equivalent
* use short, standard, consistent names

## Current Focus

| Area           | Focus                                                                                                              |
| -------------- | ------------------------------------------------------------------------------------------------------------------ |
| Positioning    | Explain minivm as a Go-native bytecode VM for embedded scripting, rules, DSLs, and plugins                         |
| Host embedding | Improve function registration, value conversion, error behavior, and runtime options                               |
| Safety         | Strengthen execution limits, cancellation, heap limits, and failure reporting                                      |
| Benchmarks     | Broaden coverage beyond numeric loops to host calls, heap objects, maps, strings, and mixed workloads              |
| JIT            | Keep ARM64 trace coverage growing only where correctness and benchmark value are clear                             |
| Docs           | Keep architecture, instruction semantics, memory rules, and embedding guidance easy for agents and users to follow |

## Completed

* Added static bytecode verification with `program.Verify`.

  * Validates decoding, operand bounds, branch targets, termination, and best-effort typed operand-stack consistency.
  * Keeps per-opcode stack effects on `instr.Type`.
  * Runs before `interp.New`, not inside it.
  * Integrated into CLI `run`.

* Added safer embedded execution controls.

  * `WithMaxHeap`
  * `ErrHeapExhausted`
  * heap-limit tests

* Added structured runtime failures.

  * `RuntimeError`
  * VM call frames
  * preserved `errors.Is` / `errors.As` cause checks

* Added shared execution infrastructure for pools.

  * shared JIT code cache
  * aggregate profiling
  * pool-level profile reporting

* Expanded ARM64 JIT coverage.

  * direct bytecode calls
  * selected indirect calls
  * guarded ref-bearing locals/globals/upvalues
  * closure-body upvalues
  * selected heap reads

* Improved coroutine/JIT interaction.

  * `CORO_DONE` and `CORO_VALUE` lower as guarded heap reads.
  * anchor-frame `YIELD` and `RESUME` record as terminal deopts.
  * hot loops can still compile when yield/resume is on a rare branch.

* Replaced the method JIT with a trace JIT.

  * added loop-anchored compilation
  * native back-edge with safepoint polling
  * improved fib and issue #60 benchmark results

## Near-Term Work

| Priority | Work                                      | Why                                                                             |
| -------- | ----------------------------------------- | ------------------------------------------------------------------------------- |
| P0       | Clarify positioning and safety boundaries | Set correct expectations for Go-native embedding use cases                      |
| P1       | Broaden benchmark scenarios               | Validate JIT thresholds and runtime tradeoffs on realistic workloads            |
| P1       | Improve host embedding examples           | Make adoption easier for Go services                                            |
| P2       | Refine runtime control APIs               | Keep cancellation, fuel, heap limits, and errors consistent                     |
| P2       | Decide x86-64 JIT strategy                | `asm/amd64` is currently a placeholder; backend work needs users and benchmarks |

## Benchmark Priorities

Add repeatable workloads for:

* host function calls
* Go value marshal/unmarshal
* arrays, structs, maps, and strings
* coroutine and iterator usage
* mixed interpreter/JIT execution
* small scripts typical of rules or DSL engines
* long-running loops with cancellation and fuel checks

Performance claims should include:

```text id="xzrqer"
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

* Add an adapter example showing how to pair minivm with an external Wasm runtime for teams that need standard `.wasm`, WASI, or component-model workflows.
* Extend ARM64 JIT coverage for new workload shapes when correctness tests and benchmarks justify it.
* Add architecture-specific backends only when target users and benchmark coverage are clear.
* Improve documentation and examples around embedding, safety policy, and host integration.
* Keep public APIs small and stable as the project matures.

## Agent Notes

When updating the roadmap:

* keep it short and decision-oriented
* separate completed work from planned work
* avoid promising backend work without users and benchmarks
* prefer concrete priorities over broad aspirations
* keep safety, embedding, and measurement ahead of speculative JIT expansion
* preserve the project’s core design rule: same behavior should use the simplest clear design

The roadmap should help contributors choose the next smallest useful change, not list every possible feature.
