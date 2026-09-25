# Pass System

Analysis caching, bytecode transforms, SSA transforms, optimization composition.

## Model

```text
analysis  IR → cached facts
transform IR → mutation
pipeline   ordered transforms + invalidation
```

`pass.Manager` owns analysis registration and the analysis cache. `pass.Pipeline` owns transform order and invalidation timing. Analyses `MUST NOT` mutate IR. Transforms `MUST` return `true` when all cached analyses survive and `false` when they must be invalidated. Pipeline errors `MUST` invalidate cached analyses because an in-place transform cannot be assumed unchanged on error.

## Layers

| Layer | Owns |
|---|---|
| `analysis` | reusable program facts |
| `transform` | bytecode transforms, bytecode↔SSA conversion, and SSA transforms |
| `optimize` | user-facing optimization levels |
| `pass` | pipeline infrastructure |

## SSA

Each pass owns one policy. Current passes include constant folding, algebraic simplification, local promotion, load forwarding, CSE, guard elimination, LICM, and DCE.

SSA transforms are target-independent and `MUST` accept any valid `ssa.Function`.

## Bytecode

A size-changing transform `MUST` repair position-sensitive metadata or leave the function unchanged. `transform.SSAPass` `MUST` decline invalid encodings.

Passes `SHOULD` stay local, reuse existing analyses, and keep target policy out of target-independent code.

## Related

- `architecture.md`
- `jit-internals.md`
- `verification.md`
