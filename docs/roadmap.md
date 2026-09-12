# Roadmap

Current priorities for minivm as a Go-native bytecode VM for embedded scripting, rules, DSLs, and plugins.

## Direction

minivm uses a custom bytecode format with a Go-first host API and an optional ARM64 JIT. The threaded interpreter remains the semantic baseline.

Priorities:

- simple embedding
- bounded and predictable execution
- interpreter/JIT semantic parity
- measured performance work
- small, stable public APIs
- simple implementations when behavior is equivalent

## Current Focus

| Priority | Area | Goal |
|---|---|---|
| P0 | Runtime boundaries | Keep verification, execution, ownership, and fallback contracts explicit. |
| P1 | Host integration | Improve registration, conversion, error handling, and examples. |
| P1 | Benchmarks | Measure host calls, heap objects, maps, strings, coroutines, and mixed workloads. |
| P1 | ARM64 JIT | Expand coverage only when correctness and benchmark value are clear. |
| P2 | Execution policy | Keep cancellation, fuel, heap limits, and frame limits consistent. |
| P2 | Other architectures | Add a backend only when target users and benchmark evidence justify it. |

## JIT Expansion

A new native path requires:

1. correct threaded semantics and verifier coverage;
2. explicit native fallback;
3. matching reference ownership;
4. success, guard-failure, and fallback tests;
5. reproducible benchmark evidence.

Prefer one guarded native fast path over duplicated partial interpreter semantics. When native code cannot fully own an operation, hand it back to threaded execution before the unsupported behavior runs.

## Performance

Benchmark claims belong in `docs/benchmarks.md` and must include reproducible before/after evidence. Do not optimize from aggregate scores alone; measure the workload that exercises the changed path.

## Documentation

Architecture, opcode semantics, value representation, ownership, JIT contracts, and host behavior are maintained in their owning topic documents. Roadmap text describes current priorities, not implementation history.

## Related Docs

- `benchmarks.md` — measured performance
- `jit-internals.md` — JIT contracts
- `host-integration.md` — embedding APIs
- `compatibility.md` — platform support
