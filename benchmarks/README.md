# VM Benchmarks

Owns the benchmark fixture contract, registry, runtime matrix, and measurement commands for contributors adding or reviewing benchmark workloads.

## Fixtures

Each workload uses one flat, lower-kebab-case stem under `benchmarks/fixtures/`. Runtime source stays in its native language; Go registers the executable contract in `init()`, and the registry supplies source, expected result, native implementation, and Wazero metadata to the runners.

| Artifact | Purpose |
|---|---|
| `.mvm` | canonical minivm program text |
| `.go` | Native Go implementation and registry entry; also the Yaegi input |
| `.tengo` | Tengo fixture |
| `.lua` | GopherLua fixture |
| `.js` | Goja fixture |
| `.py` | gpython and CPython fixture |

## Registry

`benchmarks/registry` is the only benchmark execution index. `registry.All()` returns benchmarks in name order with all registered runtime sources attached. Benchmark runners and report generation do not inspect the fixture directory directly.

Each Go fixture registers its implementation directly:

```go
func init() {
	run := func() int32 { return iterativeFib(30) }
	registry.Register(registry.Spec{
		Name:   "iterative-fib",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}
```

Yaegi supplies the compiled `registry.Register` and dependency symbols, evaluates the same Go fixture source, captures the registered function, and benchmarks it. No AST rewriting or `Run...` adapter is used.

## Runtimes

The corpus contains 25 workloads.

| Runtime | Coverage | Notes |
|---|---:|---|
| Threaded | 25 | minivm interpreter with JIT disabled |
| JIT | 25 | minivm default execution |
| Native Go | 25 | direct compiled Go implementation |
| Wazero | 7 | exact WebAssembly implementations |
| Tengo | 25 | all workloads |
| GopherLua | 22 | I32 workloads; exact I64 values are outside Lua's numeric model |
| Goja | 25 | I32 plus BigInt-based I64 fixtures |
| gpython | 25 | Python fixture |
| CPython | 25 | same Python fixture through `python3.13` |
| Yaegi | 25 | Go fixture; two cases are intentionally unmeasured |

A runtime cell is `—` only when no meaningful implementation exists or the benchmark is intentionally inapplicable. Yaegi skips `typed-array-sum` because its generic typed-array program is outside Yaegi's supported syntax and `recursive-fib-35` because the interpreted workload is impractical.

## Measurement

`make benchmark` is the only benchmark command. Only measurement time changes by level.

| Level | Target |
|---|---:|
| `quick` | 100 ms |
| `standard` | 300 ms |
| `deep` | 1 s |

Fixture loading, compilation, module construction, function lookup, and correctness checks stay outside the timed loop where the runtime permits it. CPython measures the repeated workload inside `perf_counter()` so process startup is excluded.

## Maintenance

Use one shared workload stem and one Go registry entry. Add every runtime fixture that can express the same deterministic algorithm and input without changing its computation. Keep expected-result calculation owned by the Go registration.

Regenerate the result document with:

```bash
make benchmark
```

## Ownership

- `benchmarks/registry` owns benchmark metadata and source attachment.
- `benchmarks/fixtures` owns workload source and native implementations.
- `benchmarks/cmd/benchreport` owns conversion of benchmark output into `docs/benchmarks.md`.

## Related

- `../docs/benchmarks.md` — current benchmark results
- `../docs/writing.md` — Markdown document rules
- `../docs/testing.md` — test structure and validation
- `../docs/coding-patterns.md` — Go design and naming rules
