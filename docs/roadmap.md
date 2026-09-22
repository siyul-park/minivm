# Roadmap

Planning document for project priorities.

Topic docs own current contracts; this document `MUST NOT` override them.

## Direction

- simple embedding
- bounded execution
- threaded semantic parity; native execution improvements are evidence-driven
- measured performance
- small public APIs
- simple equivalent implementations

## Priorities

| Priority | Area | Goal |
|---|---|---|
| P0 | Runtime boundaries | Keep verification, execution, ownership, and fallback contracts explicit. |
| P1 | Host integration | Improve registration, conversion, errors, and examples. |
| P1 | Benchmarks | Measure host calls, heap objects, maps, strings, coroutines, and mixed workloads. |
| P1 | Native execution | Improve native execution only with correctness and benchmark evidence. |
| P2 | Execution policy | Keep cancellation, fuel, heap limits, and frame limits consistent. |
| P2 | Other architectures | Add a backend only with target demand and evidence. |

## Native Execution

Native execution `MUST` preserve threaded semantics, explicit ownership, test-first evidence, and reproducible benchmark evidence. Deferred native work belongs in tracked issues and does not change the current contract until implemented and verified.

## Docs

Roadmap owns priorities only. Benchmark evidence belongs to `benchmarks.md`; architecture, opcode, value, memory, JIT, host, and platform contracts belong to their topic owners.

## Related

- `architecture.md`
- `jit-internals.md`
- `benchmarks.md`
