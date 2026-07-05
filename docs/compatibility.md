# Compatibility

Platform support, CGO requirements, build constraints, and portability notes for minivm.

## When to Read

Use this document when changing platform-specific code, executable memory, instruction-cache flushing, build tags, public package support, or JIT backend availability.

For native backend internals, see `docs/jit-internals.md` and `docs/guides/add-architecture.md`.

## Summary

minivm is portable by default.

The threaded interpreter and optimizer work on all supported Go platforms. The JIT is ARM64-only. On Darwin/ARM64, JIT execution requires CGO for instruction-cache coherence.

Design rules:

- keep platform-specific code small and isolated
- prefer simple stubs over scattered build-condition checks
- do not require manual build tags for normal use
- keep backend support explicit

## Go Version

**Minimum: Go 1.26.2**

The minimum version is declared in `go.mod`.

The VM core uses only the Go standard library. The CLI and tests use small external dependencies such as `cobra` and `testify`.

## Platform Matrix

| Platform | Threaded Interpreter | AOT Optimizer | ARM64 JIT |
|---|---:|---:|---:|
| Any OS / Any arch | ✅ | ✅ | — |
| Darwin / ARM64 | ✅ | ✅ | ✅, CGO required |
| Linux / ARM64 | ✅ | ✅ | ✅ |
| Darwin / x86-64 | ✅ | ✅ | — |
| Linux / x86-64 | ✅ | ✅ | — |

On non-ARM64 platforms, only the threaded interpreter and optimizer are active.

JIT stubs compile cleanly but do not emit native code. `asm/amd64` is currently a placeholder: it preserves the generic `asm.Arch` shape, but its encoder and ABI return `asm.ErrNotImplemented`.

## CGO

CGO is optional except for Darwin/ARM64 with JIT enabled.

On Apple Silicon, Darwin requires explicit instruction-cache synchronization after writable code pages are sealed for execution. minivm performs this flush through CGO in `asm/icache_darwin_arm64.go`.

| Build | CGO | Icache behavior |
|---|---:|---|
| `darwin && arm64 && cgo` | required | real flush |
| `darwin && arm64 && !cgo` | disabled | no-op flush; unsafe with JIT |
| `linux && arm64` | not used | no-op; kernel handles coherence |
| other platforms | not used | no-op; JIT inactive |

Building with `CGO_ENABLED=0` on Darwin/ARM64 is allowed, but safe only when JIT is disabled with `WithThreshold(-1)`.

## Build Tags

Normal users should not set manual build tags. The Go toolchain selects the correct files from `GOOS`, `GOARCH`, and CGO state.

| Condition | Effect |
|---|---|
| `arm64` | enables ARM64 encoder, ABI, trampoline, and JIT lowering |
| `!arm64` | uses ARM64 stubs; ARM64 JIT lowering is not compiled |
| `darwin && arm64 && cgo` | enables real instruction-cache flush |
| `!darwin || !arm64 || !cgo` | uses no-op instruction-cache flush |
| `darwin || linux` | enables executable-memory mapping |
| `!darwin && !linux` | executable-memory allocation returns `ErrMmapFailed` |

Keep build tags at file boundaries. Avoid inline platform branching unless it is simpler and clearly local.

## Executable Memory

On Darwin and Linux, the JIT uses executable memory backed by `mmap`.

The memory flow is:

1. allocate anonymous private memory
2. write code while pages are writable
3. seal pages as executable
4. flush instruction cache when required
5. call generated code through the platform ABI bridge

Platforms without executable-memory support use `asm/memory_stub.go`. In that case, `asm.NewBuffer` returns `ErrMmapFailed`.

## Low-Level Code

Low-level packages may use direct memory or bytecode access when required.

Current areas include executable memory, code patching, ABI bridges, fixed-width bytecode reads, interpreter scratch state, and host-object field access.

There is no low-level direct memory use in `types`, `program`, `pass`, `analysis`, `transform`, or `optimize`.

Keep these assumptions local, obvious, and documented. Do not spread them into higher-level packages.

## Windows and Plan9

Windows and Plan9 do not support JIT execution in minivm.

The threaded interpreter and optimizer still build and run normally. `asm/memory_stub.go` keeps packages that import `asm` buildable, but executable buffer allocation fails with `ErrMmapFailed`.

No special build setup is required.

## Module Stability

Public packages that follow semantic versioning:

```text
interp
types
instr
program
pass
analysis
transform
optimize
```

Low-level implementation packages that may change without a major version bump:

```text
asm
asm/arm64
asm/amd64
```

## Maintenance Notes

When changing compatibility-sensitive code:

- prefer one clear platform boundary over many scattered checks
- add a stub for every architecture-specific implementation
- keep unavailable JIT paths buildable and predictable
- do not make normal users pass custom build tags
- keep public behavior stable even when backend support differs

## Related Docs

- `docs/jit-internals.md` — native backend contracts
- `docs/guides/add-architecture.md` — backend addition checklist
- `docs/benchmarks.md` — platform-specific benchmark context
