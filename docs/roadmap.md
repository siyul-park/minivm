# Roadmap

minivm is a Go-native programmable runtime for scripting, rules, DSLs, and plugin-style execution inside Go services. The near-term direction is to make the existing VM easier to embed, safer to operate, and clearer to evaluate before expanding compatibility or JIT scope.

## Current Focus

- Keep the public position precise: custom bytecode VM for Go services, with a WebAssembly-inspired instruction set.
- Strengthen host embedding APIs, including function registration, error behavior, and runtime option documentation.
- Improve operational controls for guest execution, especially resource limits, cancellation, and failure reporting.
- Expand benchmark coverage beyond narrow numeric loops so default JIT thresholds are justified by repeatable data.
- Keep interpreter/JIT behavior aligned as more instructions become eligible for native segments.

## Near-Term Work

| Priority | Work | Why |
|----------|------|-----|
| P0 | Clarify positioning and safety boundaries | keeps expectations aligned with Go-native embedding use cases |
| P1 | Add resource-control guidance and tests | makes embedded execution safer for rule and plugin workloads |
| P1 | Broaden benchmark scenarios | validates JIT defaults across host calls, object operations, and numeric loops |
| P2 | Stabilize host API examples and error model | lowers adoption cost for Go service integration |
| P2 | Decide x86-64 JIT strategy | clarifies performance expectations on common server targets |

## Future Expansion

- Add an adapter example that pairs minivm with an external Wasm runtime for teams that also need standard `.wasm`, WASI, or component-model workflows.
- Increase ARM64 JIT coverage for calls, globals, refs, and selected heap operations only after benchmarks show the payoff.
- Add architecture-specific backends when a target platform has clear users and benchmark coverage.
