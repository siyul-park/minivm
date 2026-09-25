# Symbol Naming Audit

Applied naming/vocabulary reference.

`coding-patterns.md` is normative; this document records domain vocabulary that `SHOULD` remain consistent across JIT packages.

## Current Vocabulary

Package and topic docs own current names. This document records only cross-package vocabulary; historical/rebuild terms `MUST NOT` be used for current symbols.

## Native Vocabulary

New compiler or target terms `MUST` be introduced by the owning package or topic doc before use.

| Package | Naming domain |
|---|---|
| `internal/jit` | native runtime and tiering |
| `internal/jit/compile` | native compilation and lowering coordination |
| `internal/jit/arm64` | ARM64 lowering |

## Ownership Vocabulary

| Package | Naming domain |
|---|---|
| `instr` | opcode/ISA |
| `internal/ssa` | IR/dataflow |
| `internal/asm` | machine encoding/executable memory |
| `transform` | bytecode/SSA translation and rewrite |
| `interp` | runtime execution |
| `prof` | execution sampling |

Naming rules, symbol-removal review, ownership checks, and simplification checks are owned by `coding-patterns.md` and `refactoring.md`.

Rename history does not belong here; dated plans and audits own historical decisions.

## Related

- `coding-patterns.md`
- `refactoring.md`
- `architecture.md`
- `jit-internals.md`
