# Roadmap

Planning document for project priorities. Topic docs own current contracts; this document does not override them.

## Direction

- simple embedding
- bounded execution
- threaded/JIT semantic parity
- measured performance
- small public APIs
- simple equivalent implementations

## Priorities

| Priority | Area | Goal |
|---|---|---|
| P0 | Runtime boundaries | Keep verification, execution, ownership, and fallback contracts explicit. |
| P1 | Host integration | Improve registration, conversion, errors, and examples. |
| P1 | Benchmarks | Measure host calls, heap objects, maps, strings, coroutines, and mixed workloads. |
| P1 | ARM64 JIT | Expand coverage only with correctness and benchmark evidence. |
| P2 | Execution policy | Keep cancellation, fuel, heap limits, and frame limits consistent. |
| P2 | Other architectures | Add a backend only with target demand and evidence. |

## JIT Expansion

Every native path requires:

1. threaded semantics and verifier coverage;
2. explicit fallback;
3. matching ownership;
4. success, guard-failure, and fallback tests;
5. reproducible benchmark evidence.

Prefer one guarded native path over duplicated interpreter semantics. Fall back before unsupported behavior executes.

## Docs

Roadmap owns priorities only. Benchmark evidence belongs to `benchmarks.md`; architecture, opcode, value, memory, JIT, host, and platform contracts belong to their topic owners.

## Related

- `architecture.md`
- `jit-internals.md`
- `benchmarks.md`
