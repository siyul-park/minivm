# Compatibility

Supported platforms and native-tier availability.

## Matrix

| Platform | Threaded | AOT | Native |
|---|---:|---:|---:|
| Other Go-supported platforms | ✅ | ✅ | — |
| Darwin / ARM64 | ✅ | ✅ | planned |
| Linux / ARM64 | ✅ | ✅ | planned |
| Darwin / x86-64 | ✅ | ✅ | — |
| Linux / x86-64 | ✅ | ✅ | — |

ARM64 is the executable-memory and encoding target (`internal/asm`, `internal/asm/arm64`); no native tier is installed today. AMD64 has no encoder.

The minimum Go version is the version declared in `go.mod`.

## Build

Platform mechanics stay behind build constraints in `internal/asm` (`enter_arm64.s`, `icache_arm64.s`, `memory.go`, `memory_stub.go`); normal builds `MUST NOT` need manual tags or cgo.

## Ownership

Executable memory and target mechanics belong to `internal/asm` and target packages. Native-tier contracts belong to `jit-internals.md`; opcode status belongs to `instruction-set.md`.

## Related

- `jit-internals.md`
- `instruction-set.md`
- `guides/add-architecture.md`
