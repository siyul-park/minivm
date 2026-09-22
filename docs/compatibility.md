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

ARM64 is the native target. The runtime entry and icache paths are build-tagged in `internal/asm`; lowering is in `internal/jit/arm64`. AMD64 has no encoder or native tier.

The minimum Go version is the version declared in `go.mod`.

## Build

Platform mechanics stay behind build constraints in `internal/asm` (`enter_arm64.s`, `icache_arm64.s`, `memory.go`, `memory_stub.go`); normal builds `MUST NOT` need manual tags or cgo.

## Ownership

Executable memory and target mechanics belong to `internal/asm` and target packages. Native-tier contracts belong to `jit-internals.md`; opcode status belongs to `instruction-set.md`.

## Related

- `jit-internals.md`
- `instruction-set.md`
- `guides/add-architecture.md`
