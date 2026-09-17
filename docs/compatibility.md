# Compatibility

Supported platforms and native backend availability.

## Matrix

| Platform | Threaded | AOT | ARM64 JIT |
|---|---:|---:|---:|
| Other Go-supported platforms | ✅ | ✅ | — |
| Darwin / ARM64 | ✅ | ✅ | ✅, CGO |
| Linux / ARM64 | ✅ | ✅ | ✅ |
| Darwin / x86-64 | ✅ | ✅ | — |
| Linux / x86-64 | ✅ | ✅ | — |

ARM64 is the only native JIT target. AMD64 JIT is not implemented.

The minimum Go version is the version declared in `go.mod`.

## Build

Architecture selection is in `interp/jit_arm64.go` and `interp/jit_stub.go`; normal builds need no manual tags.

Darwin/ARM64 JIT requires CGO for instruction-cache synchronization. Linux/ARM64 does not.

## Ownership

Executable memory and target mechanics belong to `internal/asm` and target packages. JIT contracts belong to `jit-internals.md`; opcode status belongs to `instruction-set.md`.

## Related

- `jit-internals.md`
- `instruction-set.md`
- `guides/add-architecture.md`
