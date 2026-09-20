# Add an Architecture

Checklist for adding architecture-specific executable-memory and encoding support.

`compatibility.md` owns platform support; `instruction-set.md` owns opcode status; `jit-internals.md` owns the planned native rebuild.

## Ownership

| Concern | Owner |
|---|---|
| machine encoding | `internal/asm/` |
| reference encoder | `internal/asm/arm64/` |
| executable memory | `internal/asm/` |
| platform selection | `internal/asm/` build-tagged files |
| future native lowering | JIT rebuild |
| platform support | `compatibility.md` |

An architecture package MUST expose one concrete constructor when its behavior is consumed by another package.

## Machine Layer

The agent MUST create `internal/asm/<arch>/` for register IDs, instruction encoding, and branch relaxation. It MUST keep executable-memory ownership in `internal/asm/` rather than duplicating allocation or publication logic per architecture.

Encoders MUST accept the architecture-neutral instruction representation and MUST NOT own runtime state, interpreter frames, or compiler policy.

## Native Layer

Native compilation is planned, not current. The future native layer MUST keep target lowering separate from the encoder and MUST use the runtime contract defined during the JIT rebuild.

A future lowering MUST return the concrete target type from one exported constructor and MUST keep unsupported operations on the threaded execution path until an explicit bridge contract exists.

## Platform

The agent MUST update `compatibility.md` with GOOS/GOARCH, CGO, executable-memory, instruction-cache, and build-tag requirements. Normal builds MUST NOT need manual tags.

## Coverage

The agent MUST start with low-risk paths in this order:

1. register and instruction encoding;
2. constants and simple data movement;
3. arithmetic and comparison encodings;
4. numeric conversions;
5. memory operands;
6. branches and relaxation;
7. executable-memory publication;
8. architecture-specific runtime entry when the native rebuild exists.

The agent MUST add native lowering, host interaction, calls, heap access, loops, and suspension only after the core runtime contract is stable.

## Validation

The agent MUST run:

```bash
go test ./internal/asm/<arch>/...
GOOS=linux GOARCH=<arch> go build ./...
GOOS=linux GOARCH=<arch> go test -exec=true ./...
```

When the native rebuild exists, the agent MUST also run its architecture-specific lowering tests and benchmark gates.

## Related

- `compatibility.md`
- `instruction-set.md`
- `jit-internals.md`
- `testing.md`
