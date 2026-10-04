# Compatibility

Supported platforms and native-tier availability.

## Matrix

| Platform | Threaded | AOT | Native |
|---|---:|---:|---:|
| Other Go-supported platforms | ✅ | ✅ | — |
| Darwin / ARM64 | ✅ | ✅ | ✅ |
| Linux / ARM64 | ✅ | ✅ | ✅ |
| Darwin / x86-64 | ✅ | ✅ | — |
| Linux / x86-64 | ✅ | ✅ | — |

ARM64 is the native target. `internal/asm` owns build-tagged entry/icache paths; `internal/jit/arm64` owns lowering. AMD64 has no encoder or native tier.

The minimum Go version is the version declared in `go.mod`.

## Build

Platform mechanics are build-tagged in `internal/asm`; normal builds `MUST NOT` require manual tags or cgo.

Executable memory and target mechanics belong to `internal/asm`; native runtime contracts belong to `jit-internals.md`; opcode status belongs to `instruction-set.md`.

## Related

- `jit-internals.md`
- `instruction-set.md`
- `guides/add-architecture.md`
