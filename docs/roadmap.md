# Roadmap

minivm: Go-native programmable runtime for scripting, rules, DSLs, and plugin-style execution in Go services. Near-term work focuses on easier embedding, safer operation, and clearer evaluation before expanding compatibility or JIT scope.

## Current Focus

- Position clearly: custom bytecode VM for Go services with WebAssembly-inspired instruction set.
- Strengthen host embedding APIs: function registration, error behavior, runtime options.
- Improve guest execution controls beyond heap limits, cancellation, and failure reporting.
- Broaden benchmarks beyond numeric loops to justify default JIT thresholds with repeatable data.
- Keep interpreter/JIT behavior aligned as native segment coverage grows.

## Completed

- Added `WithMaxHeap`, `ErrHeapExhausted`, and heap-limit tests for safer embedded execution.
- Added `RuntimeError` with VM call frames while preserving `errors.Is` / `errors.As` cause checks.

## Near-Term Work

| Priority | Work | Why |
|---|---|---|
| P0 | Clarify positioning and safety boundaries | align expectations with Go-native embedding use cases |
| P1 | Broaden benchmark scenarios | validate JIT defaults across host calls, objects, and numeric loops |
| P2 | Expand host API examples | lower Go service adoption cost |
| P2 | Decide x86-64 JIT strategy | clarify performance expectations on common server targets |

## Future Expansion

- Add adapter example pairing minivm with external Wasm runtime for teams needing standard `.wasm`, WASI, or component-model workflows.
- Expand ARM64 JIT coverage for calls, globals, refs, and selected heap operations only when benchmarks justify.
- Add architecture-specific backends only when target users and benchmark coverage are clear.
- Share JIT code cache and aggregate profiling across `interp.Pool` interpreters when concurrent embedding becomes a measured bottleneck.
