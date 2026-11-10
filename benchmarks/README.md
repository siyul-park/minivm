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

The corpus contains 29 workloads.

| Runtime | Coverage | Notes |
|---|---:|---|
| Threaded | 29 | minivm interpreter with JIT disabled |
| JIT | 29 | minivm default execution |
| Native Go | 29 | direct compiled Go implementation |
| Wazero | 7 | exact WebAssembly implementations |
| Tengo | 27 | string-keyed maps only; `map-ops` and `coroutine-gen` have no Tengo equivalent |
| GopherLua | 26 | I32 workloads; exact I64 values are outside Lua's numeric model |
| Goja | 29 | I32 plus BigInt-based I64 fixtures |
| gpython | 29 | Python fixture; one case is intentionally unmeasured |
| CPython | 29 | same Python fixture through `python3.13`; one case is intentionally unmeasured |
| Yaegi | 29 | Go fixture; three cases are intentionally unmeasured |

A runtime cell is `—` only when no meaningful implementation exists or the benchmark is intentionally inapplicable. Yaegi skips `typed-array-sum` because its generic typed-array program is outside Yaegi's supported syntax, `recursive-fib-35` because the interpreted workload is impractical, and `coroutine-gen` because Yaegi's standard library has no `iter` package. gpython skips `map-ops` because its dicts accept only string keys. CPython skips `host-call` because it runs out of process and cannot call a Go function.

## Measurement

`make benchmark` measures every runtime and regenerates the result document. Only measurement time changes by level.

`make benchmark-diff base=<ref>` compares the `threaded` and `jit` tiers against `base` in alternating rounds, with `benchstat`. CI runs it on each branch against its merge base with `main`, and writes the table to the job summary. It reports and does not fail.

| Level | Target |
|---|---:|
| `quick` | 100 ms |
| `standard` | 300 ms |
| `deep` | 1 s |

`host-call` binds its Go function `mix` (`Spec.Host`) outside the timed loop: minivm wires it through `Interpreter.Marshal`, `Alloc`, and `SetGlobal` (the codec default path) into global 0, again after each `Reset`, which reseeds globals; every other in-process runtime uses its native function binding, and Yaegi calls `mix` as Go source.

Fixture loading, compilation, module construction, function lookup, and correctness checks stay outside the timed loop where the runtime permits it. CPython measures the repeated workload inside `perf_counter()` so process startup is excluded.

## Maintenance

Use one shared workload stem and one Go registry entry. Add every runtime fixture that can express the same deterministic algorithm and input without changing its computation. Keep expected-result calculation owned by the Go registration.

CI regenerates the result document on each same-repository pull request and commits it to the PR branch, so it lands with the PR's squash merge. Do not commit a table measured locally.

## Ownership

- `benchmarks/registry` owns benchmark metadata and source attachment.
- `benchmarks/fixtures` owns workload source and native implementations.
- `benchmarks/cmd/benchreport` owns conversion of benchmark output into `docs/benchmarks.md`.

## Related

- `../docs/benchmarks.md` — current benchmark results
- `../docs/writing.md` — Markdown document rules
- `../docs/testing.md` — test structure and validation
- `../docs/coding-patterns.md` — Go design and naming rules
