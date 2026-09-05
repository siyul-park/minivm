# Pass System

How minivm analyses, transforms, and optimization pipelines work.

## When to Read

Use this document when changing `analysis/`, `transform/`, `optimize/`, `pass/`, or `internal/ssa/transform/`.

For bytecode semantics, see `docs/instruction-set.md`. For branch and handler validity, see `docs/verification.md`. For the SSA IR itself and JIT contracts, see `docs/jit-internals.md`.

## Summary

The pass system follows LLVM's new pass manager model:

- `pass.Manager` lazily computes and caches analyses
- `pass.Pipeline` runs transforms in order
- analyses and transforms stay separate

Use an analysis when code needs reusable information about an IR unit. Use a transform when code mutates `*program.Program`.

Design rules:

- keep analyses pure
- keep transforms in-place
- request analyses through `pass.Manager`
- report invalidation with `pass.Preserved`
- prefer simple local passes before complex global rewrites

## Source of Truth

| Concern | File or package |
|---|---|
| pass manager and pipeline | `pass/` |
| analyses | `analysis/` |
| transforms | `transform/` |
| optimizer levels | `optimize/` |
| bytecode to SSA and back | `transform/ssa.go`, `transform/emit.go` |
| SSA transformation policies (one per pass, no composer) | `internal/ssa/transform/` |

## Core Model

```text
analysis:  IR unit -> cached result
transform: IR unit -> mutate in place -> preserved analyses
pipeline:  ordered transforms + invalidation
```

Analyses compute facts. They should not mutate the program.

Transforms may mutate the program. They must report which cached analyses remain valid.

## `pass.Manager`

`pass.Manager` owns analysis registration and caching.

It caches results by result type and IR unit identity.

Main APIs:

| API | Purpose |
|---|---|
| `Register[U, R](m, analysis)` | register analysis result type `R` for unit type `U` |
| `GetResult[R](m, unit)` | get cached result or compute it on demand |
| `Invalidate(preserved)` | drop stale cached results |

Rules:

- never recompute registered analyses by hand
- always request analyses with `pass.GetResult`
- do not retain the manager after `Run` returns
- keep analysis result types specific and meaningful

If no registered analysis produces `R`, `GetResult` returns `ErrUnregisteredAnalysis`.

## `pass.Pipeline`

`pass.Pipeline[U]` runs transforms over an IR unit.

```go
pl := pass.NewPipeline[*program.Program]()
pl.Add(transform.NewFoldPass())

prog, err := pl.Run(m, prog)
```

The pipeline:

1. runs each pass in order
2. receives the pass's `Preserved` result
3. invalidates stale analyses
4. stops on the first error

Transforms see fresh analysis results after earlier transforms invalidate stale ones.

## Writing an Analysis

An analysis implements:

```go
type Analysis[U, R] interface {
    Run(*Manager, U) (R, error)
}
```

Rules:

- keep analysis read-only
- request dependent analyses through the manager
- avoid hidden global state
- return deterministic results for the same unit state

## Writing a Transform

A transform implements:

```go
type Pass[U] interface {
    Run(*Manager, U) (Preserved, error)
}
```

Rules:

- mutate the unit in place
- use `pass.GetResult` for analysis data
- return `pass.PreserveNone()` after code changes
- return `pass.PreserveAll()` only when nothing changed
- return `pass.PreserveNone(), err` on failure
- do not keep references to stale analysis results after mutation

## Optimizer Levels

`optimize.New(level)` registers the shared analyses and builds a cumulative pipeline.
Every rewrite of a function's own code is one of `internal/ssa/transform`'s policies, run
over each of a program's functions through `transform.SSAPass`:

```text
O0  no transforms

O1  FoldPass, DCEPass
    DedupPass

O2  FoldPass, CSEPass, GuardPass, DCEPass
    DedupPass

O3  FoldPass, PromotePass, ForwardPass, CSEPass, GuardPass, HoistPass, DCEPass
    DedupPass
```

O1 folds and sweeps up what folding leaves behind, O2 adds the dominance-scoped
common-subexpression elimination and the guard elimination that rides on it, and O3 adds
the redundant-load forwarding that makes a repeated read one value and the
loop-invariant code motion that reads best once the rest has canonicalized the function.
`DedupPass` follows the route at every level: a constant pool is a whole-program concern
no per-function IR has a counterpart for, and it collects the constants the route
interned while folding.

`Optimize(prog)` runs the configured pipeline. `Add(p)` appends a custom transform.

One `pass.Manager` serves both unit types, and `transform.SSAPass` builds no pipeline of
its own - `optimize` composes the `pass.Pipeline[*ssa.Function]` and hands it over.

Because analyses are invalidated between transforms, each pass receives fresh analysis data.

## Basic Blocks

`analysis.Blocks` computes the control-flow blocks of one function, for the JIT frontends
and for `transform.SSAPass` through them. `BlocksAnalysis` wraps it as a `pass.Analysis`,
which `optimize` registers so a transform added through `Optimizer.Add` can request it;
no pass a level composes does.

Each block contains:

| Field | Meaning |
|---|---|
| `Start` | first instruction byte offset, inclusive |
| `End` | byte offset after the last instruction |
| `Succs` | successor block indexes |
| `Preds` | predecessor block indexes |

Block boundaries are:

- offset `0`
- every branch target
- the byte after `BR`, `BR_IF`, `BR_TABLE`, `UNREACHABLE`, `RETURN`, or `RETURN_CALL`

A branch to the past-the-end offset has no successor block: the analysis models
it as a virtual exit for any function, top-level or not. `program.Verify` is the
sole owner of whether that virtual exit is legal, and it only accepts one for
top-level code (slot `0`); a function body that branches to its own end is
malformed. `BlocksAnalysis` stays permissive because the JIT needs the
same CFG shape regardless of scope; the emitter re-derives every branch offset from the
layout it produced and declines a function whose top-level exit it cannot spell, so no
pass relocates a target verification would have rejected.

`BR`, `BR_IF`, and `BR_TABLE` targets are computed once in `instr.Targets`
and reused by `program.Verify` and `BlocksAnalysis`, so the same
arithmetic is not duplicated at each call site.

Keep these boundary rules consistent with the JIT and verifier.

## Redundancy and Folding

There is one implementation of each of these, over SSA, in
`internal/ssa/transform`; the sections below describe what they do to bytecode once
`transform.SSAPass` has taken it round.

`FoldPass` replaces a pure operation with the result its arguments already decide:

```text
const, const, op   -> const              // whatever the opcode computes
x + 0, x - 0, x | 0, x ^ 0, x << 0, x >> 0, x & -1, x * 1, x / 1  -> x
x * 2^n -> x << n
x / 2^n -> x >> n   // unsigned division only
```

Folding a whole window and reducing one operand short of a constant are the same policy,
so they are the same pass. Comparison folds produce `i1`, matching runtime comparison
results; because there is no `i1` immediate, folded booleans are interned in the constant
pool and emitted with `CONST_GET`.

Skipped intentionally:

- float identities, because IEEE-754 makes them unsafe
- annihilators such as `x * 0` or `x & 0`, whose left argument would have to be dropped
  along with them, which is `DCEPass`'s judgement rather than this pass's
- a division or remainder by a folded zero, whose trap belongs to the interpreter
- an identity whose left argument is not a value some opcode computed. An identity hands
  that argument back, and with it its representation. A slot is zero-filled rather than
  written with its declared type's zero, so an unwritten `i32` local reads back as a raw
  `Boxed(0)` whose kind is `f64` (`docs/memory-model.md`); the arithmetic the identity
  would remove is what re-tags it. The bytecode `AlgebraicPass` this replaced applied the
  identity unconditionally and turned an `I32(0)` into an `F64(0)` at O2.

`PromotePass` turns an entry-frame local slot into values: every load reads whatever the
last store left, and a merge point takes a block parameter for it. It is textbook mem2reg -
Cytron's iterated dominance frontier over the slot's store blocks (`graph.Frontier`) decides
where a parameter goes, and a dominator-tree walk renames every access - and it is what
`ForwardPass` deliberately cannot do, since forwarding drops availability at every
multi-predecessor block and so gives a loop-carried local back its per-iteration load,
store, and boxing.

A slot qualifies only when nothing but `OpLoad` and `OpStore` names it, which the IR gives
for free: `LOCAL_GET`, `LOCAL_SET`, and `LOCAL_TEE` are the only opcodes `instr`'s effect
model says read or write `Local`, and a call writes `Global`, `Upval`, `Heap`, and `Frame`
but never the caller's own locals. Four things disqualify one: a `Slot.Base` that is not
the entry frame's, accesses that disagree on a type, a reference (whose slot holds the
reference count of what is in it, and `ssa.Local` has no ownership mark to say where that
count went - the same reason `internal/jit/arm64`'s `carry` takes scalars only), and no
store at all (a read-only slot already has one definition everywhere, so promoting it only
makes the value live across every block that reads it, which the emitter has to home in a
fresh local). A function that bridges a local opcode to the interpreter declines outright,
since a bridge reads the frame the promotion emptied.

A promoted local is no longer where the interpreter looks for it, so every `OpState`'s
entry frame gains an `ssa.Local` per promoted slot: the value that slot must be written
back with before a deopt resumes. That is what `internal/jit/arm64`'s `commitCarried`
already does for a carried register - write it to its VM slot on the paths that hand
control back, and nowhere else - rather than leaving a store in front of every guard,
store, and call, which would put more stores on the hot path than the loads removed.
`internal/jit/backend`'s `Exit` resolves those into `Deopt.Slots` entries at
`Frame.Base + Local.Index`, below the frame's own operands, so the flush stays in ascending
slot order. A function with no `OpState` at all - every function the ahead-of-time
optimizer sees, since bytecode does not deoptimize - has nothing to name.

The entry block loads each promoted slot once, which is the reaching definition every other
one starts from. A function whose entry block is itself a loop header gets a fresh entry
block in front of it so that load runs once per entry; an entry that both has predecessors
and takes parameters is declined, since a block in front of it has no operands to pass on.
This is the only place the pass grows the block graph, and it never splits an edge.

`ForwardPass` replaces a load with the value an earlier load of the same slot already
produced, which is the textbook redundant-load elimination and what `CSEPass` needs in
front of it: a value in SSA is its own definition, so two reads of one local are two
definitions and every computation over them is two computations. A forwarded value is
invalidated by an `OpStore` to the same slot, by an `OpExec` or `OpBridge` whose opcode
`instr`'s effect model says writes that storage - a call writes `Global` and `Upval` but
never the caller's `Local` - and by reaching a block more than one edge reaches, which is
also what makes a loop safe, since a header always has at least two predecessors. Nothing
else can: a guard and every other deoptimizing operation resume the interpreter with the
slot's committed value, which the stores this pass never removes have already written
there.

A store starts no availability of its own. Bytecode already keeps that value in the slot
the store wrote, so forwarding a later load onto the stored value only makes it live
across the store, which the emitter then homes in a fresh local - one more instruction
and one more slot than reading back the slot the program itself named.

## SSA Transformation Policies

`internal/ssa/transform` is a second pass system over `*ssa.Function`, reusing `pass.Pass`, `pass.Analysis`, `pass.Manager`, and `pass.Pipeline` exactly as the top-level `transform` and `optimize` do over `*program.Program` - no second pipeline abstraction exists. Its role in `docs/coding-patterns.md` §6 is the top-level `transform`'s role, not `optimize`'s: it owns one transformation policy per pass and exposes no composer. A caller builds its own `pass.Pipeline[*ssa.Function]` and adds `FoldPass`, `PromotePass`, `ForwardPass`, `CSEPass`, `GuardPass`, `HoistPass`, then `DCEPass`, in that order, because Promote and Forward have to make a repeated read one value before CSE can see a computation over it as one computation, CSE has to unify a repeated load's value before a guard built on each read is recognizably the same guard, DCE has to run last to sweep up whatever folding, deduplicating, guard elimination, and hoisting leave behind (an operand a fold made unused, an `OpState` a removed guard no longer resumes into), and Hoist reads best once Fold and CSE have already canonicalized the function, so it moves one instance of an invariant computation rather than a would-be duplicate. None of that is a correctness requirement for `HoistPass` specifically, though: it never unifies a value (only `CSEPass` and `GuardPass`'s shared `dedup` does that) and never moves a guard or anything else that carries deopt state (only `GuardPass`'s target), so it is sound wherever it runs in the sequence - unlike `GuardPass`, whose placement after `CSEPass` is load-bearing. `internal/ssa/transform/transform_test.go`'s `TestPassOrder` composes exactly this pipeline and asserts the ordering facts end to end. `optimize` owns the leveled, user-facing composition of these passes, over `*program.Program` through `transform.SSAPass`, so `internal/ssa/transform` never grows an `Optimizer`-shaped composer that would only duplicate `optimize.Optimizer`'s shape.

Every pass here must stay correct and useful on a function with no JIT-specific fact at all, not only one `internal/jit/frontend` produced with guards and deopt state - `internal/ssa/transform` does not import `internal/jit`, so nothing in it may assume one is present.

| Pass | Collapses | Mechanism |
|---|---|---|
| `FoldPass` | an `OpExec` whose opcode is `IsPure()` and whose arguments are all `OpConst`, or whose right argument alone makes it an identity or a shift | decodes each `types.Boxed` argument into its native Go type and computes with ordinary operators; an identity aliases the result onto the left argument, a strength reduction adds the shift amount as a new `OpConst` |
| `PromotePass` | every `OpLoad` and `OpStore` of an entry-frame local slot that is stored at least once, holds no reference, and is read back at the type it was written | mem2reg: a block parameter at the iterated dominance frontier of the slot's stores (`graph.Frontier`), then a dominator-tree walk renaming each access to the definition reaching it |
| `ForwardPass` | an `OpLoad` a dominating `OpLoad` of the same `Slot` already performed, with nothing in between writing that storage | a dominator-tree walk carrying the value held in each slot, cleared at a block more than one edge reaches and at every operation `instr`'s effect model says writes that space |
| `CSEPass` | two `OpConst` or pure `OpExec` operations with equal value, related by dominance | `dedup`, keyed by opcode and argument identity |
| `GuardPass` | two guards of the same kind admitting the same fact over the same operand, related by dominance | `dedup`, keyed by guard kind, operand, and the admitted fact (target kind, shape, bounds, or specialized value) |
| `HoistPass` | an `OpConst`, or a pure and non-trapping `OpExec`, every one of whose arguments is defined outside a loop it sits in (or was itself just hoisted) | loop-invariant code motion: moves the operation into the loop's preheader when one already exists, never by splitting an edge to build one |
| `DCEPass` | an operation with no live result and no effect, and any block unreachable from the entry | mark-sweep liveness seeded from every operation instr's effect model says runs unconditionally, propagated backward through every value an operation reads - including an `OpState`'s own frame stacks and promoted locals |

`CSEPass` and `GuardPass` share one engine (`dedup` in `internal/ssa/transform/dedup.go`): a dominator-tree-scoped hash-consing table, walked in the dominator tree's own preorder so a key one block establishes stays visible to every block it dominates and is forgotten once that whole subtree is done. This is the SSA counterpart of the bytecode global value numbering minivm used to carry, and is far smaller than it: a value here is its own definition, so identity is free, and dominance alone - no available-expression dataflow, no per-block value renumbering, no stable-versus-opaque story for mutable loads - decides what one definition may stand in for.

`GuardPass` has no bytecode counterpart; a guard is a fact only a JIT frontend's speculation invents. It is the pass `docs/jit-internals.md`'s "Heap Reads and Mutations" section describes as still needed: a repeated heap access re-emits and re-guards independently today, and running `CSEPass` first is what makes the second access's guard operand equal the first's.

`HoistPass` (`internal/ssa/transform/hoist.go`) is the general loop-invariant code motion `docs/jit-internals.md`'s "No hoist, no carry" paragraph always said belonged in an optimizer rather than in the trace frontend: it uses `internal/graph`'s dominance and `LoopHeaders` directly, over any function, not only a trace-compiled one. It is deliberately narrower than that paragraph once envisioned, though. Eligibility is `OpConst` or a pure, non-trapping `OpExec` - never a guard, an `OpLoad`, an `OpStore`, or anything else that reads or writes `Local`, `Global`, `Upval`, `Heap`, `Frame`, or `Branch`, and never an integer division or remainder, whose zero-divisor fault could otherwise fire on a loop trip count of zero that the original program never reached. Every one of the four guards always carries deopt state (`ssa.Verify`'s own `resume()` rule), and that state names a frame chain and stack valid at the guard's original position, not at a point before the loop ran - `HoistPass` refuses anything that carries state outright rather than try to reconstruct one that would be. It hoists only into a preheader that already exists (the loop header's one predecessor from outside the loop) and never splits an edge to build one. See `internal/ssa/transform/hoist.go`'s own documentation for the full argument, including which of `hoistable`'s three restrictions in `internal/jit/traceplan.go` were backend representation artifacts and which - the ban on ref-array containers - is a real hazard that belongs to retain/release pairing instead, still deferred.

These six are the only implementation of each policy: `transform`'s own `FoldPass`, `AlgebraicPass`, `DCEPass`, and `GVNPass`, together with `analysis`'s `GVNAnalysis` and the branch-and-handler rewriter they shared, are gone, and `optimize` reaches these instead through `transform.SSAPass`. The JIT's own use of them is still future work.

## Bytecode to SSA and Back

`transform.SSAPass` (`transform/ssa.go`) is a `pass.Pass[*program.Program]` that takes each
of a program's functions - the top-level body first, then every `*types.Function` constant -
to SSA with `frontend.Body`, runs the `pass.Pipeline[*ssa.Function]` it was constructed with,
and writes the result back out as bytecode with the emitter in `transform/emit.go`. It builds
no second translator: `frontend.Body` is `frontend.Static` without the three rules that belong
to a native compile rather than to the translation (see `docs/jit-internals.md`).

A constant pool has no heap behind it here, so `pool` gives every constant the interpreter
would allocate a cell for - a string, a container, a function, an i64 too wide for its boxed
payload - a reference naming its own pool slot plus one, and resolves the `jit.Object` facts
for it off the constant itself. A translation only ever hands that identity back to
`jit.Objects`, and the emitter inverts it through the same table, so no heap address is
needed or invented.

The emitter's whole problem is that SSA carries dataflow and bytecode carries an operand
stack. A value read exactly once, in its own block, at the moment it is on top stays on the
stack, which is every value an untransformed function holds, since it came from a stack
machine in the first place. Anything else - read twice, read in another block, or moved out
of stack order by a pass - takes a fresh local. Which values those are is not known before
the stack is walked, so a walk that cannot reach a value names it, that value takes a local,
and the walk runs again.

### What the route declines

A declined function comes back byte for byte as it was; nothing is ever emitted wrong.

| Declined | Why |
|---|---|
| `UNREACHABLE` anywhere in the function | the IR has no operation for it, so the trap would be lost |
| an opcode carrying an immediate operand (`REF_TEST`, `REF_CAST`, `ARRAY_NEW`, `ARRAY_NEW_DEFAULT`, `STRUCT_NEW`, `STRUCT_NEW_DEFAULT`, `MAP_NEW`, `MAP_NEW_DEFAULT`) | the IR resolves what the operand meant and keeps no way to spell it again |
| a protected region, an unresolved callee, an operand kind no opcode can pop | the frontend declines them already |
| a module value needing a local | a module's locals sit on the operand stack a caller reads results off, so one more of them is one more result |
| a local slot past 255, a constant slot past 65535 | the operand widths `LOCAL_*` and `CONST_GET` encode them in |
| a branch offset outside signed 16 bits | blocks come back in the order the SSA holds them, not the order the bytecode laid them out, so a branch that just reached its target may not |

A block nothing reaches is not on that list. `frontend.resolve` leaves a span its fixpoint
never entered without a state and `build` simply does not emit it, so the block goes and the
function stays. `frontend.Static` still refuses such a function outright, because the block
graph a native compile emits is checked against `jit.StaticPlan`'s, which refuses one too;
`frontend.Body` is the only caller that drops the block instead.

### What it gains over a peephole

Asserted in `TestSSAPass_Run`:

- `FoldPass` reaches the whole pure family through `instr`'s own purity rather than one
  hand-written case per opcode, so `i64.xor`, `i64.and`, and `i64.or` fold exactly as their
  i32 counterparts do.
- `DCEPass` drops a computation nothing reads, which whether an operand stack still needs a
  value it pushed is not a question a peephole over bytecode answers, and a block nothing
  reaches.
- `ForwardPass` then `CSEPass` collapse a computation repeated over reloaded operands, and
  the emitter writes the shared value into one fresh local with `LOCAL_TEE`.
- `PromotePass` carries a loop counter held in a local out of its slot and back through the
  emitter as bytecode that still runs the same way.

## Rewrite Rules

Any pass that changes bytecode length must repair all position-sensitive data:

- branch offsets
- branch table targets
- exception handler starts, ends, and catch targets

Check separately:

- constant indexes
- type indexes
- local indexes
- handler depths
- signed 16-bit branch reachability

If repair cannot preserve behavior, leave the function unchanged. Prefer a safe no-op over a
risky rewrite.

`transform.SSAPass` re-emits rather than repairs, which is the same rule at its limit: it
computes every branch offset from the layout it produced and declines the whole function when
one no longer fits its operand. It is the only transform that moves an offset at all, so no
in-place repair mechanism survives - a new offset-moving pass must re-emit or repair
everything above.

## Maintenance Notes

When changing the pass system:

- decide first whether the change is analysis or transform
- keep analyses cached and read-only
- keep transforms in-place and explicit
- use the manager instead of recomputing analysis facts
- invalidate aggressively when unsure
- prefer one simple pass over several tightly coupled passes
- keep bytecode rewrites conservative
- never silently produce invalid branch offsets
- test indexes and offsets independently
- preserve optimizer/JIT agreement on CFG boundaries

## Related Docs

- `docs/instruction-set.md` — opcode semantics
- `docs/verification.md` — valid bytecode and stack rules
- `docs/jit-internals.md` — CFG agreement with trace roots
- `docs/coding-patterns.md` — code style conventions
