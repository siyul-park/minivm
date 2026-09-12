# Compatibility

Supported platforms, build constraints, and native backend availability.

## Summary

- Threaded interpreter: portable across supported Go platforms.
- AOT optimizer: portable across supported Go platforms.
- ARM64 JIT: ARM64 only.
- Darwin/ARM64 JIT execution requires CGO for instruction-cache coherence.
- AMD64 native JIT is not implemented.

## Platform Matrix

| Platform | Threaded | AOT | ARM64 JIT |
|---|---:|---:|---:|
| Any supported OS / arch | ✅ | ✅ | — |
| Darwin / ARM64 | ✅ | ✅ | ✅, CGO |
| Linux / ARM64 | ✅ | ✅ | ✅ |
| Darwin / x86-64 | ✅ | ✅ | — |
| Linux / x86-64 | ✅ | ✅ | — |

## Go Version

The minimum Go version is the one declared in `go.mod`.

## Native Memory

Executable buffers are allocated and released through `internal/asm`. Published native code remains valid until all owners release it.

On ARM64, branch range is validated before encoding. If a safe long-branch form cannot be produced, native compilation falls back to threaded execution.

## Build Constraints

Architecture selection is isolated in `interp/jit_arm64.go` and `interp/jit_stub.go`. Normal builds do not require manual build tags.

## Related Docs

- `jit-internals.md`
- `guides/add-architecture.md`
