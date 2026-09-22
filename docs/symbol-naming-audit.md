# Symbol Naming Audit

Applied naming/vocabulary reference.

`coding-patterns.md` is normative; this document records domain vocabulary that `SHOULD` remain consistent across JIT packages.

## Current Vocabulary

Current implementation vocabulary is owned by package docs. The rows below are historical/rebuild terms and MUST NOT be used to describe current symbols.

## Native Vocabulary

Current native terminology is defined by the owner docs and packages below. New compiler or target terminology MUST be introduced in the owning package or topic doc before use.

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

The agent `MUST NOT` record rename history here. Historical decisions belong in dated plans/audits.

## Related

- `coding-patterns.md`
- `refactoring.md`
- `architecture.md`
- `jit-internals.md`
