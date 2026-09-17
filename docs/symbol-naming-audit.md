# Symbol Naming Audit

Applied naming/vocabulary reference. `coding-patterns.md` is normative; this document records domain vocabulary that should remain consistent across JIT packages.

## JIT Vocabulary

| Symbol | Role |
|---|---|
| `jit.Compiler` | JIT driver |
| `jit.Plan` | native plan |
| `jit.Anchor` | native entry location |
| `jit.IsBridgeable` | bridge eligibility |
| `backend.Compiler` | SSA→machine compilation state |
| `backend.Machine` | target capability/lowering seam |
| `backend.Deopt` | native→interpreter state metadata |
| `backend.Bridge` | native resume point |
| `arm64.emitter` | ARM64 lowering state |
| `compile.Queue` | compile admission/scheduling |
| `compile.Store` | published code ownership |
| `tier.Watchdog` | native retirement verdict |

## Ownership Vocabulary

| Package | Naming domain |
|---|---|
| `instr` | opcode/ISA |
| `internal/ssa` | IR/dataflow |
| `internal/jit` | JIT-neutral planning |
| `internal/jit/frontend` | translation/planning facts |
| `internal/jit/backend` | lowering state/metadata |
| `internal/jit/<arch>` | target mechanics |
| `internal/journal` | native/interpreter ABI |
| `internal/jit/compile` | compilation lifecycle |
| `internal/jit/tier` | tiering policy |

Naming rules, symbol-removal review, ownership checks, and simplification checks are owned by `coding-patterns.md` and `refactoring.md`.

Do not record rename history here. Historical decisions belong in dated plans/audits.

## Related

- `coding-patterns.md`
- `refactoring.md`
- `architecture.md`
- `jit-internals.md`
