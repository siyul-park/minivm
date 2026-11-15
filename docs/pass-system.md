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

`internal/transform` `O1`, `O2`, and `O3` define the SSA pass order once. `optimize.O1`–`O3` use them directly; `optimize.O3` also inlines (see Inlining under SSA). The JIT uses `O1` for Baseline and `O3` for Optimized, with its profile-reading `BCEPass` as a late pass.

## Layers

| Layer | Owns |
|---|---|
| `analysis` | reusable program facts |
| `internal/transform` | bytecode↔SSA conversion and SSA transforms; not public |
| `optimize` | user-facing optimization levels and program-level pass extension (`Optimizer.Add`) |
| `pass` | pipeline infrastructure |

## SSA

Each pass owns one policy. Current passes include folding, simplification, promotion, forwarding, CSE, guard elimination, LICM, bounds-check elimination, DCE, inlining, and state stripping.

Forwarding (`ForwardPass`) replaces a load of a slot by the value an earlier load read from it, or an earlier store wrote to it, along the dominator tree while nothing may write the slot and the types are equal; a stored reference or i64 is not forwarded, because the slot owns the reference and the load of an i64 is guarded. A value held for a local is dropped at a `call` and the next load reads the slot again: a load is cheaper than a value kept live, and so spilled, across the call.

Folding evaluates pure operations on constants and reduces integer identities. A block param that every surviving edge into its block feeds the same constant is that constant (a block a retreating edge also enters has no such param; Simplify removes a loop-carried constant first). A constant absorbs the other operand (`x*0`, `x&0`, `x|-1`), on either side of a commutative operation. Equal operands give a known result (`x-x`, `x==x`, `x&x`). A multiply or unsigned divide by a power-of-two constant becomes a shift. Floats are exempt from the identity and equal-operand reductions (`x+0`, `x-x`), which NaN and signed zeros break, though a float operation on constants folds; traps (`x/x`) are never folded. A bitwise result keeps its operands' kind tag, as the interpreter does, so operands of different kinds are not folded. Folding also turns a branch or table on a constant, or a branch whose edges are identical, into a jump to the selected edge (the interpreter's i32-lane rule); the blocks that jump leaves unreachable are dropped. On a graph where that would leave a block reachable only by a retreating edge, the terminators stay and the operations still fold. CSE keys commutative integer operations on sorted operands, so `a*b` and `b*a` share one value.

Simplification (`SimplifyPass`) replaces a trivial param, one whose incoming edges all feed one value of its type or the param itself, by that value everywhere, resolving chains of such params to a fixed point. A block that only one jump reaches, and whose params are all replaced, merges into the block that jumps to it, so a chain of such blocks becomes one and its values stay on the bytecode operand stack instead of crossing a block boundary. A param of the entry block is always kept.

DCE removes a block param no operation, terminator, frame or live param reads, with the edge arguments that feed it. A param of the entry block is always kept. A state's frames read their locals, so a function that keeps its states keeps the values they name.

Guard elimination (`GuardPass`) removes a guard dominated by an equal guard of the same operand, across calls, releases, and allocations. It relies on representation invariance (`memory-model.md`, Representation): a live object keeps its Go type and declared type, so a passed shape guard stays true.

LICM moves pure operations that cannot trap (`op.Traps()`) and, in a loop with no `call`, shape guards on invariant refs in blocks every iteration runs and loads of the running frame's locals the loop never stores: representation is immutable and only a call writes a local, so neither goes stale across an allocation or release. A loop with a call keeps both in place: a hoisted value live across a call spills for its whole life, and its per-iteration reload costs more than the load and guard it replaced (n-queens' recursive loop measured +2.7% hoisted). Loads of other slots (globals, upvalues) and `slice`s (host code may resize or replace an array's storage) need a loop that calls, allocates, and releases nothing (`quiet` in `internal/transform/licm.go`). A hoisted guard takes the loop header's entry state, so its failure deopts at loop entry. In a quiet loop, the array ops on an invariant array guard read the array through one `slice` (`ssa.OpSlice`) placed in the preheader of the outermost such loop; the bytecode emitter drops it.

Inlining (`InlinePass`, ahead of the `O3` passes in the JIT Optimized tier and in the bytecode `optimize.O3`) replaces a `CALL` of a constant callee with the callee's body when the callee is a pure scalar leaf (`leaf` in `internal/transform/inline.go`): no call, handler, capture, write or allocation, only scalar params, locals and results (no i64 or ref), at most 32 instructions, and no backward branch. Every state-bearing operation of the inlined body takes the `CALL`'s own state, so a deopt resumes the interpreter at the `CALL`, which reruns the pure callee unobserved. A feedback-guarded callee keeps its guard ahead of the body. Lowering's check on bridged operations in such a body is owned by `jit-internals.md` (Exits). The bytecode path has no state to resume from, so there a trap in the body would be reported in the caller's frame: it inlines only a leaf none of whose operations may trap, where a division or remainder by a nonzero constant cannot. Removing the call also removes that call's frame and stack use, so a frame or stack limit reached only at that call no longer traps; a resource limit is not program semantics. The bytecode path reads each callee as it was before any round trip rewrote it, so a rewrite that is rolled back leaves nothing in a caller.

State stripping (`StripPass`) removes every state operation and the state each operation and terminator holds. Bytecode emission never reads a state, and a state's frames keep dead values alive, so `transform.SSAPass` strips ahead of its pipeline and after inlining, and the pipeline's DCE then removes what only a state named. A later pass may add a state back (`PromotePass` seeds one for i64 locals), which emission ignores. Stripped SSA is outside the `ssa.Verify` contract, which requires a state on every exec, store, guard and exit; it is only emitted to bytecode, which `program.Verify` checks. The JIT tiers keep their states.

Bounds-check elimination (`BCEPass`, JIT only: it reads `Profile.Refuted`) drops the check of an array op on a slice indexed by a loop's induction variable `i`: the header tests `i <s n` with `n` invariant, `i` enters at a constant ≥ 0 and every latch adds a constant ≥ 1 that cannot wrap past `n` (any step for a constant `n`, else 1). The index becomes a `bound` (`ssa.OpBound`). `n ≤ length` is checked once, even when `n` is the array's own `array.len`, by a `guard.bounds n, length` in the preheader at the header's entry state; a loop whose header entry offset is refuted keeps its checks.

SSA transforms are target-independent and `MUST` accept any valid `ssa.Function`.

## Bytecode

A size-changing transform `MUST` repair position-sensitive metadata or leave the function unchanged. `transform.SSAPass` `MUST` decline invalid encodings.

Passes `SHOULD` stay local, reuse existing analyses, and keep target policy out of target-independent code.

`transform.SSAPass` emits each constant, and each load of a local slot no operation stores, again at every use; it never homes them. A value whose uses within its own block consume it in turn is duplicated (`dup`) instead, except ahead of an operation that traps or enters a frame. It homes every other value that crosses a block or leaves stack order. A homed value takes a declared local that no emitted operation touches, or another homed value's local, when their live ranges do not overlap and their types are equal, and only a value left over gets a new local. A homed value's type is its kind, and for a reference the type its definition guarantees: the type of the slot a load reads, a constant's type, the common type of a block param's incoming values, an array slice's array type, or else the type of the slots it is stored to when every such store is in its defining block ahead of every other use; otherwise `any`. Params and slices are solved together, unknown below every type and `any` above, so the result does not depend on visiting order and a value left unknown is `any`. A constant or load nothing reads is not emitted. A block that only jumps reach, and that a later block jumps back to, keeps its params in locals: each jump into it writes them, so no value is on the operand stack across a back edge. A jump stores a value to its param as soon as the value is computed unless a later operation, or another argument of the same jump, reads that param's local, which keeps the producer and its store adjacent; otherwise it stores the values together after the last one, in reverse. A value that a later operation takes behind rematerialized or homed arguments is not homed when it is the result of an expression tree: a contiguous run of operations, none a call, each reading rematerialized or homed values or the single-use result of the run's previous operation. Those leading arguments are loaded before the tree's first operation, unless the tree produces or reads them, so the result lands beside them. Blocks are emitted with block 0 first and, after a branch, its fall-through successor when the condition is not a comparison with an exact inverse (a float ordered comparison, because NaN makes both false; a constant; a bitwise result; a param or call result), so a loop's body is not placed behind its exit. A branch on a comparison with an exact inverse falls through to its successor inside the loop it heads, when only one edge stays in it, so an iteration takes the back edge alone. Otherwise the next block is the lowest not yet placed that does not end the function, so a module's exit falls off the end of the code and needs no `br`. A branch on a comparison with an exact inverse whose taken edge falls through is emitted with the inverted comparison.

Given a program that verifies, `transform.SSAPass` leaves one that verifies. It verifies the whole program once after rewriting. On a fault it restores every changed function and verifies again: a program that still fails was invalid, and the pass returns it unchanged. Otherwise it reapplies the changed functions one at a time and keeps each only if the program still verifies, so a function whose emitted code would not verify is left as it was. Constants added for functions that are not kept are dropped. It does not read the failing function from the verifier, which reports a function shared by identity at the first slot reaching it.

Module code is rewritten like any function: its results stay on top of the operand stack, but its locals, which sit beneath them, are not program semantics. A pass may change their values, reuse them for homed values, or add locals, so the stack a module leaves can hold different or more values below its results.

## Related

- `architecture.md`
- `jit-internals.md`
- `verification.md`
