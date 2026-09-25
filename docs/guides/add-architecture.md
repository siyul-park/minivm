# Add an Architecture

Checklist for adding architecture-specific executable-memory and encoding support.

`compatibility.md` owns platform support; `instruction-set.md` owns opcode status; `jit-internals.md` owns the native tier's runtime contract.

## Ownership

| Concern | Owner |
|---|---|
| machine encoding | `internal/asm/` |
| reference encoder | `internal/asm/arm64/` |
| executable memory | `internal/asm/` |
| platform selection | `internal/asm/` build-tagged files |
| SSA lowering for a target | `internal/jit/<arch>/` implementing `compile.Machine` |
| platform support | `compatibility.md` |

An architecture package MUST expose one concrete constructor when its behavior is consumed by another package.

## Machine Layer

The agent MUST create `internal/asm/<arch>/` for register IDs, instruction encoding, and branch relaxation. It MUST keep executable-memory ownership in `internal/asm/` rather than duplicating allocation or publication logic per architecture.

Encoders MUST accept the architecture-neutral instruction representation and MUST NOT own runtime state, interpreter frames, or compiler policy.

## Native Layer

`internal/jit/arm64` is the current native layer: it implements `compile.Machine` and keeps target lowering separate from the encoder in `internal/asm/arm64`, using the runtime contract `jit-internals.md` owns. A new architecture's lowering package MUST do the same.

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
go test ./internal/asm/<arch>/...
GOOS=linux GOARCH=<arch> go build ./...
GOOS=linux GOARCH=<arch> go test -exec=true ./...
```

Once the architecture has a `compile.Machine` lowering, the agent MUST also run its architecture-specific lowering tests and benchmark gates, following `internal/jit/arm64`'s golden tests as the pattern (see `testing.md` Native / JIT).

## Related

- `compatibility.md`
- `instruction-set.md`
- `jit-internals.md`
- `testing.md`
