# Pass System

Analyses, transforms, and optimization pipelines.

## When to Read

Read when changing `pass/`, `analysis/`, `transform/`, `optimize/`, or `internal/ssa/transform/`.

## Model

```text
analysis:  IR → cached facts
transform: IR → mutate in place
pipeline:  ordered transforms + invalidation
```

- `pass.Manager` owns analysis caching and invalidation.
- `pass.Pipeline` runs transforms in order.
- Analyses do not mutate IR.
- Transforms report preserved analyses through `pass.Preserved`.

## Layers

| Layer | Responsibility |
|---|---|
| `analysis` | reusable program facts |
| `transform` | bytecode transforms and bytecode↔SSA conversion |
| `internal/ssa/transform` | standalone SSA transforms |
| `optimize` | user-facing optimization levels |
| `pass` | generic pipeline infrastructure |

## SSA Transforms

Each SSA pass owns one policy. Current passes cover constant folding, algebraic simplification, local promotion, load forwarding, common-subexpression elimination, guard elimination, loop-invariant code motion, and dead-code elimination.

SSA passes are target-independent and must work on any valid `ssa.Function`, including one with no JIT-specific state.

## Bytecode Transforms

A bytecode transform that changes code size must repair all position-sensitive metadata or leave the function unchanged. `transform.SSAPass` instead re-emits from SSA and declines the whole function when the new encoding is invalid.

## Rules

Prefer a small local pass over a broad rewrite. Do not duplicate an existing analysis inside a transform. Do not move target-specific optimization into a target-independent pass.

## Related Docs

- `verification.md`
- `jit-internals.md`
- `coding-patterns.md`
