# Compatibility

Platform support, CGO requirements, build constraints, and low-level portability notes for minivm.

## Summary

minivm is portable by default.

The threaded interpreter and optimizer work on all supported Go platforms. The JIT is ARM64-only. On Darwin/ARM64, JIT execution requires CGO for instruction-cache coherence.

Default rule for contributors and agents:

* keep platform-specific code small and isolated
* prefer simple stubs over scattered build-condition checks
* do not require manual build tags for normal use
* keep names short, standard, and consistent
* if behavior is the same, choose the simpler design

## Go Version

**Minimum: Go 1.26.2**

The minimum version is declared in `go.mod`.

The VM core uses only the Go standard library. The CLI and tests use small external dependencies such as `cobra` and `testify`.

## Platform Matrix

| Platform          | Threaded Interpreter | AOT Optimizer |       ARM64 JIT |
| ----------------- | -------------------: | ------------: | --------------: |
| Any OS / Any arch |                    ✅ |             ✅ |               — |
| Darwin / ARM64    |                    ✅ |             ✅ | ✅, CGO required |
| Linux / ARM64     |                    ✅ |             ✅ |               ✅ |
| Darwin / x86-64   |                    ✅ |             ✅ |               — |
| Linux / x86-64    |                    ✅ |             ✅ |               — |

On non-ARM64 platforms, only the threaded interpreter and optimizer are active.

JIT stubs compile cleanly but do not emit native code. `asm/amd64` is currently a placeholder: it preserves the generic `asm.Arch` shape, but its encoder and ABI return `asm.ErrNotImplemented`.

## CGO

CGO is optional except for **Darwin/ARM64 with JIT enabled**.

On Apple Silicon, JIT code is written to executable memory. Darwin requires explicit instruction-cache synchronization after sealing writable code pages. minivm performs this flush through CGO in `asm/icache_darwin_arm64.go`.

| Build                     |      CGO | Icache behavior                        |
| ------------------------- | -------: | -------------------------------------- |
| `darwin && arm64 && cgo`  | required | real flush via `__builtin_arm_isb(15)` |
| `darwin && arm64 && !cgo` | disabled | no-op flush; unsafe with JIT           |
| `linux && arm64`          | not used | no-op; kernel handles coherence        |
| other platforms           | not used | no-op; JIT inactive                    |

Building with `CGO_ENABLED=0` on Darwin/ARM64 is allowed, but safe only when JIT is disabled with `WithThreshold(-1)`. Otherwise, stale instruction cache state may cause intermittent `SIGILL`.

## Build Tags

Normal users should not set manual build tags. The Go toolchain selects the correct files from `GOOS`, `GOARCH`, and CGO state.

| Condition                       | Effect                                                   |
| ------------------------------- | -------------------------------------------------------- |
| `arm64`                         | enables ARM64 encoder, ABI, trampoline, and JIT lowering |
| `!arm64`                        | uses ARM64 stubs; ARM64 JIT lowering is not compiled     |
| `darwin && arm64 && cgo`        | enables real instruction-cache flush                     |
| `!darwin \|\| !arm64 \|\| !cgo` | uses no-op instruction-cache flush                       |
| `darwin \|\| linux`             | enables executable-memory mapping                        |
| `!darwin && !linux`             | executable-memory allocation returns `ErrMmapFailed`     |

Design rule: keep build tags at file boundaries. Avoid inline platform branching unless it is simpler and clearly local.

## JIT Support

The JIT is currently implemented for ARM64 only.

| Platform                              | JIT behavior                              |
| ------------------------------------- | ----------------------------------------- |
| Darwin/ARM64 with CGO                 | supported                                 |
| Linux/ARM64                           | supported                                 |
| ARM64 without required icache support | builds, but JIT may be unsafe or inactive |
| x86-64                                | not implemented                           |
| Windows / Plan9                       | not supported                             |

When no JIT backend is available, the interpreter remains usable. JIT-related paths fall back to stubs or fail only when executable memory is explicitly requested.

Use `WithThreshold(-1)` to disable JIT explicitly.

## Executable Memory

On Darwin and Linux, the JIT uses executable memory backed by `mmap`.

The memory flow is:

1. allocate anonymous private memory
2. write code while pages are writable
3. seal pages as executable
4. flush instruction cache when required
5. call generated code through the platform ABI bridge

On Darwin/Linux, minivm toggles pages between:

```text id="3azauu"
PROT_READ | PROT_WRITE
PROT_READ | PROT_EXEC
```

Platforms without executable-memory support use `asm/memory_stub.go`. In that case, `asm.NewBuffer` returns `ErrMmapFailed`.

## `unsafe` Usage

`unsafe` is limited to low-level packages that need direct memory, bytecode, or host-object access.

Current usage includes:

| Package/file                                  | Purpose                                        |
| --------------------------------------------- | ---------------------------------------------- |
| `asm/memory.go`                               | `mmap`, `mprotect`, executable memory access   |
| `asm/buffer.go`, `asm/link.go`, `asm/arch.go` | code patching and callable entry binding       |
| `asm/arm64/abi.go`                            | native ABI bridge                              |
| `instr/instr.go`                              | fixed-width bytecode operand loads             |
| `interp/threaded.go`                          | fast bytecode operand reads                    |
| `interp/interp.go`                            | scratch pointers passed to JIT code            |
| `interp/host.go`, `interp/marshal.go`         | host-object field access and reflection bridge |

There is no `unsafe` usage in:

```text id="meqih8"
types
program
pass
analysis
transform
optimize
```

Guest bytecode cannot use `unsafe` to escape the VM heap.

Design rule: keep `unsafe` local, obvious, and documented. Do not spread unsafe assumptions into higher-level packages.

## Windows and Plan9

Windows and Plan9 do not support JIT execution in minivm.

The threaded interpreter and optimizer still build and run normally. `asm/memory_stub.go` keeps packages that import `asm` buildable, but executable buffer allocation fails with `ErrMmapFailed`.

No special build setup is required.

## Module Stability

The following packages are public and follow semantic versioning:

```text id="shfr7l"
interp
types
instr
program
pass
analysis
transform
optimize
```

The following packages are low-level implementation details and may change without a major version bump:

```text id="fekwr2"
asm
asm/arm64
asm/amd64
```

## Agent Notes

When changing compatibility-sensitive code:

* prefer one clear platform boundary over many scattered checks
* add a stub for every architecture-specific implementation
* keep JIT unavailable paths buildable and predictable
* do not make normal users pass custom build tags
* keep public behavior stable even when backend support differs
* use short, standard names such as `memory`, `buffer`, `arch`, `flush`, `seal`, and `stub`
* do not introduce a new abstraction when a file-level build tag or small stub is enough

The simplest compatible design is usually best: portable by default, specialized only where required, and explicit at the boundary.
