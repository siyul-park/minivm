# JIT Internals

Contracts for the ARM64 JIT in `internal/jit/arm64` and its interaction with `interp/` and `internal/asm/`.

## When to Read

Use this document before changing `internal/jit/arm64/*.go`, `interp/jit_*.go`, `interp/trace.go`, `interp/tier.go`, `asm` callable ABI code, trace recording, lowering, deoptimization, loop safepoints, or JIT installation.

For user-facing performance results, see `docs/benchmarks.md`. For sampling and hotness thresholds, see `docs/profile.md`.

## Source of Truth

| Concern | File or doc |
|---|---|
| opcode semantics | `docs/instruction-set.md`, `instr/type.go` |
| threaded behavior | `interp/threaded.go` |
| trace recording | `interp/trace.go` |
| architecture-neutral compiler IR (plan graph, dataflow facts, layout tables, recorded-trace data) | `internal/jit/` |
| architecture-neutral compiler driver | `internal/jit` |
| runtime tier-up mechanism (hot-event sampling, tracing, compile/install/cool/retire) | `interp/tier.go` |
| compile job queue, its execution mode, and the published-code store shared by interpreters | `internal/jit/compile` |
| throughput/give-up retirement verdict (pure, no interpreter state) | `internal/jit/tier` |
| ARM64 lowering | `internal/jit/arm64/` |
| architecture-neutral SSA backend and the `Machine` seam | `internal/jit/backend` |
| ARM64 arch selection | `interp/jit_arm64.go`, `interp/jit_stub.go` |
| frame-journal layout | `internal/journal/` |
| callable ABI | `internal/asm/` |
| value layout | `docs/value-representation.md` |
| heap ownership | `docs/memory-model.md` |
| ticks and thresholds | `docs/profile.md` |

## Summary

minivm always compiles bytecode to threaded closures first. The JIT is a lazy ARM64 plan backend layered on top of that portable threaded runtime.

```text
program.Program
  -> threader -> []func(*Interpreter)   always available
  -> tracer           -> trace snapshots        lazy runtime recording
  -> compiler         -> *module                lazy ARM64 backend
```

The threaded interpreter is the source of correctness. Native code is an optimization and must always have a correct threaded fallback.

Default rules:

- preserve threaded and JIT semantic parity
- normalize every frontend into one small plan before architecture lowering
- keep fallback behavior explicit
- keep architecture-specific code isolated
- use short, standard names
- if two designs behave the same, choose the simpler one

## Execution Model

The dispatch table is:

```go
i.code[addr][ip]
```

Where:

- `addr` is the function slot
- `ip` is the bytecode offset
- each entry is a threaded closure or a wrapper around a native callable

A hot JIT attempt records a runtime trace from the current interpreter state. The ARM64 backend then emits native callables for usable roots.

| Root | Meaning | Install point |
|---|---|---|
| module entry | top-level program start | `i.code[0][0]` |
| function entry | function start | `i.code[addr][0]` |
| loop header | hot backward-branch target | `i.code[addr][header]` |

Rejected traces emit nothing. The threaded closure remains installed.

Function entry callables tear down their frame on return. Module entry callables preserve the top-level frame and complete by advancing to the end of program code. Loop callables re-enter a live frame and must not unwind it.

## Solo and Pool JIT

Solo and pooled interpreters run one compile path. Every `Interpreter` holds a
`compile.Queue` and a `compile.Store` (`internal/jit/compile`): the queue
decides which root is built next and where that build runs, and the store holds
what those builds publish. A solo interpreter owns both privately and simply
never contends for its own queue; `Pool` is the only public seam that shares
them.

`Pool` owns one queue, one store, and one `tracer`. Borrowed interpreters
attach to that state and keep their runtime stacks, heaps, dispatch tables, and
installed wrappers local. The published native code is shared; the dispatch
table is not.

`interp` raises a `compile.Job` wherever it learns a root is worth building -
the entry root from `warm`, a recorded loop root from `trace`, a hot side exit
from `exit` - and then tries to claim the queue for that function. Whoever wins
compiles, publishes, and installs; every other interpreter picks the code up
through `sync` at its next safepoint. The queue guarantees:

- trigger counts are atomic, and aggregate across everyone sharing the queue
- one holder builds a function at a time
- published builds publish immutable `asm.Callable`s
- each interpreter installs those callables into its own dispatch table

`Queue.Claim` hands out a build's root and trigger together. Each function owns
a coalescing queue of exact anchors rather than one pending slot: distinct loop
roots are retained, duplicate requests are discarded, and a side exit arriving
behind an active hot build remains queued because it represents newer trace
work. Side-exit jobs take priority over queued hot roots. `Queue.Done`
finishes only the claimed build and leaves queued jobs for the next winner.

`Queue.Done` also records every root the build actually emitted an entry for, so
a later hot request for one of them is discarded instead of repeating work a
peer already did. A build that emitted nothing records nothing, so a caller that
learns more about a root - a bounded recording it can retry deeper - may ask
again. A side-exit job is never discarded that way: asking for a root that is
already native to be rebuilt with the exit's leg folded in is its whole point.

### Where a build runs

`Queue.Serve` runs the build a caller claimed, and the queue's construction
decides where. `compile.New(size)` runs it on the goroutine that claimed it;
`compile.New(size, compile.WithAsync())` hands it to one worker goroutine and
returns at once. `Pool` builds the async queue; a solo interpreter builds the
inline one, so it holds no extra goroutine and polls for nothing. Both modes
drive one seam: the interpreter resolves everything only it can produce, calls
`Serve`, and then adopts through `sync`, which finds the result already parked
when the build ran inline and finds it at a later safepoint when the worker ran
it.

What may leave the interpreter's goroutine is exactly `jit.Compiler.Compile`
and the `Store.Publish` that follows it, because both read only the immutable
snapshot and the store's own synchronized state. Trace recording cannot:
`tracer.capture` clones the running interpreter and single-steps its threaded
closures. Building the snapshot cannot: only the interpreter can read its own
private state. Installing cannot: `i.code`, `i.natives`, `i.cold`, `i.live`,
and the installed wrappers are interpreter-local, and so is the `prof.Collector`
the outcome is recorded into.

A finished build parks its `jit.Result` in `interp`'s `builds`, and `sync`
records it - the attempt's compile row, its emission rows, and any compile
error, which reaches `Run` through the safepoint that adopted it. A pool member
already reaches a safepoint every tick because `Store.Shared` is true for it,
which is why the worker needs no second polling mechanism; `withQueue` and
`withStore` are given together for exactly that reason.

A build parks its outcome before it publishes its code, and `sync` adopts what
is parked before it installs what is published, so a native entry never starts
counting dispatches before the build that emitted it was recorded. The claim
ends after the publish, so a peer that learns the root is built finds its code,
and the build's registration ends last, so a `Close` waiting on it knows
nothing is still publishing into the store.

Shutdown is ordered so no build outlives the memory it publishes into.
`Queue.Close` stops the worker only after it has drained every build already
handed to it, so each one still ends its claim and publishes or frees the
buffer it linked into; a build claimed after `Close` runs on the claiming
goroutine. `Pool.Close` calls it before any interpreter detaches from the
store, and `Interpreter.Close` waits for the builds it claimed, records their
outcomes, and only then drops its own hold. Both are idempotent.

`compile.Store` is append-only and reference counted. It hands out published
`jit.Code` by generation (`Store.Code`), and frees the `asm.Buffer`s those
callables live in only once the last holder detaches, because a published
mapping stays executable for as long as anyone can still dispatch into it.
`Store.Shared` reports whether anyone but the creator ever attached; a solo
interpreter builds inline into a store nobody else holds and therefore needs no
per-tick poll for code arriving from elsewhere.

## Compiler

`jit.Compiler` lives in `internal/jit`. It tries the SSA pipeline first and falls back to the plan frontends: `Compiler.native` hands the root to `Machine.Compile`, and a machine that declines it, or output that does not build, leaves nothing behind - the assembler is discarded whole - so the plan frontends below compile that root exactly as they did before the seam existed. That is the gate the ARM64 port advances behind, one opcode at a time, with the `interp` parity corpus green at every step. The interpreter builds the read-only `jit.Input` snapshot itself (`Interpreter.compileSnapshot` in `interp/jit.go`) and calls `Compile(input, root)` — on its own goroutine or on the queue's worker, see Solo and Pool JIT — receiving a `jit.Code`; it does not select or inspect a compilation strategy. Passing a snapshot rather than the interpreter is what keeps the dependency one-way: `internal/jit` never imports `interp`. A frontend may discover several recorded roots, but compilation selects only the requested anchor so later loop attempts do not re-emit already-installed entries.

Nothing an `Input` carries is storage the interpreter keeps mutating. `Constants`, `Globals`, and `Decl` are fixed once a program is loaded, a published `Trace` is immutable, and `Objects` (`jit.Objects`, a `map[int]Object`) replaces what used to be a live `[]types.Value` heap view: `Interpreter.objects` resolves address zero to the module body, every cell the constant pool publishes, and every function the host bound at a runtime address into the immutable facts a compile reads off them — the `*types.Function` published there, the `*types.StructType` a struct carries, the concrete itab of a primitive typed array, and the address a closure calls. Those are the only addresses a plan can reach: a static plan resolves a container or a callee only through a constant, and a trace plan names a callee the tracer already resolved to a function address. Resolving them on the interpreter's own goroutine is what makes a compile safe to run elsewhere — a `*types.Struct` is recycled through a pool that rewrites its type, an array header is rewritten in place, and a released slot is handed to the next allocation, so neither a heap slot nor the object behind it is stable to read from another goroutine. A resolved address stays present in the map even when its cell carries no fact, so a lowering that only needs to know an address named a live cell tests for membership. `TestCompiler_CompileConcurrentHeap` compiles on four goroutines against a heap another goroutine keeps overwriting and growing.

The compiler runs two ordered frontends over that input:

1. `jit.StaticPlan` constructs complete plans from verified bytecode and dataflow: one entry plan when no entry is installed, plus one `jit.EntryLoop` plan per loop header (`headers`, unexported).
2. `jit.TracePlan` constructs plans from immutable runtime trace snapshots.
3. If neither frontend produces a lowerable plan, threaded execution remains installed.

The order depends on the requested root. An entry root tries the static frontend first: it plans the whole function deterministically and covers opcodes no trace can record. A loop root tries the trace frontend first, because a recorded loop specializes its body to the path actually taken - folded legs, a hoisted container - and the static loop plan is the fallback for a loop no trace could record.

Both frontends return the same `jit.Plan` model (`internal/jit`): ABI kind, a root block ID, flat blocks, entry states, ordinary steps, and explicit edges. Every internal edge carries a block ID; unresolved edges retain only their threaded fallback anchor. Build, link, validation, accounting, and publication are centralized in the compiler.

## Static Frontend

The static frontend (`internal/jit`) analyzes basic blocks with one forward fixpoint that tracks stack kind, constant-ref provenance, direct-call targets, declared struct types, and known i32 constants. A `STRUCT_GET` whose container carries a declared struct type (or references a known heap struct) and whose field index is a known in-bounds constant resolves its result kind statically; the planner synthesizes `Step.Seen` as the zero boxed value of that kind, and the lowering's runtime itab, type, and per-field kind guards keep it sound. It emits plan blocks with explicit entry state, decoded operands, and block-ID edges. An opcode `jit.Bridgeable` names (see Bridge) ends its block on that opcode instead of rejecting the whole function, provided `applyStep` (unexported) can still model its stack effect; any other opcode the backend cannot lower, or one whose effect cannot be modeled, rejects the whole static plan for the function.

A container's element kind resolves from the cell `Input.Objects` recorded for it when its identity is known — which for a static plan is always a constant, the one ref provenance the forward fixpoint tracks — and otherwise from its declared array type (`types.ArrayType.ElemKind`) reached through the local, param, upvalue, or `REF_CAST` slot that carries it. Both are hints the runtime tag, itab, and bounds guards verify before any access, so a slot declared as an array that currently holds null or a differently shaped array deopts instead of being read. The declared type answers only in a call-free plan: the general array path combined with a native call still corrupts native state, so a calling function keeps resolving only from a constant container.

Every root of a function is planned from one shared block list, but each `entryLoop` plan keeps only the blocks its own root reaches (`prune`), renumbered densely. The backend emits every block a plan holds, so an unpruned header would re-emit the whole function once per header - O(headers) redundant code size, register pressure, and branch range. Reachability follows `term.edges` and their tail continuations, plus one edge no terminator names: a `terminateBridge` block resumes at the block planned immediately after it, because resumption is a fresh external entry rather than a branch. A block list that does not satisfy that layout skips the root instead of emitting a plan whose resume target is missing.

The loop-carried registers are recomputed per pruned plan rather than inherited: a block the header cannot reach is never emitted, so its bridges must not strip the plan's carried registers. Plans no longer carry a spill policy of their own (see Register Allocation below): a container store used to force a plan off the spill frame entirely, but the allocator now judges every spill by dominance, so a store is just another step.

Top-level modules containing `CALL` or `RETURN_CALL` are rejected because module entry does not implement the framed native-call ABI. Primitive typed-array constants remain ownership-neutral markers until `ARRAY_GET`; native code reloads the current heap cell, guards its shape and index, and retains the marker only on a cold fallback.

### SSA Static Frontend

`frontend.Static(input, root)` (`internal/jit/frontend`) applies the same planning rules and emits `internal/ssa` instead of a `jit.Plan`. Both frontends exist: the ARM64 backend still consumes `jit.Plan`, and nothing installs SSA yet. It reads only the snapshot and never imports `interp`, `internal/asm`, or a backend.

Which roots it accepts and rejects is identical to `StaticPlan`'s, including root selection, the forward fixpoint, the module-with-`CALL` rejection, and the call-free gate on declared array types. `frontend.TestStatic` asserts that equality over a written corpus and over generated well-typed functions, checks `ssa.Verify` on everything it emits, and compares the two block graphs.

`frontend.Module` is the narrow input that translation actually reads: the constant pool as boxed values, the declared kind of each global, the declared-type table, and the `jit.Objects` a constant reference resolves through. A `jit.Input` carries it beside the recorded traces, the address, the layout table, and the installed flag, none of which any translation consults; `Static` takes those four and applies the JIT's own entry rules to them. The identity a reference carries is the caller's to choose, because a translation only ever hands it back to `Objects` - the interpreter's heap address for a JIT compile, the constant's own pool slot for a compile with no heap.

`frontend.Body(m, addr, fn)` is that same translation over a whole function against a `Module` alone, for the ahead-of-time optimizer (`transform.SSAPass`). It skips exactly the four rules that are the JIT's and not the translation's: the anchor must be the function entry rather than a loop header, an already-installed entry is still translated, module code holding a `CALL` is accepted because nothing here has to implement the framed native-call ABI, and a span the fact fixpoint never reaches is left out instead of refusing the function. `Static` still refuses that last one, because the block graph it emits is checked against `jit.StaticPlan`'s, and `planStates` refuses it too. Address zero still means module code, which ends on `OpComplete` rather than a return.

- **Blocks.** One block per plan anchor: a basic block, or the part of one between two bridges. Each takes its live operands as block parameters, which is what `block.state` carried; the abstract operand stack is SSA values rather than a reload list.
- **Bridge.** `OpBridge` is an operation, not a terminator, so the block ends on it with an `OpJump` to the block the interpreter resumes into. The resume position is therefore that single successor of a block whose last operation is a bridge, and its IP is the bridged opcode's own IP — carried by the bridge's `OpState` frame — plus that opcode's encoded width, which is fixed per opcode for every bridgeable one. `instr` states an arity for `ARRAY_NEW` that only approximates its real one, so a bridge names the arguments `instr.TypeOf` declares and the operands beyond them reach the interpreter through the flushed stack the `OpState` frame lists.
- **Guards.** A container access whose shape the plan resolved is preceded by `OpGuardShape` carrying it: the element itab `ElemShapeByKind` gives for an array, the `*types.StructType` pointer for a struct. An access whose shape did not resolve carries neither, exactly as `Step.Shape` is zero in a static plan today. Index bounds are not guarded in the IR; the guarded heap path still owns that check.
- **Ownership.** `backing`'s five-way inference stays a planning fact, and the IR states its result. A ref loaded from a local, global, upvalue, or constant is borrowed; anything an operation produces is owned. `OpRetain` materializes where `own`, `detach`, `ownRefs`, and `ret` take one: before a slot store, for the surviving copy of a tee and of a `DUP` of an owned ref, before a container store's stored value, before every operand a call or a bridge hands over, on a returned value, and on an edge whose successor merged that operand to owned. `OpRelease` drops the count a consumed owned container held. Two successors that disagree about one operand's ownership leave the function unplanned rather than retaining it on a path that never releases it.
- **Cold-path ownership.** `ssa.Frame.Stack` is a list of `ssa.Operand`, each naming a value and whether that stack entry owns a reference count, which is the fact `retainDeferred` and `emitExits` act on: a borrowed entry is the one a cold path retains before handing the flushed stack to the interpreter. Ownership belongs to the entry and not to the value, because a `DUP` puts one value in two positions and a later retain moves exactly one of them onto the stack, so no per-value flag could state it. `ssa.Verify` rejects a frame owning a value that cannot hold a reference. The retains themselves stay the machine's — the IR says which entries need one, not how to take it. One instruction materializes one state for as long as its operands hold still: a retain taken after a state was emitted leaves that state as it was, resuming into a stack that did not own the entry yet, and starts a new one for everything after it. That is what lets a guard stand in front of an operation that adopts the stack — the guard's cold path retains what the operation's own state already owns.
- **Extra block.** A branch to the offset one past the end of the code, which `analysis.Blocks` treats as a virtual exit and a plan leaves as an unresolved edge, becomes a real block that returns or completes.
- **`UNREACHABLE` is an operation, not a no-op.** `instr` states no stack effect for it, so nothing but an explicit rule puts it in the IR at all, and reaching it raises. `walk.perform` emits an argument-free `OpExec` for it, which the ARM64 SSA machine declines, so a function holding one compiles through the plan pipeline instead of natively running past it. `NOP` stays the one opcode the translation drops.
- **Narrower than the plan.** An operand whose kind its opcode cannot pop leaves the function unplanned, where `applyStep` never type-checks; `program.Verify` rejects such bytecode before it runs. A `RETURN_CALL` ends the block on `OpExit` (see Tail calls below). An `i64` literal that does not survive `types.BoxI64` is refused for the same reason, because `ssa.Operation.Const` is the only place a literal can live.

### SSA Trace Frontend

`frontend.Trace(input, root)` (`internal/jit/frontend`) is the second frontend over one snapshot: it plans the recording published at `root` into `internal/ssa`, where `TracePlan` plans every recorded anchor into `jit.Plan`s. It returns `nil` for a root no native entry can be anchored on and never an error, because nothing in trace planning fails any other way. It shares `walk` — the operation-level translation, ownership inference, guard emission, and deopt-state construction — with `Static`; only the observation `walk.seen` carries differs, and it is what resolves an element shape, a struct field kind, and a call target from what the recording ran against rather than from constants and declared types.

Which roots it accepts is identical to `TracePlan`'s, status for status, including the `Carried` refusal and the loop-anchor rule. Leg selection and hit ordering are identical too. `frontend.TestTrace` asserts that over hand-built trees, and `interp.TestFrontend_Trace` asserts it over trees the real recorder produced (the recorder clones a running interpreter, so nothing outside `interp` can obtain one); both check `ssa.Verify` on everything emitted and compare the two block graphs.

- **Blocks.** A recording is linear, so the path it took keeps filling one block until a branch, a cut, or a seam ends it. Only a folded continuation, a loop back-edge, and a caller continuation are real merges, and those blocks take the live operands as parameters, so nothing reloads from VM slots the way a `Block.State`-less trace-plan block must.
- **Inlining.** The recording states which calls were entered — the ops after an inlined one run one frame deeper — so `walk.enter` pushes a frame, moves the arguments into its slots, and clears the rest, and `walk.stitch` pops it, owning the results the caller adopts. `ssa.Slot` grew a `Base` for this: `local[i]` names different storage in different frames, and without the frame floor no reader or pass could tell an inlined callee's local from the entry frame's. Distinct inlined contexts stay distinct because they are distinct blocks, never a bytecode anchor.
- **Deopt state.** `OpState` materializes the whole frame chain, outermost first, matching `journal.Record` order: every outer frame resumes past the call it is suspended on and carries the operands still live in it, and the innermost one resumes at the instruction being translated. `Frame.Base` is the frame's VM stack floor relative to the entry frame, the same coordinate `Slot.Base` uses.
- **Speculated callees.** A call reaches the function it enters through one question in both frontends: which reference its callee operand resolves to, which `jit.Objects` then answers about. A static call carries that reference as an `OpConst` already; a recorded one loads it at runtime, so `walk.callee` emits an `OpGuardValue` admitting only the reference the recording observed and hands the call the guarded value. The speculation sits on the guard, where the deopt state for it already belongs, and the call stays an ordinary call carrying no callee of its own; `ssa.Verify` requires the value a guard admits to be defined by an `OpConst`, so folding a callee through one always ends on a constant. A reference the snapshot resolves to no function leaves the call unplanned — a host function, a coroutine, and a closure allocated at runtime all name no `jit.Object.Fn` — which replaces the plan lowering's test that the recorded `Step.Callee` is positive evidence rather than the caller's own address; neither `Step.Callee` nor `Step.Shape` is read at a call any more. `frontend.TestStatic` and `frontend.TestTrace` assert that resolution over every call in every function either frontend emits.
- **Continuations.** A continuation recorded inside an inlined callee ends where that frame returns, and the caller must carry on. `jit.Plan` says this with `Edge.Tail`, a list of duplicated blocks its own block graph does not name; the SSA says it with an ordinary edge, so the recording that was cut out of the callee branches into the caller's block instead of the plan re-splitting the tail. `seams` opens that block at exactly the return the depth drops on, and only when a continuation was recorded at a cold target of a branch inside that frame, so a recording with no such fold lays out the same blocks the plan does.
- **Exits.** A plan leaves a cold target as an edge to `NoBlock`; the SSA lays out a real block that owns the live operands and ends on `OpExit`, because every SSA edge names a block.
- **No hoist, no carry.** `Plan.Hoist` and `Plan.Carried` have no SSA form, deliberately. `internal/ssa/transform.HoistPass` is the general loop-invariant code motion pass `hoistable` motivated, built over `internal/graph` dominance and `LoopHeaders` exactly as this section once said an optimizer pass could — but it hoists a narrower class than `hoistable` ever counted, not a broader one: an `OpConst`, or a pure, non-trapping `OpExec` whose arguments are all loop-invariant, moved into an existing preheader. It never touches an `OpGuardShape` or anything else that carries deopt state, because a guard is speculative — hoisting one into a preheader would run it (and risk failing it) on a loop trip count of zero the original program never reached — and because the state a guard resumes into names a frame chain valid at its original position, not at a point before the loop ran. Rereading `hoistable`'s three restrictions against that pass: "one container per loop" and `MaxHoistSlot` are both backend representation artifacts of caching one container's data pointer and length in fixed ARM64 registers, which `HoistPass` has no register budget to protect and so does not reproduce; the ref-array exclusion is real, but it belongs to the slice-header cache `internal/jit/arm64/control.go`'s own `hoist` builds to skip an element's retain/release accounting on every access, which `HoistPass` never attempts (it moves no `OpLoad`, no `OpStore`, and no `OpExec` that reads or writes `Heap`) — that hazard remains retain/release pairing's, still deferred until a backend consumes SSA. `Carried` now has a counterpart in `internal/ssa/transform.PromotePass`, which says the same thing one level up: a loop-carried scalar local is promoted out of its slot into SSA values with a block parameter on the back edge, so the backend sees a value with a live range instead of a slot to keep a register authoritative for, and every `OpState` names the value each promoted slot must be written back with - the IR form of `commitCarried`. It is not the whole of `Carried`: the seven-register budget, `MaxHoistSlot`, and the bridge rule that disables carrying are all backend and plan facts a target-independent pass has no counterpart for, and a ref local is promoted by neither.
- **Narrower than the plan.** A callee holding captures or a non-scalar local of its own is not inlined: an inlined frame reaches its upvalues through the closure reference it was called with, which a `Slot` cannot name, and a fresh frame sits over stale words, so filling a reference slot would release a count it never took (`OpStore` is release-then-write). A recording reaching a tail call ends there on `OpExit` (see Tail calls below), so the records after one - which the recorder writes in the reused frame, under the callee's own address - are never replayed. The recorded `Step.Arg` observations that specialize a divisor or shift do not become `OpGuardValue` yet, and `Step.Shape` for `ARRAY_SET`/`STRUCT_SET` does not become a guard — the IR carries no shape for a container store in either frontend. A call to a closure allocated at runtime is planned by neither frontend: the plan lowering pairs the recorded `Step.Callee` with the observed closure reference, and `jit.Objects` resolves only address zero, the constant pool, and host-bound addresses, so nothing in the snapshot maps a runtime closure to the function it calls. That is the same gap as the inlining restriction above and closes with it.

`jit.Bridgeable` still hardcodes ARM64 capability in the architecture-neutral layer, and the SSA frontend consumes it as-is. `backend.Machine.Lowers` and `backend.Machine.Traps` are the two capability questions that replace it (see SSA Backend Seam) — one refuses an opcode, the other ends the block on it; the frontend switches to asking the selected machine when the ARM64 backend moves onto that seam.

### Tail calls

`RETURN_CALL` retires the frame it runs in and enters another function at the same stack floor. Neither frontend states that: no operation of this IR replaces a frame, and no edge of a block graph leads into a function whose body the graph does not hold. Both therefore end the block on `OpExit` at the tail call's own IP, and the interpreter performs it from the flushed operand stack, which the exit hands over with every live reference owned - the threaded tail path releases the retiring frame's locals and every operand under the arguments, so a borrowed one would drop a count native code never took.

That is a real narrowing against the plan, which lowers two forms (see Calls and Returns): `tailLoop` for a tail call back to the compiling entry, and `tailMorph` for one into another function. Only the first has an SSA shape at all - a branch to the entry block, which is what a native loop back-edge is - and it is not taken here: the entry block is the root's own block, so it is the function entry only for a root anchored at IP 0, never for a loop header; nothing consumes SSA yet, so the shape would be unexercised; and it would make the entry a merge the fact fixpoint, `walk.edges`, and every `internal/ssa/transform` pass over loop headers must first learn. `tailMorph` has no SSA shape in either frontend, because neither holds the callee's body: the static frontend does not inline, and a recording past a tail call runs under the callee's address in a frame the caller's block cannot name.

The cost of exiting instead is measured rather than assumed, on the two `benchmarks/call_test.go` kernels written for it (`TailSum`, `TailPingPong`) - the first in the repository whose bytecode holds a `RETURN_CALL` at all. On the installed plan pipeline a tail call is a native **loss** today: both kernels run 15-28x slower with the JIT than threaded, and the loss is fixed per run rather than per tail call. The tail lowering itself is not what costs: a self tail call compiles once through the static frontend into a 260-byte entry that yields once per run, while the caller's trace side-exits at the tail call every run and is recompiled 250 times over 2000 runs (7.09 MB emitted, RSS bounded by retirement at ~195 MB). So the capability the SSA path forgoes is one that does not pay on the pipeline that runs.

`span.leaves` gives a tail call no successor, which `analysis.Blocks` has always agreed with: the bytecode after a tail call is entered only from elsewhere. A static plan wires that fall-through as a branch edge, so `frontend.TestStatic` drops it when comparing the two block graphs. A function whose only path to the code after a tail call is that edge is refused by both, because neither fixpoint reaches the block.

`ssa.Verify` rejects an `Operation` performing `BR`, `BR_IF`, `BR_TABLE`, `RETURN`, or `RETURN_CALL`: where control goes is what a `Terminator` and its edges say, and each of those already has one. That is the rule the old emission slipped through - it left `OpExec return_call` in the middle of a block and then returned with a clamped arity or branched into the code after the call.

## Trace Recording

Trace recording lives in `interp/trace.go`.

Recording clones the interpreter, starts the clone at the requested `(addr, ip)`, and executes threaded closures until it reaches return, loop back-edge, branch exit, unsupported operation, trace limit, or abort condition. A backward edge to a different header cuts the linear prefix so that header can become a standalone loop trace with a native safepoint budget. Reaching the trace limit records a partial trace with a resumable cut instead of aborting. Native execution deoptimizes at that cut; when the exit becomes hot, the existing side-exit machinery records and compiles the next bounded continuation.

A recursive `CALL` from a non-entry loop trace is a hard trace boundary. The recorder cannot model the recursive callee with `skipCall`, because that path supplies placeholder returns and does not reproduce the callee's heap mutations. The recorder therefore marks the `CALL` itself as a resumable cut; native execution falls back at that instruction, and a later hot continuation is recorded from the real frame and heap state. Function-entry traces retain native self-call lowering because that call is part of their native frame contract.

The live interpreter is not mutated while recording.

Each recorded step stores the data needed for speculative lowering:

- opcode
- function and IP
- inline depth
- observed call target
- observed callee address
- observed guard values
- observed heap shape
- branch target and taken state
- partial-trace resume boundary
- selected heap values for read-only fast paths

The tracer aborts before host calls and allocation. It records boxed-array writes, ref-field struct writes, and bulk mutations (`ARRAY_FILL`, `ARRAY_COPY`, `ARRAY_APPEND`, `MAP_SET`) only as terminal fallback boundaries. A primitive typed-array write or a scalar struct-field write may remain inside the trace when it occurs in the anchor frame before any inlined call. Capture clones every overlapping visible range of aliased primitive typed arrays into one replacement backing store, preserving slice offsets while leaving the live heap unchanged. Boxed arrays and structs are copied before their terminal mutation. The clone also owns mutable dispatch metadata and suppresses external finalizers, so speculative reference reclamation cannot alter live functions, trace trees, or host resources.

Every recorded `trace` has one status: `fallback`, `loop`, `returned`,
`completed`, `partial`, or `aborted`. `fallback` is an explicit usable linear
prefix that ends in threaded fallback. The trace frontend maps usable statuses
to plan terminators and excludes aborted fragments from learned continuations.
A loop anchor accepts a `loop` root or a `returned` straight-line root: a body
that hits a terminal boundary before its back-edge compiles as a per-entry
prefix that deopts at the boundary and re-enters at the header next iteration.
When an observed block runs out of steps, lowering decides completion from that
block's terminator, never from the root trace. This prevents an unsupported side
fragment from being mistaken for normal completion.

`tracer.publish` assigns the status and returns the capture result. It publishes
accepted and partial roots under the tree lock. `fallback`, `loop`, `returned`,
and `completed` map to `CaptureOutcomePublished`; `partial` maps to
`CaptureOutcomePartial`; and `aborted` maps to `CaptureOutcomeRejected` without
publishing the trace.

`tracer.capture` serializes recording and returns an already-published root when
one exists, so sampling and compilation cannot record the same entry
concurrently. A tracer is bound to one `program.Program`; `New` isolates it with
a fresh tracer instead of reusing one bound to another program.

`tracer.headers` (the static loop-header scan) uses `instr.Targets(code, ip)` rather than switching on `BR`/`BR_IF` directly, so a loop formed only through a backward `BR_TABLE` case target is recognized as a header too.

Threaded back-edge handlers report loop hotness directly; forward branches carry no hotness state. The hotness policy and thresholds are defined in `docs/profile.md`.

## Trace Snapshots

A pool shares one private `tracer`. Tree mutations are locked. `rootAt` returns a stable snapshot containing immutable trace pointers plus copied branch and hit containers.

`tracePlan` converts that snapshot into flat plan blocks (`frontend.Trace` converts the same snapshot into SSA; see SSA Trace Frontend). It excludes aborted fragments and loop-kind legs (a loop-kind leg is a loop root of its own: anchored at this header it is the root itself and its edge already wires to the root block, anchored elsewhere it is a different loop with its own native entry), sorts continuation roots deterministically, and connects internal paths by block ID rather than exposing the trace tree to lowering.

In a loop plan, a partial leg whose cut lands on the plan's own header (same function, depth 0) folds into the loop back-edge: `split` emits a real branch terminator instead of a fallback, `wire` resolves it onto the root block, and lowering takes the committing-flush native back-edge, so an in-loop branch that rejoins the header no longer exits native code (issue #155). A cut record that directly follows an explicit branch with the same target ends the split without materializing a spurious block, leaving the branch edge for `wire` to resolve. Cuts inside an inlined frame or to any other location keep the deopt fallback.

## Backend

`internal/jit.Compiler` (built by `jit.New`) is architecture-neutral: it picks the arch and `Machine` its caller supplies, builds the assembler, and hands both to one of that machine's two seams. `Machine.Compile(a *asm.Assembler, input *jit.Input, root jit.Anchor) (jit.Entry, bool)` takes a root and plans it itself, which is the SSA pipeline; `Machine.Lower(a *asm.Assembler, input *jit.Input, p jit.Plan, nativeLoop bool) ([]jit.Exit, bool)` takes a plan the Compiler's own frontends built, which is what every root the SSA machine still declines compiles through. `jit.Input` and `jit.Plan` live in `internal/jit`; all lowering state — `lowering`, the symbolic `value` stack, inlined `activation`s, deferred `work`, and queued `sideExit`s — lives on the machine's side of that seam, private to `internal/jit/arm64`. `interp`'s build-tagged `jit_arm64.go`/`jit_stub.go` are the arch selector: on arm64, `newCompiler` builds `jit.New(arm64.New(), newMachine())` where `newMachine` calls `internal/jit/arm64.New()`; on every other architecture `newCompiler` returns `(nil, nil)` and the unavailable backend is never constructed.

`internal/jit/arm64` owns all ARM64 lowering, split by concern across `machine.go` (orchestration and the `Machine`/`lowering` types), `dispatch.go` (the single opcode dispatcher), `control.go` (control flow), `numeric.go` (numeric operations), `call.go` (calls and frames), `deopt.go` (deoptimization), `heap.go` (heap access), and `ref.go` (reference ownership).

Every plan block passes through one `emitBlock` path and every edge carries an explicit block ID or an unresolved threaded-fallback anchor. Bytecode locations describe source positions only; block IDs preserve distinct inlined contexts even when they share the same `(function, IP)`. A state-backed block reloads VM homes, while a profiled successor may continue with the current symbolic state.

Caller continuations are ordinary blocks in the same flat block pool. A cold edge carries the continuation block IDs that must run after an inlined callee returns. A deferred edge receives a label and a canonical symbolic snapshot (register-free values, reset locals); `label` shares the label of a previously scheduled continuation only when block, tail, and canonical snapshot are identical, and its ledger keeps consumed work items so folded legs that branch into one another (a loop nest) converge instead of exhausting the continuation limit. States are never merged by bytecode anchor alone.

### SSA Backend Seam

`internal/jit/backend` is the architecture-neutral half of an SSA backend, and
`backend.Machine` is the seam an architecture implements under it.
`backend.Compile(m, a, in, root, f)` drives one `*ssa.Function`, entered at one
`jit.Anchor`, through one machine into one `asm.Assembler`, and returns the
layout it chose, the exit descriptors it registered, and the bridge resume
points its callable may be re-entered at.

`backend.Root(m, a, in, root)` is one whole compile above it: it plans `in` at
`root` through the frontend the anchor implies - bytecode first for an entry
root, the recording first for a loop root, the plan pipeline's own order - and
lowers what that planned through `Compile`, reporting the `jit.Entry` facts
publication needs. Only the first frontend that plans the root gets an attempt,
because a `Lowering` that declines leaves its own instructions in `a` and `a` is
the caller's to discard; that costs no coverage, since the caller still has the
plan pipeline, which tries both. `Root` is why `backend` may import
`internal/jit/frontend`: a `jit.Compiler` cannot, because the frontend imports
`internal/jit` back.

Both seams exist until the ARM64 lowering is ported: `internal/jit/arm64`
implements `Machine.Compile` over the SSA it has learned so far and
`Machine.Lower` over `jit.Plan` for everything else.

The seam is four hooks, opened per compile so one `Machine` serves concurrent
compiles and holds none of their state:

| Hook | Contract |
|---|---|
| `Machine.Lowers(instr.Opcode) bool` | can the machine emit native code for this opcode at all. `Compile` refuses a function holding an `OpExec` the machine declined, so a `Lowering` never sees one. It judges only the blocks it lays out: an opcode reachable solely behind a trap is code no native path runs, and refusing the function for it would throw away the prefix the trap exists to keep |
| `Machine.Traps(instr.Opcode) bool` | does lowering this opcode end the block by handing control back — the unconditional terminal exit ARM64 emits today for the map, string-iteration, bulk-array, `MAP_SET`, `REF_TEST`/`REF_CAST`, error, `THROW`, and `YIELD`/`RESUME` opcodes. The opposite answer to `Lowers`: a trapped opcode *is* lowered, as an exit, so `Traps` is asked only of one `Lowers` admits |
| `Machine.Open(*Compiler) Lowering` | begins one compile; the returned `Lowering` holds that compile's state |
| `Lowering.Enter() bool` | the callable prologue, emitted before the first block's label is bound. Every block already has its label, so a prologue that dispatches an external bridge re-entry can branch to one |
| `Lowering.Lower(block int, ops []ssa.Operation) (int, bool)` | emits `ops[0]` and reports how many of `ops` it consumed. Consuming more than one is how a machine fuses an adjacent run; the compiler calls again from the first operation left. `ops` ends at the block's trapping operation, so a fusion can never reach past the point control leaves |
| `Lowering.Term(block int, ssa.Terminator) bool` | ends the block, unless a trap already did. No label is bound after it, so a machine asks `Compiler.Next` which block falls through |
| `Lowering.Leave() bool` | emits what lowering deferred — the cold stub behind each guard, any continuation it scheduled |

Anything else a machine needs it asks the `*Compiler` it was opened with:
`Asm` (the assembler it emits, allocates temporaries, and takes labels from),
`Input` (the compile-time snapshot), `Root` (the anchor this compile is entered
at), `Func` (the IR, for a value's type or a block's parameters), `Reg` (the
virtual register bound to a value), `Def` (the operation defining a value),
`Block` (a block's label), `Next` (the block laid out after one), `Bridges`
(the external re-entries an entry dispatch must answer), `Moves` (an edge's
block-parameter copies), and `Exit` (the journal words a deoptimization
writes).

The entry kind is not carried anywhere: `jit.Anchor.Kind` derives it, because
an offset inside a function is a loop header, address zero at offset zero is
the module body, and any other address at offset zero is that function's own
entry. `Plan.Valid` checks its `Kind` field against exactly that. So `Compile`
takes the anchor alone, `Compiler.Root` hands it to the machine, and
`ssa.Function` — the same IR the ahead-of-time optimizer builds, which has no
anchors and no entries — gains nothing.

The neutral layer owns exactly four decisions, because each is stated by the IR
rather than by a target:

- **Layout.** Blocks are emitted in reverse postorder, with the depth-first
  walk taking successors back to front so the first edge of a terminator is the
  one laid out next. A block precedes every block it dominates and a loop body
  stays contiguous. A block that traps reaches none of its successors, so the
  walk stops there and lays out nothing only that block led to; the compiler
  also stops the block at the trap, handing `Lower` no operation after it and
  calling no `Term`, since control has already left. `Code.Order` reports it.
- **Registers.** Every value is bound to one `asm.VReg` for the whole compile,
  allocated in value order before lowering starts, so the assignment is
  deterministic. An integer or reference takes a 64-bit integer register, an
  `f32` a 32-bit float register and an `f64` a 64-bit one — the lane each
  representation occupies. A machine that wants a narrower view derives it.
- **Edge copies.** `Compiler.Moves` turns an edge's arguments into the copies
  its target block's parameters need, ordered so no register is overwritten
  before it is read, with a fresh temporary parked in front of a cycle (the
  swap two loop-carried values make on a back edge). Placement stays the
  machine's, because only it knows whether a copy fits before its branch or
  needs a landing pad.
- **Deopt metadata.** `Compiler.Exit` resolves the `ssa.Frame` chain an
  `OpState` carries into `backend.Deopt`: `Resume` is `journal.CellNextIP`,
  `SP` is `journal.CellSP`, `Stack` is every live operand with the VM stack
  slot it must be boxed into and whether that entry already owns its reference
  count (`Flush.Owned`, taken from the `ssa.Operand` the frame carries, which
  is what tells a cold stub which entries to retain), and `Frames` are the
  `journal.Record` rows. Every coordinate is a delta from the entry frame's
  base, exactly as the ARM64 `trapFlushed` writes them. A frame's operand sits
  at its `Base` plus its function's `types.Function.Declared()` slot count plus
  its position, so a compile whose frame address names no function is refused
  rather than resuming at a wrong slot. `Frames` is **innermost first**, which
  is the order `journal.At` indexes records in and `Interpreter.deopt` reads
  them back in — the reverse of the outermost-first order `ssa.Frame` chains
  are built in.
  `Exit` also assigns the descriptor `journal.CellExitID` reports (`ID+1`, so
  `ID == -1` writes the zero that means none, which is what a bridge and a
  yield take), and `Code.Exits` is those descriptors in assignment order.
- **Bridge resume points.** A block whose last operation is an `OpBridge` and
  which leaves by a single edge creates one external re-entry: the interpreter
  runs that opcode and leaves execution at its own IP plus its encoded width,
  in the block that edge names. `backend.Bridge` is that pair, `Code.Bridges`
  the list, and `Compiler.Bridges` the same list during the compile, because
  the entry dispatch that turns the journal's IP into a branch is emitted in
  `Enter`. `jit.Entry.Resumable` is these IPs.

Everything else stays the machine's: the instructions, the prologue, the
journal stores, the cold stubs and their retains, register pinning, and the
symbolic operand state a stack-machine lowering keeps.

Two lowering facilities the ARM64 backend uses today are preserved by name.
`dispatch.go`'s `fuse` reads the step *after* the one being lowered, which is
`Lowering.Lower`'s window and consumed count. `value.known`/`imm` reads a
constant *behind* an operand — the fold that turns a shift amount into an
immediate and elides a divide-by-zero guard — which is `Compiler.Def` returning
the argument's `OpConst`. A seam offering only one of the two would cost the
backend an optimization it already has.

#### ARM64 SSA machine

`internal/jit/arm64`'s `machine` and `emitter` are the ARM64 side of that seam,
reached through `arm64.New().Compile`, split across `emit.go` (the machine, the
prologue, and one operation's instructions), `guard.go` (a speculated shape and
the cold stub it leaves through), `read.go` (an array read), and `struct.go` (a
struct or `*HostStruct` field read). They are
deliberately small: an immutable machine holding the pinned journal registers,
and a per-compile emitter holding the frame base every local slot is addressed
from, the frame facts a prologue and a teardown are shaped by, which blocks are
already laid out, the heap cell each guarded container was walked to, and the
cold stubs its guards branch to. Everything else - a value's register and type, a block's label, the
block that falls through, an edge's copies, a deopt's journal words - it asks
the `backend.Compiler` for, which is why it needs none of the plan `lowering`'s
symbolic operand stack, inlined activations, deferred work, or backing
inference.

A value's representation is a function of its `ssa.Type`, so no per-value state
records it: an `i1`, `i8`, and `i32` carry their value in the low 32 bits of a
64-bit integer register with the bits above undefined, exactly as the boxed
word leaves them, so unboxing one is free and boxing it is a mask and a tag; an
`f32` and an `f64` live in the float bank at their own width, so an arithmetic
sequence costs one move in per operand and one out at the frame boundary rather
than a pair around every opcode.

What it lowers, and therefore what `Lowers` admits: `i32` arithmetic, bitwise
operations, shifts, comparisons, and `eqz`; `f32` and `f64` arithmetic, `abs`,
`neg`, `sqrt`, and comparisons; compile-time constants, a pool reference
included; local and global slot loads, and stores to a slot that cannot hold a
reference; `OpGuardShape` over an array or a struct shape; `ARRAY_GET` of a
scalar element; `STRUCT_GET` of a scalar field, against a `*types.Struct` or a
`*HostStruct`; `OpJump` and `OpBranch`; and `OpReturn` and `OpComplete` for a
function and a module entry. `Traps` is false throughout: an opcode this
machine cannot compute it declines outright rather than running as an exit,
because an unconditional exit ends the block, and every exit it emits is the
cold path behind a guard the hot path falls through.

**Guards and their cold stubs.** `guard.go` lowers `OpGuardShape` as the tag
test, heap-cell walk, and itab compare the plan pipeline writes once per access
in `guardHeap`/`guardItab`, and hands the cell's data word to the read behind
it so that read loads through the same cell instead of resolving it again. It
runs *before* the access it admits, which is what Speculation requires. It
proves only `Shape.Itab`; a struct's `Shape.Typ` (its concrete
`*types.StructType` identity, cast to `uintptr` the same way an array's itab
is) and a host view's `Shape.Host` (the Go kind its field converts through)
are proven by the read behind the guard instead, because each is a per-field
or per-container-instance fact the itab alone does not decide (see `struct.go`
below).

A guard and its read are **the one shape this machine fuses**: `Lower` sees
`OpGuardShape` followed by `OpExec ARRAY_GET` or `OpExec STRUCT_GET`, consumes
both, and lowers neither alone. `Shape.Host` tells `STRUCT_GET`'s two
containers apart at that point - zero for a `*types.Struct` guard, the field's
reflect.Kind for a `*HostStruct` one - so the opcode and the shape together
name exactly one read, and the fusion is total (see `emit.go`'s `Lower`
comment for why this is load-bearing rather than a redundant check). A guard
and its read are one bytecode operation and resume into one interpreter
state, and that state describes the operand stack at that instruction and
nowhere else - so a read's own bounds and kind tests leave through the
guard's state, which is sound exactly while nothing stands between them.
Fusing makes that structural rather than a rule to keep true: `Compile` hands
`Lower` one block's operations, truncated at its trap, so `ops[1]` can never
be in another block or past the point control leaves.

**Struct and host-struct reads.** `struct.go`'s `structRead` and `hostRead`
extend the guarded-read shape with the check a struct's fields need that an
array's uniformly-typed elements do not: a struct's fields differ in kind by
index, so the field the guard and the compiled index admitted is also proven
against the kind the frontend resolved for it (`ExitGuardKind`, a third exit
alongside the shape and bounds ones) before the load runs. `structRead`
proves `Shape.Typ` itself, reusing `guard`'s own exit label, since a type-
pointer mismatch is the same shape failure a wrong itab is; `hostRead` walks
`jit.Layout`'s host offsets exactly as `heap.go`'s `hostGet` does, bounds-
checks the field index against the view's own layout, and compares the
field's recorded Go kind against `Shape.Host` before choosing the load width
and extension `HostShape.Read` names. Neither reads a field whose result kind
this machine has no lane for: an `i64` field may be heap-promoted (the
boxability guard this machine does not emit) and a `ref` field is owned by
whoever receives it (the retain this machine does not emit outside a cold
stub), so both decline exactly as `read`'s array-element lowering already
does.

`emitter.exit` resolves the guard's `OpState` through
`Compiler.Exit`, reserves a label, and queues a `stub`; `Leave` emits every
stub after the last block, so the hot path carries one rarely-taken branch and
none of the stores. A stub boxes each `Deopt.Slots` entry into its VM stack
slot, publishes `CellSP`, writes one `journal` frame record, and reports
`TrapFallback` with the descriptor and the resume IP. A flushed entry whose
`Flush.Owned` is false and whose type is `ref` is retained there and only
there: the resumed interpreter adopts every stack entry and releases it, so a
borrowed one - a pool constant, a value read out of a global slot - would be
released once more than it was retained. A null reference is skipped, because
`Interpreter.releaseBox` releases no reference to the permanently-null cell
zero and the count would be one nothing drops.

Everything else declines, which abandons the compile and leaves the root to the
plan pipeline. It declines a loop entry (a live frame it must not unwind) and a
back edge to a block already laid out (a safepoint budget and loop-carried
registers it does not emit); an edge carrying block-parameter arguments (edge
copies it does not place); `OpGuardKind`, `OpGuardBounds`, `OpGuardValue`,
`OpRetain`, `OpRelease`, `OpBridge`, `OpExit`, `OpSuspend`, and `OpTable`; a
frame declaring a reference local or returning one, and a store into a slot that can
hold a reference (reference counts it does not account for); an `i64` value
anywhere (the boxability guard it does not emit); an upvalue slot (a base only
the closure resolves); and a `Slot` whose `Base` is not the entry frame's (an
inlined callee's storage).

The other heap reads the plan pipeline lowers are blocked by the IR rather than
by this machine, and unblocking them is a frontend change. `REF_GET`,
`ERROR_GET`, and `CORO_VALUE` push `KindAny`, which `frontend`'s bytecode walk
cannot type, so no function holding one is planned at all. `ARRAY_LEN`,
`STRING_LEN`, and `CORO_DONE` are planned, but the frontend emits no
`OpGuardShape` in front of them and their `OpExec` only reads the heap, so it
carries no `OpState` either: there is no shape to load through and nowhere for
a mismatch to exit to.

`STRUCT_GET` no longer belongs to that list: `Shape.Typ`, despite its `uintptr`
representation in the IR, was never opaque to the compiler - the frontend
(`frontend/walk.go`'s `field`) already dereferences the `*types.StructType` it
names to resolve the field's kind before the SSA node exists, exactly as it
resolves an array's element kind for `ARRAY_GET`; the backend only ever needed
the *result*, carried as the read's own SSA type, plus the runtime identity
and per-field kind checks `structRead`/`hostRead` now emit. The blocker this
paragraph used to record - that lowering it soundly meant a runtime
field-kind guard and the second exit reason the `*HostStruct` path needs
anyway - is exactly what `ExitGuardKind` and `Shape.Host` above resolve; see
"Struct and host-struct reads."

An entry it does emit is `BLR`-compatible with the plan pipeline's own callers,
because both publish into `i.natives`: it clears its own non-parameter locals in
the prologue, reads parameters from the VM stack, writes each boxed result to
its frame slot and into the ABI return register, and never writes the caller's
journal cells.

#### Observability and golden code

Every stage of the pipeline is inspectable, so a test can state the machine
code a shape should compile to instead of accepting whatever lowering emits.
`Code.Order` is the layout, `Code.Exits` the descriptors, `Compiler.Exit`'s
`Deopt` the metadata, `asm.Assembler.Instructions` the emitted stream before
register allocation, and `asm.Assembler.Alloc` the stream `Build` encodes,
after allocation and spilling. `Alloc` repeats the allocation rather than
consuming it, so inspecting does not spend the assembler.

Golden machine-code tests belong beside the machine that emits them
(`internal/jit/arm64`), not here: a test builds an SSA function, runs
`backend.Compile` with the real machine, and asserts the two instruction
streams plus the layout and exits. `internal/jit/backend`'s own tests use a
recording machine that emits nothing, because the neutral layer's product is
the order, the registers, the moves, and the metadata.

#### Where optimization belongs

- Target-independent rewrites belong in `internal/ssa/transform`, which already
  owns folding, load forwarding, CSE, guard elimination, hoisting, and DCE, and
  is where retain/release pairing, critical-edge splitting, and block-parameter
  copy coalescing go. A pass reimplemented per machine is a pass in the wrong
  package.
- Instruction selection, addressing modes, immediate folding, redundant-move
  elimination, and peepholes are the machine's, judged against golden output.
- Layout, value-to-register binding, edge-copy sequencing, and deopt metadata
  are `internal/jit/backend`'s. Anything here that turns out to be
  target-specific moves out to a machine rather than growing a policy knob.

#### What the seam still cannot say

- **A bridge a block does not end on.** The static frontend cuts a span at
  every bridgeable opcode, so its `OpBridge` is always a block's last
  operation. The trace frontend does not: a bridgeable opcode recorded
  mid-trace becomes an `OpBridge` with operations after it in the same block.
  Such a bridge names no resume point, so the callable traps there and falls
  back instead of resuming. Splitting the recorded block at it is the trace
  frontend's fix, not the seam's.

## Trace ABI

Native callables use an AAPCS64-shaped entry.

`Callable.Call(ctx unsafe.Pointer)` passes:

```text
&i.journal[0] in X0
```

Native code loads VM state from the journal into pinned scratch registers.

| Name | ARM64 | Purpose |
|---|---|---|
| `scratchStack` | X10 | `&i.stack[0]` |
| `scratchGlobals` | X11 | `&i.globals[0]` |
| `scratchBP` | X12 | current frame base |
| `scratchSP` | X13 | current stack pointer |
| `scratchCtrl` | X14 | journal pointer |

The context stays an `unsafe.Pointer` through the Go call boundary so Go can
relocate a stack-backed context when the trampoline grows the goroutine stack.
Converting it to `uintptr` before that stack split would leave native code with
a stale address.

The Go trampoline preserves X19-X26 and declares an 8,192-byte native reserve:
`asm.MaxSpillSlots` (512) × 8 bytes plus `interp.nativeFrameLimit` (128) × 32
bytes. Its complete Go frame is 8,272 bytes including the 80-byte trampoline
area.
Native code starts at the top of the reserve, so generated SP adjustments stay
inside memory covered by Go's stack-growth check. X26 is the stable spill-frame
base, so a native self-call may move SP without changing spill addresses.

`internal/asm/arm64` owns the byte arithmetic behind that reserve so it has one
definition instead of being restated wherever it is needed:
`arm64.SpillBytes` derives the spill portion from `asm.MaxSpillSlots`, and
`arm64.StackReserve(recordBytes, callDepth)` / `arm64.FrameSize(recordBytes,
callDepth)` compute the reserve and total Go frame size for a caller-supplied
record width (see `journal.Shift`) and call-depth cap. `internal/asm/arm64`
has no dependency on `internal/journal`, so `recordBytes` is the caller's
concern, not this package's. A hand-written `.s` literal cannot read a Go
constant, so `abi_arm64.s`'s two literals still need a test to keep them
honest, split by what each side can see: `interp.TestARM64_StackReserve`
(`interp/tier_test.go`) asserts `arm64.StackReserve(1<<journal.Shift,
nativeFrameLimit)` equals the `ADD $N, RSP` reserve literal — the half that
needs interp's private `nativeFrameLimit` — and `arm64.TestFrameSize`
(`internal/asm/arm64/stack_test.go`) asserts the `TEXT ·invoke(SB), $N-16`
literal equals that same reserve literal plus `arm64.SaveAreaBytes`, entirely
within `internal/asm/arm64`. An edit to `asm.MaxSpillSlots`, `nativeFrameLimit`,
or either `.s` literal without updating the others fails one of these two
tests instead of corrupting the native stack at runtime.

Register allocation (`internal/asm/rewriter.go`) is a single forward pass over the instruction stream: it binds a vreg on first use, releases it at its last reference, and — when the physical bank is exhausted — spills the bound integer vreg whose final use is farthest ahead to a stack slot at the point pressure was observed, reloading it on demand. Rewritten labels target the start of any inserted reload/store prefix, and labels on a return target its inserted frame epilogue. A call whose target label is bound in the same build runs through the shared epilogue on return, so the rewriter reserves the caller's spill area again after it (`Frame.Resume`, not a fresh `Frame.Enter`).

Spill *eligibility*, though, is not decided from the flat instruction stream: `rewriter.run` builds a control-flow graph once up front (`internal/asm/block.go`), computes dominance over it by handing that graph to `internal/graph` — an IR-neutral leaf package holding the dominance and natural-loop-header algorithms, wrapped for instruction-position queries by `internal/asm/dominance.go` — and indexes every instruction position that reads each vreg (`uses.go`) — replacing an earlier design where a value was ineligible whenever any label sat between its store and its last use, and a build containing any backward branch disabled spilling entirely regardless of whether a given value was anywhere near the loop. `rewriter.crosses` combines three independent checks, each catching a hazard the others cannot see:

- **Dominance.** A value may be spilled at a point only if that store dominates every one of its remaining uses — every path from the entry to a use must pass through the store. This is the literal question the earlier "no label in between" rule only approximated: a value confined to one pass through a loop body dies before the back-edge and is redefined after it, so it is now spillable for the first time, while a value genuinely carried into the next iteration still fails, because the first pass through the loop reaches its use without ever running a store issued later. `TestAssembler_Build/spills a value live across a forward branch` and `.../declines a value spilled on one diamond arm and reloaded on the sibling arm` exercise this against a merge point and a bare sibling arm respectively; `.../spills a value confined to one loop iteration` and `.../declines a value live across a back edge` exercise it against a real back-edge.
- **Loop-carried self-reference (`internal/asm/carry.go`).** Dominance alone is not sound for a redefinition that reads its own value — an accumulating `ADD dst, dst, x`, the shape a mutable loop-carried local takes in this flat, non-SSA IR. Such a redefinition's own store trivially dominates its own reload under the ordinary definition, because dominance counts paths, not how many times a loop body re-executes; but a spill inserts exactly one store and one reload instruction, not one pair per iteration, so a reload sitting at that redefinition would replay whatever the store captured once rather than the previous iteration's update. `carryHazards` finds every self-referencing redefinition a natural loop governs — using `internal/graph.LoopHeaders` to find the loop headers — and `crosses` declines to spill across it unless the store itself sits inside the same loop, refreshing on the same schedule. `TestAssembler_Build/declines a value live across a back edge` is the case this rule alone catches: dominance would accept it.
- **Self-recursive calls (`rewriter.barriers`).** A `BL` to a label bound at or before the call site shares the caller's spill frame (`Frame.Resume`) rather than getting a fresh one, so a value the caller still needs after such a call is unsound to leave spilled across it even though the store trivially dominates the reload in the caller's own single-execution control flow — two activations of the same code are sharing one physical slot, which no dominance or liveness query is about. `TestAssembler_Build/self-recursive call clobbers the caller's spill slots` proves the build rejects rather than emits that overwrite.

A container store (`ARRAY_SET`, `STRUCT_SET`) carries no spill restriction of its own now that dominance judges every spill on its own merits; it used to force `Plan.NoSpill`, disabling the whole build's spill frame, because the allocator could not yet tell a sound spill from an unsound one around a store's own branches.

Native code does not marshal parameters or returns. It writes results and trap state into the journal, and the Go wrapper restores interpreter state from there.

## Frame Journal

`i.journal` is owned by `Interpreter`. It is both input context for native entry and output state for deoptimization. `internal/journal` owns the cell, record, and trap layout: `journal.Cell` indexes a header cell, `journal.Record` indexes a field within a frame record, and `journal.Trap` is the exit kind stored at `journal.CellTrap`.

Header cells come before fixed-stride frame records (`journal.Stride` cells wide, indexed by `journal.Record`).

| Cell | Purpose |
|---|---|
| `journal.CellStack` | stack base pointer |
| `journal.CellGlobals` | globals base pointer |
| `journal.CellBP` | current frame base |
| `journal.CellSP` | stack pointer |
| `journal.CellEntry` | bridge resume IP in; zero starts at the anchor |
| `journal.CellDepth` | number of written frame records |
| `journal.CellCap` | available frame record capacity, capped at 128 |
| `journal.CellTrap` | trap state (`journal.Trap`) |
| `journal.CellNextIP` | fallback or resume IP |
| `journal.CellBudget` | native loop back-edge budget |
| `journal.CellActive` | active native call depth |
| `journal.CellRC` | refcount base pointer |
| `journal.CellUpvals` | closure upvalue base pointer |
| `journal.CellHeap` | heap base pointer |
| `journal.CellNatives` | fixed per-function native-entry slot base |
| `journal.CellExitID` | fallback descriptor ID plus one; zero means no descriptor |
| `journal.CellHead...` | frame records `{journal.RecordAddr, journal.RecordBP, journal.RecordIP, journal.RecordReturns}` |

On guard failure, native code writes live stack state, appends frame records, sets trap state, sets the resume IP, and returns to Go.

The Go wrapper rebuilds the VM state and resumes threaded execution.

If the fallback IP is `0`, the wrapper runs the shadowed threaded entry handler once to avoid immediate native re-entry.

### Lifecycle Profiling

Observable profiling is enabled only by an explicit profiler. Internal hotness sampling alone does not emit detailed rows.

Each published native entry carries its frontend, own byte size, and immutable
exit descriptors. Installation resolves stable local counters for entry, yield,
and every descriptor. Native wrappers increment those handles directly; they do
not construct labels.

Every fallback creation site assigns a descriptor with a stable reason. It uses
the concrete source opcode when the fallback is attributable to one; synthetic
boundaries such as an `opLimit` trace cut use `none`. Generated code writes
`descriptor ID + 1` to `journal.CellExitID` before returning with `journal.TrapFallback`. The
Go wrapper resolves that ID and counts the exact exit row. Zero means no
descriptor. `journal.TrapYield` counts only a yield, and native frame overflow counts
neither an exit nor a yield.

Compile and emission ownership follows compilation ownership: the interpreter
that claimed the build records its result, and with a shared queue that is the
winning member only. That holds when the build ran on the queue's worker too -
the result is parked and recorded by the claiming interpreter at its next
safepoint, never by the worker, because a `prof.Collector` belongs to one
interpreter. Peers install their own runtime counters without duplicating
compile or emission rows. Collector flush preserves registered handles while moving accumulated
values to the shared profiler.

## Speculation

Observed numeric and heap facts are speculative unless they come from bytecode constants.

Native code may specialize on observed values, but a mismatch must exit before the opcode executes. The threaded handler owns the general case.

This rule keeps native lowering small.

## Calls and Returns

Native lowering supports selected calls:

- direct `CONST_GET function; CALL`
- guarded function-value calls
- eligible closure-body calls

A call may lower to native `BL` when the observed target is a JIT-eligible `*types.Function` with matching arity. Which function a recorded call entered is a recorded fact, not a compile-time heap read: the tracer stores the entered frame's address in `Step.Callee` and the callee operand's own itab in `Step.Shape`, so a closure call is recognized by `Shape.Itab == jit.HeapClosure` — whose `Fn` is already what `Callee` names — and every other observed value must be the callee function itself. The shape has to be positive evidence rather than "the operand differs from `Callee`", because a host call records the *caller's* address as `Callee` and would otherwise be mistaken for a closure call into it.

Unsupported targets fall back, including host calls, allocation, maps, unsupported functions, unsupported closures, and heap mutations outside the selected guarded fast paths.

Static plans recognize direct `CONST_GET function; CALL` pairs. Each interpreter owns a fixed-size `natives` slot array; installing or synchronizing a function entry publishes its executable address atomically. The caller loads the slot at runtime and uses `BLR`, so compile order does not matter: a null slot falls back at the CALL, while a later callee installation is visible without recompiling the caller. Supported `RETURN_CALL` paths use native tail-loop or tail-morph lowering.

A call whose callee is the function being compiled uses the native self-call path (`BL` to `ctx.head`) only when the current native frame is that entry plan's own frame and the target has no captures. `selfCall` owns those ABI preconditions; the call sites validate return kinds before consuming the call marker, then either enter `selfCall` or keep the ordinary call path. An inlined frame or loop plan therefore never re-enters an entry prologue over a foreign activation. Lowering the fused form is what lets the static frontend plan a recursive function at all; while it was rejected, such a function had no whole-function plan and fell back to a recorded trace, whose coverage varied with how much of the recursion the recording happened to reach. `TestARM64_SelfCallFromInlinedFrame` covers the foreign-frame case, while `TestARM64_SelfCallWithRefArg` ensures the safe whole-function form still installs native code. Recursive calls encountered inside loop traces are still cut at the call so the continuation is compiled from the real post-call state.

A callee frame's non-parameter locals are cleared by the callee, in the entry prologue at `ctx.head` (`zeroLocals`), not by its callers. Every entry path arrives there with `bp` already pointing at the new frame — the Go wrapper, `directCall`'s `BLR`, and `selfCall`'s `BL` — so one clear covers all three, and it matches what threaded `CALL` does before transferring control. Only a whole-function entry may do this: a loop plan re-enters a frame whose locals are live, and module code has no caller that would have cleared them. Skipping it hands the callee stale boxed words from whatever frame last occupied that stack region, so its first `LOCAL_SET` releases a ref it never owned and `RETURN` teardown releases the rest. `TestARM64_CalleeLocals` covers both native call paths; a function with no non-parameter local cannot expose it, which is the shape every earlier self-call test used.

Native calls are frame-aware. The lowering checks frame budget, increments native depth, saves caller state, publishes the callee BP/SP into the journal, enters the callee trace, and restores the caller state and journal frame on normal return. The journal publication is required because every native entry prologue reloads BP/SP from `journal.CellBP`/`journal.CellSP`; without it, nested native entries inherit the outer caller's frame and mutual recursion can keep reusing the wrong argument slot until frame overflow. A trap leaves the callee's journal state intact for deoptimization, while the normal path restores the caller before continuing. `TestARM64_MutualEntries` covers two independently installed native entries calling each other.

A native call invalidates the caller's cached local registers: the callee owns every allocatable register, so the call sites clear `activation.state` on return, and the committing flush before the call leaves the VM stack slot authoritative. `activation.locals` still names the register each value was last materialized into, so `activation.isLoadedAt` is the one test for whether that name is still good. `guardFrame` reads every ref local for the frame teardown; boxing an unloaded one releases whatever the callee left in that register, which faults inside the Go runtime rather than diverging quietly. `TestARM64_SelfCallFrameLocals` pins it.

X26 carries the caller's spill base across a `BLR`. The callee is entered at its own offset zero, so it runs the frame prologue and repoints X26 at its own frame; the caller saves X26 into its 32-byte save area before the call and restores it immediately after, on both the normal and the trap path. A self-call (`BL` to `ctx.head`) needs no such save: it shares the caller's frame rather than getting one of its own, so any value the caller still needs after the call is barred from spilling across it (`rewriter.barriers`, under Register Allocation above) instead of the whole stream losing its spill frame outright.

On deoptimization, native frames append enough journal records for Go to rebuild the VM call chain.

`RETURN` closes a function entry trace only when it returns from the outer recorded frame. Inlined callee returns stitch values back into the caller's symbolic stack. `RETURN_CALL` tail-loop and tail-morph paths first preflight the retiring activation, then own forwarded arguments and release the retiring frame.

A call may return a ref. `checkReturns` admits `KindRef` alongside the scalar kinds, and both branch paths push the result as a boxed operand (`raw: false`) owning exactly one retain, the same shape `ARRAY_GET` and `STRUCT_GET` produce.

Native frame teardown mirrors threaded ownership: `stitch` and `ret` guard the retiring refs, preserve returned refs that still point into the frame, and finally `releaseFrame` drops the owned refs before the frame is removed. The guard counts duplicate addresses together; native teardown deoptimizes when any address cannot cover all pending releases, so native code never decrements an object to zero without the interpreter's reclaim path.

`ret` must take the returned refs' retains *before* that guard, not after. The guard deopts when `rc <= pending`, and the common shape — return a freshly allocated object held only by one frame local — sits exactly at `rc == pending`. Owning first raises `rc` above `pending` so the guard passes, and `releaseFrame` then leaves the single reference the caller owns. With the opposite order every such `RETURN` deopts: still correct, because the interpreter finishes the work, but never native. A spurious deopt is invisible to value and refcount oracles, so `TestARM64_RefReturn` asserts the `guard-value` exit count is zero instead. `stitch` keeps the original order: the inline path rejects any callee holding a ref local, so an inlined return is backed by a parameter or upvalue and cannot reach that boundary.

Top-level module code has no synthetic `RETURN`. Falling off the end closes the module trace and writes live operands back to the VM stack.

## Branches

Recorded forward branches become guarded exits or learned branch continuations.

`BR_IF` and `BR_TABLE` emit the recorded path. Unrecorded targets deoptimize.

When a side exit becomes hot, the tracer records that target. A later compile may fold it into the same native callable as a pending block. The loop wrapper records every fallback exit as a branch, so loop anchors recompile through the same side-exit machinery as entries. Loop roots are never folded as ordinary continuations: a leg that rejoins this plan's header folds into the native back-edge (see Trace Snapshots), while a leg that is another loop's root deoptimizes and uses that loop's standalone entry, which preserves back-edge and safepoint semantics.

A loop callable normally exits through a trap, but a folded depth-0 `RETURN` leg emits `ret()` and a folded completed leg emits `complete()`, both returning with `journal.TrapNone`. The loop wrapper handles this like the entry wrappers: a function loop performs the threaded `RETURN` frame teardown, and a module loop marks the frame exhausted.

Pending blocks reload from VM stack slots, run through a FIFO worklist, and stop at a bounded pending cap. The trace frontend orders learned roots once; the backend does not repeatedly sort pending work.

A cold branch edge may carry caller-continuation block IDs. The side trace body lowers first; on callee `RETURN`, lowering stitches the result into the caller frame and follows those IDs. The continuation reloads from VM stack slots before continuing.

A deferred profiled edge gets a label and canonical snapshot, shared only with an identical scheduled continuation (same block, tail, and snapshot). Static state-backed blocks share labels only through explicit block IDs, never through bytecode-anchor equality.

A side exit rearms its root's rebuild every time its hit count crosses the hot-exit threshold, not only the first time: the trace tree keeps learning, and a rebuild it could not plan at the first crossing regularly plans at a later one.

Targets still deoptimize when they are unknown or unsupported.

Branch lowering may skip hot-path flushes only when the branch state is clean. If locals or operands are dirty, flush first. Learned continuations and side exits must see the same stack image as threaded dispatch.

A committing flush (`selfCall`, `tailLoop`) transfers operand ownership to the VM stack, so it accepts a live `backingStack` ref: that ref already carries the retain taken when it was pushed, and committing hands the same edge to the stack, exactly as the inlined call path does when it stores arguments and drops them from the operand stack. It rejects any live deferred ref (a const marker or a slot-backed operand): a deferred ref carries no retain of its own, and a loop back-edge has no cold stub to take one, so owning it each iteration would leak. Eligible loop-carried scalars are the exception to local materialization: their registers remain authoritative across the back-edge and cold handoffs commit them separately. A self-recursive function still forwards a ref parameter to itself because the argument is owned into the callee frame (through the call-argument path) before the commit. See Reference Ownership.

### Branch range validation

ARM64 conditional/compare/test branches (`B.cond`, `CBZ`/`CBNZ`, `TBZ`/`TBNZ`) encode a fixed-width signed PC-relative immediate — imm19 (±1MB) for `B.cond`/`CBZ`/`CBNZ`, imm14 (±32KB) for `TBZ`/`TBNZ`, imm26 (±128MB) for `B`/`BL`. `internal/asm/arm64.Encoder.Encode` validates every such offset is 4-byte aligned and fits its field, returning `asm.ErrBranchOutOfRange` instead of silently masking an out-of-range offset into a wrong target. `internal/jit`'s `publish` treats `ErrBranchOutOfRange` the same as `asm.ErrNoRegistersAvailable`: it aborts native lowering for that trace and falls back to threaded dispatch rather than emit a corrupt callable.

Before that fallback triggers, `asm.Assembler.encode` runs a branch relaxation fixpoint (`asm.Relaxer`, implemented by `internal/asm/arm64.arch.Relax`) between the draft and final encoding passes. Each pass drafts the current instruction list once, collects every `B.cond`/`CBZ`/`CBNZ` label branch whose imm19 displacement does not fit, and rewrites all of them together into an inverted-condition branch that skips a following unconditional `B` (imm26, ±128MB) to the original target; it then re-drafts and repeats until a pass finds nothing left to relax. Both replacement instructions are constructed to already be in range, so a given branch relaxes at most once and the loop always terminates, and batching every out-of-range branch within a pass keeps the number of drafts proportional to the number of passes rather than the number of branches; if the unconditional `B` itself would not reach the target (>±128MB), `Relax` returns `false` and `ErrBranchOutOfRange`/the JIT fallback still applies. `TBZ`/`TBNZ` never carry a `LabelOperand` in this codebase (their offset is always a caller-computed immediate — see `internal/asm/arm64/instr.go`), so they never reach `Relax` and the imm14 (±32KB) window has no relaxation path; architectures without a `Relaxer` (amd64) are unaffected — `encode` no-ops the pass.

## Loops

A loop root is the target of a backward branch. Backward branch handlers report hotness directly; profiler sampling is not involved.

Native loop entries run with the current frame and return to threaded execution through explicit safepoints or deoptimization. Loop-carried scalar locals may stay in registers when the plan can preserve their state safely; otherwise the loop uses VM stack slots.

Loop roots are compiled as separate native entries and installed at `i.code[addr][header]`. A loop root never tears down its frame. A static function or module entry that contains a loop also emits the native back-edge safepoint, but only while that entry is still a candidate for a better loop root. It uses the existing `loopWarmup` interval for the first handoff; the yield reaches `backedge`, which records the real loop state and lets the trace frontend install the specialized loop root. Once the loop root is installed, execution remains at the header and the ordinary `loopBudget` applies. If the trace cannot produce a usable loop root, normal cooling removes the extra back-edge instrumentation rather than polling every iteration indefinitely.

## Suspension

`YIELD` and `RESUME` are suspension points. They cannot execute as normal linear native trace operations.

Suspended state is held by the private `coroutine` value; it is not a host
extension seam.

For anchor-frame suspension:

- tracer records the opcode as a terminal
- native code emits an unconditional fallback at the opcode IP
- threaded dispatch performs the real suspend or resume exactly once

The resume IP is the opcode itself, not the next instruction, because the threaded handler advances `ip`.

Suspension inside an inlined callee aborts the trace. Deoptimization can rebuild inlined frames, but it does not restore their coroutine handle. Only the anchor frame can safely keep its coroutine state across deoptimization.

## Values

Scalars stay unboxed between native trace operations.

| Kind | Native treatment |
|---|---|
| `i32` | low 32 bits |
| `i1` / `i8` | low 32 bits with narrow result kind preserved where required |
| `i64` | full signed register value when inline-boxable |
| `f32` / `f64` | IEEE bit representation |
| heap-promoted `i64` | deoptimize on load |

Narrow kinds share the `i32` representation. Kind checks compare representation, so `i1` and `i8` can flow into `i32.*` lowering.

Result kinds must match the interpreter:

- `i32.and`, `or`, and `xor` preserve a shared narrow kind
- mixed narrow operands widen to `i32`
- other arithmetic widens to `i32`
- comparisons and `eqz` produce `i1`

## Slots and Refs

`GLOBAL_*`, `LOCAL_*`, and `UPVAL_*` lower for in-range static slots.

Scalar slots load and store raw values directly.

A ref slot store releases the overwritten ref and transfers the stored ref, both guarded through `journal.CellRC`. A ref `LOCAL_GET`/`GLOBAL_GET`/`UPVAL_GET` instead pushes a deferred operand and takes no retain (see Reference Ownership).

If a release may free the object (`rc == 1`), native code deoptimizes before the release. The interpreter owns recursive release and cleanup.

## Reference Ownership

A ref `value` carries an `backing` that records where its reference count lives.

| Backing | Retain location |
|---|---|
| `backingStack` | the operand-stack copy owns its own retain |
| `backingConst` | a compile-time constant marker; retain deferred |
| `backingLocal` / `backingGlobal` / `backingUpval` | deferred to the backing slot (`slot`) |

Only a `backingStack` ref carries a retain. Every other backing defers it to backing storage that already holds one, so the operand is a borrowed view until it transfers to interpreter-visible state.

Producers push deferred. `LOCAL_GET`, `GLOBAL_GET`, and `UPVAL_GET` of a ref take no retain and record its backing slot; const markers push `backingConst`; `DUP` of a deferred ref copies it deferred. Container consumers borrow the operand and elide their matching release when it is deferred: `ARRAY_GET`/`SET`, `STRUCT_GET`/`SET`, `ARRAY_LEN`, `REF_IS_NULL`, `DROP`, and the coroutine, error, and string reads all skip the container `guardRC`/release when the consumed operand is not `backingStack`. A ref element or payload result is still retained; only the container's own release is elided. This removes the per-element retain/release pair from primitive container loops.

A retain materializes at every point that hands a deferred value to storage the interpreter can see:

- `own` — storing into a local, global, or upval slot, transferring a ref into an array element or struct field, transferring a call argument through `locals()`, and boxing an entry-frame return in `ret`.
- `detach` — before a backing slot is overwritten (`LOCAL_SET`/`GLOBAL_SET`/`UPVAL_SET`) or a frame dies (`stitch`, tail dispatch), every live operand deferred to that slot is owned first.
- exit stubs — `emitExits` reloads each deferred operand from its flushed VM stack slot and retains it on the cold guard path.
- `retainDeferred` — a stub-less deopt that hands the flushed operand stack to the interpreter (a trap fallback, module completion) re-takes each deferred operand's retain from its VM stack slot.
- real calls — `directCall` and `selfCall` own every live deferred operand before the `BL`, because a callee trap adopts the caller's flushed stack.

The SSA frontends record the same fact per operand-stack entry rather than per value: `ssa.Frame.Stack` marks an entry owned exactly when its `backing` is `BackingStack` at the point the deopt state materializes, which is after the retains that instruction already took (see SSA Static Frontend).

`ssa.Frame.Locals` carries no ownership mark at all, and that is the rule rather than an omission: only a reference is ever owned, and `internal/ssa/transform.PromotePass` never promotes a ref slot. Taking a ref out of its slot takes the slot's reference count with it - the store that would have released what it replaced is gone, and so is the retain that handed the slot the new value - and nothing in this IR states where that count then lives. `ssa.Verify` rejects a promoted local of type `ref`, so the gap cannot be opened by accident. Promoting one is the follow-up work that needs retain/release pairing first, exactly as `carry`'s own scalar-only rule does.

A committing (loop back-edge) flush rejects any live deferred ref: owning it would retain once per iteration with no matching release, so a loop-carried deferred ref keeps the whole trace threaded instead. Standalone loop traces also remain threaded when their entry already has live operands because trace plans do not reconstruct loop-entry operand state.

## Heap Reads and Mutations

ARM64 supports selected heap fast paths.

Native full-trace reads include observed shapes for scalar `REF_GET`, selected `ARRAY_LEN`, selected `ARRAY_GET`, selected `STRUCT_GET`, `ERROR_GET`, `CORO_DONE`, and `CORO_VALUE`. `ARRAY_SET` and `STRUCT_SET` use the guarded fresh-register heap path for both primitive and ref stores; the former compile-time-constant-container restriction is removed.

Heap reads guard ref address, heap itab, array element kind, struct type pointer, struct field kind, index bounds, and release safety when needed.

`STRUCT_GET` and `STRUCT_SET` also lower against a `*HostStruct`, whose fields hold Go memory rather than VM words (see `docs/host-integration.md`). `hostGet` and `hostSet` (`internal/jit/arm64/heap.go`) guard the heap itab against `ctx.layout.HostStructItab` — `HostStruct` is unexported, so `Interpreter.compileSnapshot` computes its itab with `jit.Itab` and hands it across on `jit.Layout` alongside the field offsets, the same treatment struct offsets already get — bounds-guard the field index against the compiled layout the view carries, and guard that field's Go kind against the one the trace recorded in `Shape.Field`, then load or store the Go field through the address the layout's offset names. Nothing about a host view is assumed from the itab alone: the same compiled access serves every `*HostStruct`, so a container whose field at that index has another Go kind exits at the kind guard rather than loading the wrong width.

`hostShapes` (`internal/jit/layout.go`, resolved through the exported `jit.HostShapeByKind`) is the one place a hosted Go field's layout is written down, indexed by the `reflect.Kind` the codec compiled the field through, and mirrors the codec's own `leaves` table. A kind with no row - `string`, a pointer, a nested container - publishes a heap reference rather than loading a word, so its access stays with the interpreter. A read reinterprets a field as wide as its VM slot and widens a narrower one by the field's own signedness, which is why an `int16`, an `int32`, and a `uint32` field all reach the guest as i32 but do not share a load. A write lowers only for a field as wide as its slot: a narrower one decodes through the range check `setSigned` and `setUnsigned` perform, and a check that can fail belongs with the interpreter that reports it.

Ref reads retain the loaded element or payload. A container consumer releases its container handle only when that operand owns its retain, eliding the release for a deferred operand (see Reference Ownership): `CORO_VALUE` still retains the value and releases the handle when the handle is `backingStack`. `CORO_DONE` keeps the handle.

Heap-promoted `i64` values fall back before boxing.

Primitive typed-array `ARRAY_SET` and scalar-field `STRUCT_SET` may continue through native execution when their guarded heap path fits the register budget. Guard failure resumes at the original opcode; success performs the store and continues to later operations or the loop back-edge.

Ref-element `ARRAY_SET` and ref-field `STRUCT_SET` continue natively like their scalar counterparts. Before the store, lowering owns a deferred element or field value so the transferred container edge carries exactly one retain, matching threaded execution. A replaced `BoxedNull` field/element has no heap ownership and is not released. `REF_IS_NULL` tests the ref payload rather than the whole boxed word, because a container slot's null is not always the `BoxedNull` bit pattern (see `docs/memory-model.md`, Heap index 0 is always null).

Both were terminal until the callee-frame defect below was found. Letting either continue drove refcounts negative against a threaded twin from self-recursion depth two upward, and two attempts to lift the rule were reverted on that evidence. The cause was never in the stores: a native callee began with non-parameter locals that no one had cleared, so its first `LOCAL_SET` released a stale boxed word it never owned. Lifting the store rule is merely what first admitted a function holding a ref local into native lowering, which is why the two appeared connected. `TestARM64_RefContainerStore` covers the shape that used to diverge.

Stores carry no spill restriction of their own. They use the common
fresh-register heap path, and the allocator judges any spill they need the same
way it judges every other: the store must dominate every pending reload, and
must clear the loop-carry and self-recursive-call hazards (see Register
Allocation). A mutation plan used to force the whole build off the spill frame,
because the allocator could not yet tell a sound spill from an unsound one
around a store's own branches; it can now. When no value is eligible and the
bank is exhausted, `asm.Build` still rejects native compilation with
`CompileReasonRegisterPressure` and threaded execution remains installed.

Allocation and complex ref-bearing mutations either bridge (see Bridge) in a static plan or stay threaded/terminate the native trace in a trace plan.

## Bridge

A bridge deopts one opcode the backend cannot lower to the threaded interpreter and resumes native execution afterward, instead of ending the native entry outright. It generalizes the mechanism first built for `ARRAY_NEW_DEFAULT` alone.

`jit.Bridgeable` (`internal/jit`) is the single predicate naming every opcode eligible: the allocation family (`ARRAY_NEW`, `ARRAY_NEW_DEFAULT`, `ARRAY_SLICE`, `ARRAY_DELETE`, `STRUCT_NEW`, `STRUCT_NEW_DEFAULT`, `MAP_NEW`, `MAP_NEW_DEFAULT`, `MAP_DELETE`, `MAP_CLEAR`, `REF_NEW`, `REF_SET`, `CLOSURE_NEW`, `STRING_NEW_UTF32`), the map/string/bulk-array opcodes `internal/jit/arm64` otherwise lowers as an unconditional trap (`MAP_LEN`, `MAP_GET`, `MAP_LOOKUP`, `MAP_KEYS`, `MAP_ITER`, `STRING_ENCODE_UTF32`, `STRING_ITER`, `ARRAY_FILL`, `ARRAY_COPY`, `ARRAY_APPEND`, `MAP_SET`), structured errors (`ERROR_NEW`, `ERROR_CODE`, `THROW`), and `REF_TEST`/`REF_CAST`. An opcode already lowered natively (`ARRAY_GET`, `STRUCT_SET`, and so on) must never appear here: a bridge is strictly the fallback for opcodes with no native lowering. `YIELD`/`RESUME` are excluded even though the backend cannot lower them either — suspension cannot resume mid-frame into native code (see Suspension) — so they keep the unconditional terminal-fallback treatment in `internal/jit/arm64`'s `steps` instead.

The static planner (`StaticPlan`) is the frontend that acts on `jit.Bridgeable`: walking a function's bytecode, an opcode it names ends the current plan block with a `terminateBridge` terminator instead of becoming an ordinary step, and the remaining source instructions continue into a fresh block anchored right after it, marked `block.bridge`, carrying the post-op dataflow state so lowering reloads it exactly like any other state-backed block. `applyStep` must still be able to model the opcode's stack effect for the plan to proceed: fixed-arity opcodes use `instr.TypeOf`'s `Pop`/`Push` directly; the dynamic-arity ones (`STRUCT_NEW`, `MAP_NEW`, `CLOSURE_NEW`, `ARRAY_NEW`, `ARRAY_APPEND`) derive their count from the instruction's own operand, a known compile-time constant on the stack (`slot.valKnown`), or a statically resolved callee, matching how `program/verify.go`'s `flow()` computes the same opcodes' effects for verification; when none of these resolve the effect, the plan is rejected exactly as before. A pushed slot produced by a bridged opcode's own effect (a fresh allocation, a resolved element/field value) must be a new `backingStack` slot, never a mutated copy of an operand that existed before the bridge: after the bridge, `retainDeferred` has already taken a real retain for every deferred operand handed to the threaded closure, so continuing to mark a survivor as deferred (`backingLocal`/`backingGlobal`/`backingUpval`/`backingConst`) makes a later consumer elide a release that must run, leaking the retain (see Reference Ownership). `REF_CAST` (identity pass-through: pop, then push the same kind, narrowing `styp` when the declared target is a struct type) and `ARRAY_APPEND` (its array operand is never popped, so it survives on the stack) both learned this the hard way and construct a fresh slot instead of reusing the pre-bridge one.

`internal/jit/arm64`'s `dispatch`, emitted once per callable, reads the journal's entry-IP cell at the top of the callable and, when it names a `block.bridge` anchor, branches directly to that block's label instead of falling into the normal anchor start; zero (every ordinary `Call`'s value) falls through unchanged. `bridge` (`l.term`'s `terminateBridge` case) traps with `journal.TrapBridge` and the opcode's own IP, sharing `journal.TrapFallback`'s flush and `retainDeferred` handoff but carrying no exit descriptor — a bridge is productive continuation, not a give-up (see Retirement), and `tier.Watchdog.Bridge` counts it on a separate counter so it can never inflate the give-up rate. `Interpreter.bridge` (`interp/jit.go`) is the Go-side half: it runs `i.code[f.addr][ip](i)` — the bridged opcode's own threaded closure — exactly once, then reports the IP native execution may resume at, or `ok=false` when it must not (the closure moved frame/function, made no forward progress, spent the wrapper's `loopBudget` of bridge cycles, or the new IP is not one the callable's `resumable` list carries an entry-dispatch label for). If the bridged opcode's own IP is 0 — the function's very first instruction — `i.code[f.addr][0]` is the native wrapper this call is already running inside (`install` overwrites only the anchor slot), so `Interpreter.bridge` runs the shadowed threaded handler (`i.stub`) instead of that wrapper, exactly as a `journal.TrapFallback` resuming at 0 already did (see the Loops section's header note).

A bridge cycle re-enters through a fresh external `Call`, which never runs the loop-carry prologue (see `dispatch` above): a carried register would be uninitialized garbage on such a resume. A bridge a plan can reach therefore keeps every local slot-backed instead of loop-carried. The scope is the plan, not the function: each loop plan carries only the blocks its own root reaches (see Static Frontend), so a bridge this header cannot reach is not compiled into this callable, has no resume label here, and does not disable carrying. A bridge that the header does reach but that sits outside the loop's own back-edge range still disables it; narrowing that residual case to "a bridge inside the loop body" is unimplemented follow-up work, tracked because it would need the carry-load prologue to run on every re-entry path, not just the callable's own head.

`arrayKind` (`internal/jit`, unexported) resolves an `ARRAY_GET`/`ARRAY_DELETE` element kind from the concrete itab `Input.Objects` recorded for a known constant container, matching `arrayGetKnown`'s native lowering, and otherwise from the declared array type — mirroring `structFieldKind`'s declared-struct-type resolution, which `STRUCT_GET` already relies on. The declared type answers only in a call-free plan (`callFree`). The gate was added because lifting it corrupted native execution state (`runtime.mallocgc` SIGSEGV) when `ARRAY_GET`'s general lowering path ran alongside a native `CALL`; that is fixed — the `BLR` no longer clobbers the spill base (see Calls and Returns) — and the full suite passes with the gate lifted. It stands for a measured reason instead: lifting it makes a whole-function static plan available for kernels like `Numeric_SpectralNorm`, and while root arbitration (see Arbitration between roots) now keeps that from costing 45%, the wider planning still gives back the gains it buys and improves no kernel. A function containing a call therefore keeps resolving `ARRAY_GET`, `ARRAY_LEN`, and `ARRAY_DELETE` only from a known constant container.

## Structured Errors

`ERROR_NEW`, `ERROR_CODE`, and `THROW` bridge in a static plan (see Bridge) and remain terminal fallback boundaries in a trace plan.

The tracer records them without stepping the clone. In a trace plan, native code deoptimizes at the opcode IP with no resume, and the threaded handler performs error allocation, code extraction, throw unwinding, and handler landing.

If any of these appears in an inlined callee frame, the trace aborts.

## Installation

Compiled modules install into the threaded dispatch table, always on the
goroutine that owns it: `Interpreter.sync`, reached from a safepoint or from
`serve` itself when the queue built inline.

Entry wrappers and loop wrappers differ:

| Wrapper | Use | Frame behavior |
|---|---|---|
| `entry` | module/function entry | may complete or tear down frame |
| `loop` | loop header | re-enters live frame |

Install only accepted callables. Rejected roots leave the existing threaded closure intact.

### Arbitration between roots

Two roots of one function can both compile, and an installed root keeps
execution inside itself, so whichever one holds the dispatch slot starves the
other. `install` refuses a static plan that would swallow a root already
dispatching, in either compile order: it declines the install when the loser is
the newcomer, and withdraws the installed one when the loser is already in
place. `i.live` records which anchor owns each slot — `i.exits` cannot answer
that, because `retire` restores the slot but keeps its shadow entry.

Only a static plan loses. The trace frontend anchors a nested loop as an edge
to that root's own entry instead of inlining it, so a recording never takes an
inner root's work away; the static loop plan is the fallback for a loop no
trace could record (see Compiler above), so it must not displace one. The same
rule covers the anchor itself: a static plan never replaces the recording live
at the very anchor it installs into, which is what keeps a side-exit rebuild
that fell through to the static frontend from swapping a running recording -
folded legs, hoisted container - for the fallback that has neither.

What counts as swallowing depends on the root:

| Root | Swallows |
|---|---|
| function entry | every loop in the function |
| loop header | only loops nested in its own body (`tracer.encloses`) |
| module entry | nothing — see below |

Containment comes from the loop's own span: a backward branch's target opens a
loop and the last branch back to it closes it, so a nested header lies inside
that range while a sibling only follows it. Sibling loops must keep installing
independently, which is what a rule based on mere coexistence gets wrong.

Module code keeps the whole-module entry installed until a hot loop is actually
recorded. The static module plan may therefore own the loop first, but its native
back-edge yields through the existing `loopWarmup` cadence so `backedge` can record
the live loop state. A usable trace loop root then installs at the header without
requiring the whole module to be withdrawn. Module code is also the one anchor
exempt from the same-anchor rule above, for the same measured reason.

Native wrappers must always leave the interpreter in a valid state for threaded redispatch.

### Cooling and retirement

Cooling is the compile-side half: a function whose entry root and every loop
header have been attempted without installing anything stops being sampled,
captured, and back-edge instrumented. `docs/profile.md` owns that rule.
Retirement below is the runtime half, for native code that is already
installed.

### Retirement

A trace can compile into a native entry that runs a few instructions and then always gives up instead of completing its job. A high exit rate alone is not a failure signal — a healthy kernel like Sieve or NQueens exits on nearly every entry, through `loop-exit`. A high *give-up* exit rate is, because the interpreter pays full bailout and re-entry cost for work the native code never finished. `internal/jit/tier`'s private `givesUp` names the three ways that happens: `prof.ExitTraceCut` is native code that knowingly stops mid-function; `prof.ExitColdBranch` is a cold branch taken anyway, so the recording predicted the wrong path; and the four `prof.ExitGuard*` reasons are speculation the runtime refuted. `prof.ExitLoop` is how a loop normally ends and `prof.ExitTerminalOp` is a deopt the plan intended, so neither counts. "Unproductive" is cooling's word for a different thing (see `docs/profile.md`), so retirement says give-up throughout.

The verdict itself lives in `internal/jit/tier`, not `interp`: `tier.Watchdog` is a pure decision object that observes counts and durations the caller reports and answers retire-or-keep, holding no interpreter state and importing nothing from `interp`. `interp/tier.go` owns the mechanism around it — hot-event sampling, tracing, compile/install/cool/retire — and drives a `tier.Watchdog` per installed anchor (`interp/jit.go`'s `install` calls `tier.New`; `Interpreter.checkRetire` in `interp/tier.go` reads its verdict; `interp/deopt.go`'s `cycle` and `bridge` feed it every native dispatch).

Each installed anchor's `tier.Watchdog` combines the existing give-up/bridge counters with an additive throughput probe for `EntryFunction` anchors, both behind one query, `Watchdog.Retire`: it reports the throughput probe's verdict directly, or - once a full window of entries has passed - whether the give-up or bridge rate crossed the threshold, resetting the window's counters either way. The give-up and bridge counters remain exactly as before: `call`, `start`, and `loop` count one entry per invocation (`Watchdog.Enter`); a fallback exit increments the give-up counter only when `givesUp` accepts its reason (`Watchdog.Exit`); a bridge cycle counts separately so it can never inflate the give-up rate (`Watchdog.Bridge`); every 1024 entries, the existing threshold marks a net-loss anchor.

The throughput probe is deliberately limited to function entries because one reach means one function call in both threaded and native modes. Loop headers do not have that property: threaded execution reaches a header once per iteration while a native loop remains inside the callable, so this signal does not cover loop or module anchors. Those anchors keep give-up/bridge-only retirement (`tier.New` starts them already decided).

For a function entry, probing starts with a 32-reach warm window that is not judged. It then alternates native and shadow windows (`Watchdog.Enter`, `Watchdog.Reach`), starting at 32 reaches and growing to at most 256 when the confidence interval remains wider than 5%. The first native/shadow pair is also warmup and is excluded from the statistics, so first-use timing does not dominate the verdict. A `Run` boundary discards only the in-progress timed window (`Watchdog.Reset`, driven by `interp`'s `probeBoundary`); completed pairs and their accumulated statistics are preserved, so short repeated runs can still reach a verdict without including host-side work. Only the first and last reach of each timed window take `time.Now()`, so no per-reach clock call is added. `Watchdog.Reach` reports both whether the probe is currently timing a shadow window and, once the caller's reach completes the round, whether that round decided; whenever it reports the probe is timing a shadow window, the wrapper invokes its saved `resumeShadowed` handler instead of native code. The function's `i.natives` fast-path slot is atomically cleared for the whole shadow window (`Watchdog.TakePending`) and restored after the final shadow reach; native-to-native calls therefore cannot bypass the probe. Each measured native/shadow pair contributes a relative throughput delta to an online mean and variance. After three measured pairs, the probe decides to retire (`Watchdog.Retire`) when a 95% normal-approximation confidence bound is entirely above a 1% improvement, keeps native execution when it is entirely below a 1% regression, grows the window when uncertainty is still high, and gives up conservatively after seven total pairs including the warm pair. A decided anchor never probes again.

A throughput retirement emits `vm_jit_retirements_total` with the same `func`, `ip`, `kind`, and `frontend` labels as `vm_jit_native_entries_total`. The metric is recorded only when a live anchor is actually removed. Retirement still mutates only the local interpreter's dispatch table (`i.code`, `i.natives`, `i.cold`), never a pool's shared published module.

## Tests

Run focused tests after JIT changes:

```bash
go test ./internal/asm/... ./interp/...
```

Use this guide:

| Change | Test focus |
|---|---|
| ABI or callable behavior | `internal/asm/assembler_test.go` |
| trace recording | `interp/interp_test.go` |
| native lowering | `interp/interp_test.go` |
| install or wiring behavior | `interp/interp_test.go` |

## Maintenance Notes

When changing JIT internals:

- keep the threaded interpreter correct first
- keep native lowering speculative and guarded
- deoptimize before behavior the JIT cannot fully own
- prefer one simple terminal fallback over duplicated semantics
- keep architecture-neutral code in `internal/jit/`
- keep ARM64 lowering in `internal/jit/arm64/`
- keep the frame-journal cell, record, and trap layout in `internal/journal`, explicit and stable
- preserve interpreter/JIT stack and ref ownership symmetry
- keep the shared compile queue, store, tracer, and coroutine state private behind `Pool` and `Interpreter`
- use short, standard names such as `trace`, `root`, `entry`, `loop`, `module`, `lowering`, `guard`, `exit`, `frame`, and `value`
- avoid adding an abstraction unless it removes real duplication or isolates real complexity

## Related Docs

- `docs/profile.md` — sampling, hotness thresholds, and JIT counters
- `docs/benchmarks.md` — benchmark results and methodology
- `docs/value-representation.md` — boxed values and kind semantics
- `docs/memory-model.md` — refs, ownership, and heap lifecycle
- `docs/instruction-set.md` — opcode semantics
- `docs/debugging.md` — bytecode-level mode that disables optimized execution
