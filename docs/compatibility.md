# Compatibility

Platform requirements, CGO usage, and build constraints for minivm.

## Go Version

**Minimum: Go 1.26.2**

Declared in `go.mod`. VM core uses only Go standard library. CLI and tests pull small module dependencies (`cobra`, `testify`).

## Platform Matrix

| Platform | Threaded Interpreter | AOT Optimizer | ARM64 JIT |
|---|---|---|---|
| Any OS / Any arch | ✅ | ✅ | — |
| Darwin / ARM64 | ✅ | ✅ | ✅ (CGO required) |
| Linux / ARM64 | ✅ | ✅ | ✅ (no CGO required) |
| Darwin / x86-64 | ✅ | ✅ | — |
| Linux / x86-64 | ✅ | ✅ | — |

On non-ARM64 platforms, only threaded interpreter and optimizer are active. JIT stubs compile without error but produce no native code. The `asm/amd64` package is a placeholder: it exposes the generic `asm.Arch` shape, but its encoder and ABI return `asm.ErrNotImplemented`.

## CGO

**CGO is optional except on Darwin/ARM64 with JIT enabled.**

On Darwin/ARM64, JIT writes native code into mmap'd executable memory. Apple Silicon enforces W^X and requires explicit instruction-cache flush (`__builtin_arm_isb`) after every `Seal()`. This flush is implemented via CGO in `asm/icache_darwin_arm64.go`.

| Build | CGO | Icache flush |
|---|---|---|
| `darwin && arm64 && cgo` | required | `__builtin_arm_isb(15)` via CGO |
| `darwin && arm64 && !cgo` | not used | no-op — **unsafe: stale icache can cause intermittent `SIGILL`** |
| `linux && arm64` | not used | no-op (Linux kernel handles coherence) |
| any other platform | not used | no-op (JIT not active) |

To build without CGO on Darwin/ARM64, set `CGO_ENABLED=0`. This compiles cleanly but disables icache coherence — only safe when JIT is also disabled via `WithThreshold(-1)`.

## Build Tags

| Tag | Effect |
|---|---|
| `arm64` | enables `asm/arm64/` encoder, ABI, trampoline; enables `interp/jit_arm64.go` handler table |
| `!arm64` | stubs out `asm/arm64/` with zero-value returns; `interp/jit_arm64.go` not compiled |
| `darwin && arm64 && cgo` | real icache flush in `asm/icache_darwin_arm64.go` |
| `!darwin \|\| !arm64 \|\| !cgo` | no-op icache stub in `asm/icache_noop.go` |
| `darwin \|\| linux` | real executable-memory mapping in `asm/memory.go` |
| `!darwin && !linux` | executable-memory allocation returns `ErrMmapFailed` through `asm/memory_stub.go` |

No manual build tags required for normal use. The Go toolchain selects correct files automatically based on `GOOS` and `GOARCH`.

## `unsafe` Usage

`unsafe` is used in a few low-level packages:

- `asm/memory.go`: `mmap`/`mprotect` syscalls and pointer access to executable memory on Darwin/Linux
- `asm/buffer.go`, `asm/link.go`, `asm/arch.go`, `asm/arm64/abi.go`: pointer arithmetic for native code patching and callable entry binding
- `instr/instr.go`, `interp/threaded.go`: fixed-width bytecode operand loads
- `interp/interp.go`: scratch pointers passed to native JIT code
- `interp/host.go`, `interp/marshal.go`: host-object field access and reflection bridge

No `unsafe` in `types/`, `program/`, `pass/`, `analysis/`, `transform/`, or `optimize/`. Guest bytecode cannot escape VM heap via `unsafe`.

## Executable Memory

JIT uses `syscall.Mmap` + `syscall.SYS_MPROTECT` directly on Darwin/Linux:

- **Darwin/Linux**: `MAP_ANON | MAP_PRIVATE`, toggle between `PROT_READ|PROT_WRITE` and `PROT_READ|PROT_EXEC`

Platforms without the Darwin/Linux mapping path (e.g. Windows, plan9) cannot use JIT. `asm.NewBuffer` returns `ErrMmapFailed` there, while threaded interpreter and optimizer code still compile with the non-ARM64 JIT stub.

## Windows / Plan9

JIT not supported. `asm/memory_stub.go` keeps packages that import `asm` buildable, but executable buffer allocation fails with `ErrMmapFailed`. Threaded interpreter and full optimizer pipeline work without restriction.

Build normally — JIT stubs compile cleanly and interpreter runs. `WithThreshold(-1)` effectively applies when no JIT backend is registered.

## Module Stability

`interp`, `types`, `instr`, `program`, `pass`, `analysis`, `transform`, and `optimize` packages follow semantic versioning. `asm/`, `asm/arm64/`, and `asm/amd64/` are internal; APIs may change without major version bump.
