# Add a JIT Architecture

Checklist for a new native backend. `jit-internals.md` owns runtime contracts; this guide owns integration order.

## Ownership

| Concern | Owner |
|---|---|
| machine interfaces | `internal/asm/` |
| reference backend | `internal/asm/arm64/`, `internal/jit/arm64/` |
| arch selection | `interp/jit_arm64.go`, `interp/jit_stub.go` |
| JIT driver | `internal/jit/` |
| frame journal | `internal/journal/` |
| trace recording | `interp/trace.go` |
| platform support | `compatibility.md` |

`internal/jit` must not import an architecture package.

## Machine Layer

Create `internal/asm/<arch>/` for register IDs, encoder, ABI bridge, callable adapter, and optional spill frame.

Callable adapter:

1. receives `&i.journal[0]` as `ctx`;
2. passes `ctx` in the first integer argument register;
3. preserves allocator-selected callee-saved registers;
4. calls native code and returns to Go.

Native traces use the journal, not a VM argument/return ABI. `Arch.Frame()` returns `nil` without spill support; otherwise keep spill state private.

## JIT Layer

Create `internal/jit/<arch>/` with target lowering. Keep one exported constructor:

```go
type lowerer struct{}
func New() jit.Machine { return lowerer{} }
```

Return `false` before mutating state on unsupported opcode, kind, or heap shape. Materialize live symbolic state on guards.

Before returning to Go, commit `journal.CellSP`, `journal.CellNextIP`, and frame records. Preserve return, call, frame, stack, ref, host, and write-barrier contracts.

Use the ARM64 scratch layout:

| Slot | Value |
|---|---|
| `scratchStack` | `&i.stack[0]` |
| `scratchGlobals` | `&i.globals[0]` |
| `scratchBP` | frame base |
| `scratchSP` | interpreter SP |
| `scratchCtrl` | `&i.journal[0]` |

Add `interp/jit_<arch>.go` for selection. Extend `jit_stub.go` only when another real backend needs to carve out its architecture.

## Platform

Update `compatibility.md` with GOOS/GOARCH, CGO, executable-memory, instruction-cache, and build-tag requirements. Normal builds should need no manual tags.

## Coverage

Start with low-risk paths:

1. `NOP`, `DROP`, `DUP`, `SWAP`
2. constants and `CONST_GET`
3. numeric arithmetic/comparison
4. numeric conversions
5. locals/globals
6. branches
7. entry `RETURN`
8. RC-neutral refs

Add calls, ref-counted stores, heap access, loops, and suspension after core lowering is stable.

## Validation

```bash
go test ./internal/asm/<arch>/... ./internal/jit/<arch>/... ./interp/...
GOOS=linux GOARCH=<arch> go build ./...
```

A hot arithmetic workload must emit native code; verify with the existing JIT emission metric.

## Related

- `jit-internals.md`
- `compatibility.md`
- `instruction-set.md`
- `testing.md`
