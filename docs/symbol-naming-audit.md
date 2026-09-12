# Symbol Naming Reference

Current naming and ownership conventions applied to minivm's production Go code.

## Rules

- Prefer one-word names.
- Add qualifiers only when they distinguish a real domain or ownership boundary.
- Use one canonical term for one concept across packages.
- Name symbols by role or behavior, not implementation steps.
- Keep standard domain terms such as `JIT`, `SSA`, `ABI`, `CFG`, `GVN`, and `DCE`.
- Use `HasX` for membership, `IsX` for predicates, and `MatchX` for equality checks.
- Use `<Field>` for direct boolean fields and `<Field>At` for position-sensitive predicates.
- Keep capability names singular and collections plural.
- Avoid compatibility aliases for internal symbols.

## Core JIT Vocabulary

| Symbol | Role |
|---|---|
| `jit.Compiler` | architecture-neutral JIT driver |
| `jit.Plan` | architecture-neutral native plan |
| `jit.Anchor` | native entry location |
| `jit.IsBridgeable` | unsupported operation eligible for threaded bridge |
| `backend.Compiler` | SSA-to-machine compilation state |
| `backend.Machine` | target capability and lowering seam |
| `backend.Deopt` | native-to-interpreter state metadata |
| `backend.Bridge` | native resume point after a bridge |
| `arm64.emitter` | one ARM64 compile's lowering state |
| `compile.Queue` | compile admission and scheduling |
| `compile.Store` | published native code ownership |
| `tier.Watchdog` | native-entry retirement verdict |

## Package Ownership

| Package | Naming focus |
|---|---|
| `instr` | opcode and ISA vocabulary |
| `internal/ssa` | IR operations and dataflow terms |
| `internal/jit` | planning and JIT-neutral concepts |
| `internal/jit/frontend` | translation and planning facts |
| `internal/jit/backend` | lowering state and machine-neutral metadata |
| `internal/jit/<arch>` | target-specific lowering mechanics |
| `internal/journal` | interpreter/native ABI cells and records |
| `internal/jit/compile` | compilation lifecycle |
| `internal/jit/tier` | tiering policy |

## API Rules

Export only symbols with an independent caller-visible contract. Prefer methods when behavior belongs to a receiver. Return defensive copies for mutable collections. Keep compiler artifacts immutable after publication.

## Review Checklist

For each changed symbol:

1. Can it be removed or inlined?
2. Does the narrowest owner hold the behavior?
3. Can visibility be reduced?
4. Does the name describe the role at the call site?
5. Does an existing type, operation, or API already express the same concept?
6. Does the final structure keep one implementation of the rule?

For documentation, describe these final names and roles directly. Do not record previous names or rename chronology here.

## Related Docs

- `coding-patterns.md` — normative naming specification
- `architecture.md` — package ownership
- `jit-internals.md` — JIT contracts
