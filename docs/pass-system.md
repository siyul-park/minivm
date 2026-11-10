# Pass System

Analysis caching, bytecode transforms, SSA transforms, optimization composition.

## Model

```text
analysis  IR → cached facts
transform IR → mutation
pipeline   ordered transforms + invalidation
```

`pass.Manager` owns analysis registration/cache; `pass.Pipeline` owns transform order/invalidation. Analyses `MUST NOT` mutate IR. A transform returns `true` only when all cached analyses remain valid; errors `MUST` invalidate them because an in-place transform may have partially mutated IR.

## Standard pipelines

`internal/transform` `O1`, `O2`, and `O3` define the SSA pass order once. `optimize.O1`–`O3` use them directly. The JIT uses `O1` for Baseline and `O3` for Optimized, with its profile-reading `BCEPass` as a late pass.

## Layers

| Layer | Owns |
|---|---|
| `analysis` | reusable program facts |
| `internal/transform` | bytecode↔SSA conversion and SSA transforms; not public |
| `optimize` | user-facing optimization levels and program-level pass extension (`Optimizer.Add`) |
| `pass` | pipeline infrastructure |

## SSA

Each pass owns one policy. Current passes include folding, simplification, promotion, forwarding, CSE, guard elimination, LICM, and DCE.

LICM moves pure operations that cannot trap (`op.Traps()`), and in a loop that calls, allocates, and releases nothing (`quiet` in `internal/transform/licm.go`), slot loads the loop never stores and shape guards on invariant refs in blocks every iteration runs. A hoisted guard takes the loop header's entry state, so its failure deopts at loop entry. In such a loop, the array ops on an invariant array guard read the array through one `slice` (`ssa.OpSlice`) placed in the preheader of the outermost such loop; the bytecode emitter drops it.

Inlining (`InlinePass`, JIT Optimized tier only, ahead of the `O3` passes) replaces a `CALL` of a constant callee with the callee's body when the callee is a pure scalar leaf (`leaf` in `internal/transform/inline.go`): no call, handler, capture, write or allocation, only scalar params, locals and results (no i64 or ref), at most 32 instructions, and no backward branch. Every state-bearing operation of the inlined body takes the `CALL`'s own state, so a deopt resumes the interpreter at the `CALL`, which reruns the pure callee unobserved. A feedback-guarded callee keeps its guard ahead of the body. Lowering's check on bridged operations in such a body is owned by `jit-internals.md` (Exits).

Bounds-check elimination (`BCEPass`, JIT only: it reads `Profile.Refuted`) drops the check of an array op on a slice indexed by a loop's induction variable `i`: the header tests `i <s n` with `n` invariant, `i` enters at a constant ≥ 0 and every latch adds a constant ≥ 1 that cannot wrap past `n` (any step for a constant `n`, else 1). The index becomes a `bound` (`ssa.OpBound`). `n ≤ length` is checked once, even when `n` is the array's own `array.len`, by a `guard.bounds n, length` in the preheader at the header's entry state; a loop whose header entry offset is refuted keeps its checks.

SSA transforms are target-independent and `MUST` accept any valid `ssa.Function`.

## Bytecode

A size-changing transform `MUST` repair position-sensitive metadata or leave the function unchanged. `transform.SSAPass` `MUST` decline invalid encodings.

Passes `SHOULD` stay local, reuse existing analyses, and keep target policy out of target-independent code.

`transform.SSAPass` emits each constant, and each load of a local slot no operation stores, again at every use; it never homes them. A value whose uses within its own block consume it in turn is duplicated (`dup`) instead, except ahead of an operation that traps or enters a frame. It homes every other value that crosses a block or leaves stack order. A homed value takes a declared local that no emitted operation touches, or another homed value's local, when their live ranges do not overlap and their types are equal, and only a value left over gets a new local. A homed value's type is its kind, and for a reference the type its definition guarantees: the type of the slot a load reads, a constant's type, the common type of a block param's incoming values, or else the type of the slots it is stored to when every such store is in its defining block ahead of every other use; otherwise `any`. A constant or load nothing reads is not emitted. A block that only jumps reach, and that a later block jumps back to, keeps its params in locals: each jump into it writes them, so no value is on the operand stack across a back edge. It emits a branch whose taken edge falls through with the inverted comparison when the comparison has an exact inverse; float ordered comparisons have none, because NaN makes both false.

## Related

- `architecture.md`
- `jit-internals.md`
- `verification.md`
