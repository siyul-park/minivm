# Roadmap

minivm: Go-native programmable runtime for scripting, rules, DSLs, and plugin-style execution in Go services. Near-term work focuses on easier embedding, safer operation, and clearer evaluation before expanding compatibility or JIT scope.

## Current Focus

- Position clearly: custom bytecode VM for Go services with WebAssembly-inspired instruction set.
- Strengthen host embedding APIs: function registration, error behavior, runtime options.
- Improve guest execution controls beyond heap limits, cancellation, and failure reporting.
- Broaden benchmarks beyond numeric loops to justify default JIT thresholds with repeatable data.
- Keep interpreter/JIT behavior aligned as native trace coverage grows.

## Completed

- Added a static bytecode verifier (`verify` package) with `interp.WithVerify` and CLI `run` integration: decode/operand-bounds, branch-target, termination, and best-effort typed operand-stack checks reject malformed bytecode before execution. See `verification.md`.
- Added `WithMaxHeap`, `ErrHeapExhausted`, and heap-limit tests for safer embedded execution.
- Added `RuntimeError` with VM call frames while preserving `errors.Is` / `errors.As` cause checks.
- Added shared JIT code cache and aggregate profiling for `interp.Pool`.
- Expanded ARM64 JIT coverage for direct and indirect bytecode calls, guarded ref-bearing slots, closure-body upvalues, and selected heap reads.
- Extended ARM64 JIT coverage to coroutines: `CORO_DONE`/`CORO_VALUE` lower as itab-guarded heap reads, and `YIELD`/`RESUME` record as anchor-frame trace terminals that deopt to the threaded suspend/resume, so a hot loop that yields only on a rare branch now compiles instead of being rejected wholesale.
- Replaced the method JIT with a trace JIT and added loop-anchored compilation (native back-edge with a safepoint poll), beating the former method JIT across the fib and issue #60 benchmarks.

## Near-Term Work

| Priority | Work | Why |
|---|---|---|
| P0 | Clarify positioning and safety boundaries | align expectations with Go-native embedding use cases |
| P1 | Broaden benchmark scenarios | validate JIT defaults across host calls, objects, and numeric loops |
| P2 | Expand host API examples | lower Go service adoption cost |
| P2 | Decide x86-64 JIT strategy | clarify performance expectations on common server targets |

## Future Expansion

- Add adapter example pairing minivm with external Wasm runtime for teams needing standard `.wasm`, WASI, or component-model workflows.
- Extend ARM64 JIT coverage only with matching correctness tests and benchmark scenarios for the new workload shape.
- Add architecture-specific backends only when target users and benchmark coverage are clear.
