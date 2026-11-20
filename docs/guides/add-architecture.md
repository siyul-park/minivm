# Add an Architecture

Checklist for contributors adding a target architecture: encoding, executable memory, the entry trampoline, and SSA lowering.

`compatibility.md` owns platform support; `instruction-set.md` owns opcode status; `jit-internals.md` owns the native tier's runtime contract.

## Ownership

| Concern | Owner |
|---|---|
| machine encoding | `internal/asm/` |
| reference encoder | `internal/asm/arm64/` |
| executable memory | `internal/asm/` |
| platform selection | `internal/asm/` build-tagged files |
| entry/exit trampoline | `internal/jit/enter_<arch>.{go,s}`; narrow the `!arm64` tag of `internal/jit/enter_noasm.go` |
| SSA lowering and register pins for a target | `internal/jit/<arch>/` implementing `compile.Machine` |
| host backend selection | `internal/jit/host/host_<arch>.go`; narrow the `!arm64` tag of `host_unsupported.go` |
| platform support | `compatibility.md` |

An architecture package MUST expose one concrete constructor when its behavior is consumed by another package.

## Machine Layer

The agent MUST create `internal/asm/<arch>/` for register IDs, instruction encoding, and branch relaxation. It MUST keep executable-memory ownership in `internal/asm/` rather than duplicating allocation or publication logic per architecture.

Encoders MUST accept the architecture-neutral instruction representation and MUST NOT own runtime state, interpreter frames, or compiler policy.

## Native Layer

`internal/jit/arm64` is the current native layer: it implements `compile.Machine` and keeps target lowering separate from the encoder in `internal/asm/arm64`, using the runtime contract `jit-internals.md` owns. A new architecture's lowering package `MUST` do the same, and its `Machine.Reserve` `MUST` hold every register the allocator must not assign: its `Ctx`, its own pinned and scratch registers, and any Go-reserved register the trampoline does not save.

The trampoline (`internal/jit/enter_<arch>.s`) `MUST` satisfy the `jit.State`, `jit.Enter` / `jit.Resume`, and exit-stub contracts in `jit-internals.md`, and its context register `MUST` be the lowering's `Ctx`.

A `compile.Machine` implementation MUST return the concrete target type from one exported constructor (see `arm64.New`) and MUST decline (return `false` from `Lower`) rather than emit incorrect code for an operation it does not support — `compile.Lower` bridges that operation into the interpreter through `ExitBridge` instead.

## Platform

The agent MUST update `compatibility.md` with GOOS/GOARCH, CGO, executable-memory, instruction-cache, and build-tag requirements. Normal builds MUST NOT need manual tags.

## Coverage

Add proof in dependency order:

1. registers/encoding;
2. constants/data movement;
3. arithmetic/comparisons;
4. conversions;
5. memory operands;
6. branches/relaxation;
7. executable-memory publication;
8. native entry.

Calls, host interaction, heap access, loops, and suspension require the core runtime contract first.

## Validation

The agent MUST run:

```bash
go test ./internal/asm/<arch>/... ./internal/jit/...
GOOS=linux GOARCH=<arch> go build ./...
GOOS=linux GOARCH=<arch> go test -exec=true ./...
```

Once the architecture has a `compile.Machine` lowering, the agent MUST also run its architecture-specific lowering tests and benchmark gates, following `internal/jit/arm64`'s golden tests as the pattern (see `testing.md` Native / JIT).

## Related

- `compatibility.md`
- `instruction-set.md`
- `jit-internals.md`
- `testing.md`
