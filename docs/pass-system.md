# Pass System

Analysis caching, bytecode transforms, SSA transforms, optimization composition.

## Model

```text
analysis  IR → cached facts
transform IR → mutation
pipeline   ordered transforms + invalidation
```text

`pass.Manager` owns analysis caching/invalidation. `pass.Pipeline` owns transform order. Analyses do not mutate IR. Transforms report preserved analyses through `pass.Preserved`.

## Layers

| Layer | Owns |
|---|---|
| `analysis` | reusable program facts |
| `transform` | bytecode transforms and bytecode↔SSA conversion |
| `internal/ssa/transform` | standalone SSA transforms |
| `optimize` | user-facing optimization levels |
| `pass` | pipeline infrastructure |

## SSA

Each pass owns one policy. Current passes include constant folding, algebraic simplification, local promotion, load forwarding, CSE, guard elimination, LICM, and DCE.

SSA transforms are target-independent and accept any valid `ssa.Function`.

## Bytecode

A size-changing transform repairs all position-sensitive metadata or leaves the function unchanged. `transform.SSAPass` re-emits from SSA and declines when the encoding is invalid.

Prefer local passes. Reuse existing analyses. Keep target-specific policy out of target-independent passes.

## Related

- `architecture.md`
- `jit-internals.md`
- `verification.md`
