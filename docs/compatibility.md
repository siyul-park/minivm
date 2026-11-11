# Compatibility

Supported platforms, native-tier availability, and the stability of each public package, for embedders choosing what to depend on.

## Matrix

| Platform | Threaded | Optimizer | Native |
|---|---:|---:|---:|
| Other Go-supported platforms | ✅ | ✅ | — |
| Darwin / ARM64 | ✅ | ✅ | ✅ |
| Linux / ARM64 | ✅ | ✅ | ✅ |
| Darwin / x86-64 | ✅ | ✅ | — |
| Linux / x86-64 | ✅ | ✅ | — |

ARM64 is the native target. `internal/asm/arm64` owns encoding and the build-tagged entry/icache paths; `internal/jit/arm64` owns lowering. AMD64 has no encoder or native tier.

The minimum Go version is the version declared in `go.mod`.

## Packages

| Package | Stability | Use |
|---|---|---|
| `instr`, `types`, `program`, `interp` | stable | build, verify, and run bytecode; host integration |
| `debug`, `prof` | stable | debugger hook and profiling |
| `optimize` | experimental | `O0`–`O3` levels; `Optimizer.Add` runs a caller's `pass.Pass[*program.Program]` |
| `pass`, `analysis` | experimental | write program-level passes and query bytecode CFG facts through `pass.Manager` |
| `internal/...`, `cli`, `cmd` | none | implementation |

`types.TypedMap[K]` takes a key from the closed set `bool`, `int8`, `int32`, `int64`, `float32`, `float64` (`types.Scalar`); a string-keyed map is `types.StringMap`.

An experimental package `MAY` change its API in a minor release. SSA translation and SSA passes are internal (`internal/transform`, `internal/ssa`): an external pass works on `*program.Program`. No exported signature in a public package names an `internal/` type.

## Build

Platform mechanics are build-tagged in `internal/asm`; normal builds `MUST NOT` require manual tags or cgo.

Executable memory and target mechanics belong to `internal/asm`; native runtime contracts belong to `jit-internals.md`; opcode status belongs to `instruction-set.md`.

## Related

- `jit-internals.md`
- `instruction-set.md`
- `guides/add-architecture.md`
