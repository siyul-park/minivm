# Add a JIT Architecture

Checklist for a new native backend.

`jit-internals.md` owns runtime contracts; this guide owns integration order.

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

`internal/jit` `MUST NOT` import an architecture package.

## Machine Layer

The agent `MUST` create `internal/asm/<arch>/` for register IDs, encoder, ABI bridge, callable adapter, and optional spill frame.

Callable adapter `MUST`:

1. receive `&i.journal[0]` as `ctx`;
2. pass `ctx` in the first integer argument register;
3. preserve allocator-selected callee-saved registers;
4. call native code and return to Go.

Native traces use the journal, not a VM argument/return ABI. `Arch.Frame()` returns `nil` without spill support; otherwise the agent `MUST` keep spill state private.

## JIT Layer

The agent `MUST` create `internal/jit/<arch>/` with target lowering and `MUST` keep one exported constructor. The constructor `MUST` return the concrete target type.

The target `MUST` own both its architecture selection and native lowering so `jit.New(target)` cannot combine different architectures. It implements `jit.Target`; the architecture-neutral compiler owns no target mechanics.

Lowering `MUST` return `false` before mutating state on unsupported opcode, kind, or heap shape. Guards `MUST` materialize live symbolic state.

Before returning to Go, lowering `MUST` commit `journal.CellSP`, `journal.CellNextIP`, and frame records. It `MUST` preserve return, call, frame, stack, ref, host, and write-barrier contracts.

The agent `MUST` use the ARM64 scratch layout:

| Slot | Value |
|---|---|
| `scratchStack` | `&i.stack[0]` |
| `scratchGlobals` | `&i.globals[0]` |
| `scratchBP` | frame base |
| `scratchSP` | interpreter SP |
| `scratchCtrl` | `&i.journal[0]` |

The agent `MUST` add `interp/jit_<arch>.go` for selection. It `MUST` extend `jit_stub.go` only when another real backend needs to carve out its architecture.

## Platform

The agent `MUST` update `compatibility.md` with GOOS/GOARCH, CGO, executable-memory, instruction-cache, and build-tag requirements. Normal builds `MUST NOT` need manual tags.

## Coverage

The agent `MUST` start with low-risk paths in this order:

1. `NOP`, `DROP`, `DUP`, `SWAP`
2. constants and `CONST_GET`
3. numeric arithmetic/comparison
4. numeric conversions
5. locals/globals
6. branches
7. entry `RETURN`
8. RC-neutral refs

The agent `MUST` add calls, ref-counted stores, heap access, loops, and suspension only after core lowering is stable.

## Validation

The agent `MUST` run:

```bash
go test ./internal/asm/<arch>/... ./internal/jit/<arch>/... ./interp/...
GOOS=linux GOARCH=<arch> go build ./...
```

A hot arithmetic workload `MUST` emit native code; the agent `MUST` verify with the existing JIT emission metric.

## Related

- `jit-internals.md`
- `compatibility.md`
- `instruction-set.md`
- `testing.md`
