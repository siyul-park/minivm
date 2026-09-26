# Pass System

Analysis caching, bytecode transforms, SSA transforms, optimization composition.

## Model

```text
analysis  IR → cached facts
transform IR → mutation
pipeline   ordered transforms + invalidation
```

`pass.Manager` owns analysis registration/cache; `pass.Pipeline` owns transform order/invalidation. Analyses `MUST NOT` mutate IR. A transform returns `true` only when all cached analyses remain valid; errors `MUST` invalidate them because an in-place transform may have partially mutated IR.

## Layers

| Layer | Owns |
|---|---|
| `analysis` | reusable program facts |
| `transform` | bytecode transforms, bytecode↔SSA conversion, and SSA transforms |
| `optimize` | user-facing optimization levels |
| `pass` | pipeline infrastructure |

## SSA

Each pass owns one policy. Current passes include folding, simplification, promotion, forwarding, CSE, guard elimination, LICM, and DCE.

SSA transforms are target-independent and `MUST` accept any valid `ssa.Function`.

## Bytecode

A size-changing transform `MUST` repair position-sensitive metadata or leave the function unchanged. `transform.SSAPass` `MUST` decline invalid encodings.

Passes `SHOULD` stay local, reuse existing analyses, and keep target policy out of target-independent code.

## Related

- `architecture.md`
- `jit-internals.md`
- `verification.md`
