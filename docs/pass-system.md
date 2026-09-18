# Pass System

Analysis caching, bytecode transforms, SSA transforms, optimization composition.

## Model

```text
analysis  IR → cached facts
transform IR → mutation
pipeline   ordered transforms + invalidation
```

`pass.Manager` owns analysis caching/invalidation. `pass.Pipeline` owns transform order. Analyses `MUST NOT` mutate IR. Transforms `MUST` report preserved analyses through `pass.Preserved`.

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

SSA transforms are target-independent and `MUST` accept any valid `ssa.Function`.

## Bytecode

A size-changing transform `MUST` repair all position-sensitive metadata or leave the function unchanged. `transform.SSAPass` re-emits from SSA and `MUST` decline when the encoding is invalid.

The agent `SHOULD` prefer local passes, `SHOULD` reuse existing analyses, and `MUST` keep target-specific policy out of target-independent passes.

## Related

- `architecture.md`
- `jit-internals.md`
- `verification.md`
