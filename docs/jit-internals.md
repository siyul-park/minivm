# JIT Internals

Native tier: ownership, runtime contract, lifecycle.

`architecture.md` owns package boundaries; `jit-lessons.md` owns prior design evidence.

## Status

- Opt-in via `interp.WithThreshold(n)` (`n >= 0`). ARM64 only. Disabled with `WithHook`/`WithFuel`.
- Threaded execution is the semantic baseline.

```text
bytecode → transform.Translate → SSA passes (per tier) → compile.Lower → asm.Assembler.Build → jit.Code
```

| Entry | Counts | Threshold | Cadence | Root |
|---|---|---|---|---|
| `CALL` | interpreted calls to a `*types.Function`, served `ExitCall`s included, and `ExitCall`s of a closure call | `n` (Baseline); `graduate` (1024) native entries (Optimized) | every call | ip 0 |
| OSR | back edges at a loop header, module code included | `n` | `interval` (256) back edges | the header |
| Module entry, loop-free | Runs of module code | `max(n, 2)` | every Run | ip 0 |
| Module entry, with loops | Runs of module code | `max(n, 2)` | `interval` (256) Runs | ip 0 |

`compile.Unit.OSR` / `jit.Code.OSR` mark OSR units, module entry included; a header can sit at ip 0.

## Owners

`architecture.md`'s Package Ownership table owns package boundaries. Within `interp/`, threaded execution, tiering, and exit handling specifically live in `native.go` and `osr.go`.

## Runtime contract

| Symbol | Contract |
|---|---|
| `asm.State` | Native stack, saved Go registers, native SP/PC/register file at the last exit. No Go pointer on the native stack. |
| `asm.Enter` / `asm.Resume` | Run code on the native stack / continue a suspended activation. Report whether it stopped at an exit. |
| exit stub | Native code `BLR`s `asm.OffsetStub`; the stub saves registers and returns to Go. `Resume` returns from that call. |
| `jit.Context` | `asm.State` first, then `Trap`, exit id, then `Heap`, `Globals`, `RC`, `Natives`, `Entries`, `Top`, `FB`, `Upvals`, `Depth`, `Limit`, `Budget`, `Results`, `Records`. `Results` stages a bridge or box exit's result words for native code to reload on resume. The interpreter writes every base before `Enter`; only `Heap` and `RC` are rewritten before each `Resume`, since serving an exit can only relocate those two append-grown slices. |
| `Context.Upvals` | The entering activation's upvals base. Written by the interpreter before an OSR `Enter` and by a native closure call before its branch; read once by a prologue into an ordinary register. |
| `jit.Trap` | `TrapReturn`, `TrapDeopt`, `TrapBridge`. |
| `jit.Code` | One unit's native code at one tier. `Free` unmaps once. |
| `jit.Store` | Published code and `Context.Natives`. |

- Native code never runs on a goroutine stack; async preemption cannot reach it, so back edges, calls, and returns spend `Budget` explicitly. Back edges and calls branch to safepoint on exhaustion; returns only credit the work.
- Native code writes no Go pointer. It reads heap interface words through `Context.Heap` and object fields at `jit.Offset*`.
- Registers: X24 budget, X25 frame base, X27 activation depth, X26 context, X16/X17 scratch, X18/X28 untouched. Allocatable: X0–X15, X19–X23, D0–D31.
- X24 mirrors `Context.Budget`: exits store it, resumed exits reload it, and a normal Go entry stores it back after native return.
- Allocation: linear scan, no splitting; a value live across a call or under pressure spills for its whole life. Values whose live rows never meet share a register: a value does not hold its register through a block it is dead in, such as a later loop laid out before its out-of-line stubs. Calls clobber every allocatable register. Exit maps keep ordinary mapped values live through their stubs. Promoted locals are deopt-only state: calls and safepoints keep them live across resumption; other exits save a non-live one on their cold path into a fixed spill home.
- In a function with a call, a scalar or ref constant no loop block uses is loaded at each use and at each exit stub instead of spilled; an `ExitCall` map keeps its constants live across the call, since a deeper trap reads them from spill slots. Other constants keep one register.

## Pipeline

| Stage | Contract |
|---|---|
| Translate | bytecode → SSA; attach interpreter state to each `OpExec`, return, and completion; block 0 has no predecessors |
| Baseline | fold → DCE |
| Optimized | fold → forward → CSE → guard → DCE → promote → hoist → DCE |
| Lower | assign registers by SSA type, including register-passed parameters the machine prologue fills, order blocks in reverse postorder with an edge to the next block falling through, resolve block parameters with edge moves, fuse a compare into the branch right after it when nothing else uses it |
| Build | assemble, allocate, encode, publish through `jit.Code` |

A loop header `MUST` have state before its budget check. An OSR unit loads block-0 parameters from the current operand stack and starts no locals.

A function or module with `Handlers` translates. A catch block is reached only by handler dispatch, never by a CFG edge, so Translate never reaches it and native code holds none; an entry at a header inside a catch block declines. A fault raised inside a protected range (a trap, a bridged op's panic, `THROW`, a callee's fault crossing a native caller) deopts at the op's state, and threaded code finds the handler (`Interpreter.handler`) over the materialized frames: the top frame at its `ip`, each suspended caller at `ip-1`. These deopts are trap class. `SSAPass` declines a function with `Handlers`: re-emitting would invalidate the table's byte offsets.

Translate never retains a constant callee: the pool keeps it alive, so its call state does not own it and the native call neither retains nor releases it. Lower takes `Call.Owned` from that state. A dynamic `CALL` resolves from its site's feedback:

| Feedback | Translation |
|---|---|
| none: the site never ran | the block ends in `OpExit` at the `CALL`, with its operands owned; no span only reachable past it is translated |
| one function or closure | a guarded constant call, only for a callee operand its state does not own |
| several callees of one function type | a generic call: every argument and the callee are owned, and `ssa.Shape.Type` carries the type |
| several callees of differing types | the translator declines the whole unit |

A `CALL` whose callee is a closure carries the closure's `ssa.Shape` (function, type, captures), and Lower calls that function, passing the closure's upvals:

| Callee | Resolution |
|---|---|
| a `CLOSURE_NEW` over a constant function in the same unit, on the stack or in a local no path re-stores before the call | static: no guard; an OSR unit knows no closure built before its header |
| a closure recorded at the site's feedback | `guard.shape` on the operand: a `*types.Closure` over the function, of its type, holding at least its captures |

A local- or constant-backed callee is lent: the call neither retains nor releases it. Feedback records a callee's function type with it: a site keeps the type its several callees share until one of another type is seen. Closure feedback is recorded only where Go already runs: an `ExitCall` of a closure call records its callee and counts its entry, since the threaded closure `CALL` has no native hook, so a site of a closure no native code has called looks never run.

A function with captures runs natively only through its closure:

| Entry | Rule |
|---|---|
| Go (`CALL`) | declines: a direct call carries no upvals |
| OSR | declines unless the frame holds its captures |
| Native call | lowering rejects a direct call of it |

A reference parameter its function never writes (`transform.Borrows`) is borrowed: a native caller lends a local- or constant-backed argument without a retain, and releases an owned one after the call.

## ARM64 activation

| Area | Contract |
|---|---|
| Prologue | Push `Records[Depth]`, save `Record.PC`, count `Entries[address]` when enabled, start non-parameter locals at their zeros (`value-representation.md`), loading each distinct zero once; OSR starts none. A function that reads or writes its upvals loads their base from `Context.Upvals`. |
| Store | Reference-capable `OpStore` releases the old slot value before overwrite, matching threaded `LOCAL_SET` and `UPVAL_SET`. |
| Return | `OpReturn` releases reference slots; borrowed parameters only at depth 1 (the Go-entered activation), returns up to two register results in X0/X1, or stores boxed results; `OpComplete` writes past locals. |
| Call | Constant calls box arguments into the callee frame, then use `Context.Natives[addr]`; self-calls use the unit entry. A closure call to a function with captures writes the closure's upvals base to `Context.Upvals` after its budget check. Missing code, depth, or frame space takes `ExitCall`, which resumes past the callee release: slot results are read there, register results are loaded from `Context.Results` by the exit stub. A generic call (`Call.Generic`) stores its arguments, always takes `ExitCall`, has no budget check, and reads its slot results at the join; an i64 result is unsupported. |

### Call convention

| State | Contract |
|---|---|
| X25 | frame base; caller-maintained and adjusted by `8·Base` around calls |
| X27 | activation depth; prologue/epilogue step it; exits store exact depth to `Context.Depth` |
| X0/X1 results | one or two results, including i64; native-to-native i64 stays raw |
| X0/X1 arguments | one or two parameters, including i64; each still has a boxed slot |
| Go entry | loads X25/X27, reads argument slots, calls body, stores X24 to `Context.Budget`, then boxes register results |

`Code.Native()` is the body at offset 0; `Code.Entry()` is the Go stub after the epilogue. i64 entry arguments are unboxed with `SBFX #0,#49`; a threaded caller whose slot holds a heap i64 ref declines native entry. OSR reads slots.

## Exits

| Kind | Taken at | Resumes native |
|---|---|---|
| `ExitDeopt` | failed check, `OpExit`; `Exit.Trap` marks a check the operation itself raises (`Site.Trap`: zero divisor, index bounds) | no |
| `ExitBridge` | unlowered `OpExec` | yes, unless denied or its handler traps |
| `ExitSafepoint` | loop header or native call when `Budget` is spent | yes |
| `ExitRelease` | dropping a last reference | yes |
| `ExitCall` | `CALL` that cannot run natively, or a generic one | yes, unless the callee is a coroutine function, does not fit the stack, has another signature than a generic call's, or leaves by a trap, throw, or cancellation |
| `ExitBox` | a wide (> 49-bit) i64 at a store, slot return, call argument, or `OpComplete` | yes |

A deopt stub sits out of line after the next terminator that does not fall through, so the values its map names stay live only that far. Safepoint, release, box, and call stubs follow the body; a bridge and `OpExit` exit inline.

`jit.Kind.Resumes` owns the "Resumes native" column above; `compile.stub`/`function` and `arm64.Machine.Exit` all read it instead of comparing kinds themselves. A non-resuming exit's stub ends in `BRK` and skips the `X24` (`Budget`) reload, since native code never runs past it; it materializes the run's native activations outermost-first, then continues threaded.

| Exit | Resume rule |
|---|---|
| Bridge | `native.bridge` runs the op's threaded handler once in Go, then native resumes, for every op `bridgeable` admits. It denies a control transfer (the op writes `Branch`), `ARRAY_NEW` (its declared two-operand arity does not cover the `1+count` it pops), and `MAP_KEYS` (it allocates every key before the result array); their bridges deopt. An op with a host-view operand deopts before its handler runs: a view runs `Registry` conversions, which are host code. |
| Release | `Exit.Word` identifies the last ref; interpreter owns reclamation, then native resumes. |
| Box | `Exit.Word` carries the wide i64; interpreter allocates a boxed value, then native resumes. |
| Trap | A bridge whose handler panics restores each heap operand to the count it saved before the attempt, then deopts; threaded code runs the op again and reports its trap. The declined attempt leaves only heap and count storage growth, stack slots above the frame, and the scratch frame changed: no admitted handler writes heap contents, allocates, or retains anything but its operands before its last panic point. A box trap deopts the same way. |

A bridge receives its lowered `SSA Args` through `Exit.Pops`, which for every admitted op are exactly the operands its handler pops; it uses a scratch stack and leaves native registers untouched. The bridge retains every ref operand for the handler, so none reaches zero during an attempt; a boxed wide i64 is a fresh heap value the handler owns. After a completed handler, the bridge releases its retain of each operand the op adopts (`transform.Adopts`: the stored value of a heap write with no result), since native code handed the op that operand's own reference; native code releases the other operands it owns. Result words are unboxed per kind; a heap-boxed i64 result is consumed into a raw word. The materializer retains borrowed refs.

### Exit policy

`interp.native.settle` gives every exit outcome one `jit.Class`; the class decides what the exit costs the code.

| Outcome | Class | Cost |
|---|---|---|
| served bridge or box | bridge | `jit.Ledger` price |
| release | release | `jit.Ledger` price |
| served `ExitCall` | call | `jit.Ledger` price; nothing while the callee has no code but may still compile (`pending`) |
| `ExitDeopt` with `Exit.Trap`; a bridge or box whose handler panicked; a denied control-transfer bridge (`THROW`, `UNREACHABLE`); a cancelled safepoint; a trap, throw, or cancellation in a served call's callee | trap | none |
| `ExitDeopt` without `Exit.Trap` (guards, `OpExit` at a cold dynamic `CALL` or `RETURN_CALL`); any other declined bridge; an `ExitCall` not served | guard | refutation |

| Rule | Contract |
|---|---|
| Ledger | Per Go-entered address (`native.ledgers`) and per OSR site, in work units (`Budget`'s back edges, calls, returns), credited on every exit and return. `Charge` adds the class price (bridge 2.4, release 0.75, call 3.5 units; guard and trap 0) and reports whether debt ≤ 64 units; credit is floored at −64 units. A bridge, box, or call is charged before it is served; a refusal deopts (replaying a call) and retires the run's code. A release cannot deopt: its charge takes effect at the next bridge or call. |
| Prices | Time one served exit adds over threaded code (bridge ~53 ns, release ~17 ns, call ~76 ns on the reference arm64 machine) divided by native time saved per work unit (~22 ns in exiting kernels). |
| Guard | Deopts. An `ExitDeopt` guard records its site (`native.refuted`, snapshot as `transform.Module.Refuted`). It is judged against the code that took it: the run's own code, or a native callee's still-published code, retired in place. That code retires at once when its feedback moved since it was built, else on the `refute` (8)th refutation. |
| Generic site | The translator emits no container `guard.shape` at a refuted offset: the op bridges. Call sites become generic through `Callees` (replay records the new callee). |
| Oscillation | Feedback per address is monotone and finite (call sites unset → callee → mixed; refuted sites only added); a retire either consumes a feedback step or fails the tier. An OSR site whose feedback moved re-arms (resubmits) instead of disabling, and each re-arm counts toward `refute`. |

`Exit.Lent` slots are retained when a callee is materialized or the interpreter runs an ExitCall; `Exit.Kept` slots (owned arguments to borrowed parameters, which native code releases after the call) are retained for a served call and released again if its caller never resumes. `Exit.Target` locates the callee value of a closure call or a generic call: the interpreter pushes it, and the materialized callee frame runs through it with its upvals. `Exit.Args` is the argument count of every `ExitCall`; `Exit.Returns` are the kinds a generic call reads back from slots.

### Served calls

`native.nest` serves an `ExitCall` while its native caller stays suspended:

| Concern | Rule |
|---|---|
| Frames | The caller's activations keep frame slots from the run's start; a stand-in frame for the innermost runs only its exact-code `CALL` (code cut past it), so `Interpreter.dispatch` stops when the callee returns to it. |
| Handlers | `Interpreter.floor` hides the suspended slots from every handler search; a panic not caught above it re-panics from `dispatch` to `nest`. |
| Native entries | Start at `Context.Depth` = the suspended depth (`native.depth`), on the same native stack below the suspended frames; `Records` bounds the total depth, and no entry starts once they are full. `Limit` counts the suspended activations. A Go entry above depth 0 releases its borrowed parameters itself after it returns, since `OpReturn` releases them only at depth 1. |
| State | `asm.State`, `Limit`, `Budget`, and `Depth` are saved before and restored after; the nested run spends its own budget. |
| Callee | A generic call has `Exit.Callee` 0: the interpreter resolves its `Target` value to a function or closure whose parameter count is `Exit.Args` and whose return kinds are `Exit.Returns`; any other callee deopts and replays the call. |
| Results | Register results are unboxed into `Context.Results`; slot results stay at the callee frame base. |
| Leaving | A cancellation materializes the caller under the callee's live frames and continues threaded; a `THROW` whose search stopped at the floor is pushed back and runs again over them; any other panic materializes them and re-panics after the entering call leaves the store. None refutes the caller. A coroutine callee, whose `CALL` returns a handle, deopts and replays the call. |

## Store and tiers

| Concern | Contract |
|---|---|
| Publish | Non-OSR code replaces only a lower tier at an address; OSR code installs once at `(address, ip)`. |
| Retire | `Retire`/`RetireAt` unpublish; retired code remains discoverable until safe to reclaim. |
| Reclaim | `Reclaim` frees code only after no interpreter remains native. |
| Promotion | Baseline entries count calls; a live Baseline reaching the interpreter's graduate threshold queues Optimized. Optimized/OSR entries do not count. |
| Failure | Code retires by its exit policy (Exits). A compile failure, or a retire, is permanent only when feedback (`Callees`, `Refuted`) is unchanged from the snapshot the code was compiled from. |
| Async | `compile.Queue` compiles one unit per address; publication is drained at the next call, OSR observation or entry, or safepoint. |
| Pool | `Pool` shares `Store`, `Queue`, module data, the Baseline promotion candidate list, each address's CALL count, and each OSR/entry site's count until it submits, so a pooled workload compiles after about `threshold` entries in total. Each interpreter keeps its own `jit.Context`, feedback, graduate entries, refutations, ledgers, failure marks, and a submitted site's cadence. A pooled interpreter whose entries reach the graduate threshold requests Optimized even if a different interpreter drained its Baseline job. A pooled interpreter whose deopts refute shared code retires it for the pool and blocks only its own tier. |

## OSR

Every loop header, including module code, has an observer; module code also has one at ip 0, whether or not it has loops. Each site fixes its threshold and cadence at construction (Entries table). Past the threshold it retries submission and looks up `Store.CodeAt` at its cadence; a resolved site also drains at that cadence on entry, so a callee reached only from its native code still tiers up. Compile failure, or a retire whose feedback has not moved, restores the threaded handler and disables the site; a retire whose feedback moved re-arms it (Exit policy).

Loop-free code reaches no safepoint, so its ip-0 site drains on every entry and declines an already-cancelled Run, leaving threaded code to report it. Its threshold floor of 2 keeps a module run once from compiling ahead of its callees.

Module code with loops reaches a safepoint at each header, so its ip-0 site does not decline a cancelled Run. Its submit waits until every header site of the address has published, failed, or been disabled (`resolved`), so it never takes the address's one queue slot ahead of them; it polls at the headers' cadence. A header at ip 0 is observed once, as a header.

`compile.Lower` rejects an OSR unit rooted at ip 0 of code with loops when a bridge or box exit lies outside every loop: its only gain is the prefix's dispatch, which a Go round trip outweighs. The rejected unit's site is disabled; its header sites keep entering.

Entry reuses the current frame (`FB = bp`, `Depth = 0`); exits rewrite it in place. Materialized frames finish threaded execution without observers.

## Limits

- OSR block 0 does not accept i64 parameters.
- i64 results use X0/X1 only for one or two results; wider result sets stay boxed.
- A wide (>49-bit) i64 takes `ExitBox` at the sites listed in Exits above.
- Container lowering requires `guard.shape`; null or mismatched representation deopts.
- Bridges resume except the denied ops in Exits (above); `RETURN_CALL`'s `ExitDeopt` does not, nor an `ExitCall` to a coroutine function. `YIELD` and `RESUME` never reach an exit at all: they make the translator decline the whole unit at compile time.
- A catch block never runs natively: the interpreter runs it after the deopt.
- Host functions are not speculated at dynamic CALL sites; owned callees are not candidates.
- A hoisted `guard.shape` deopts at its loop header's state, an offset no op guards: its refutation falls back to the `refute` count instead of a generic site.

## Metrics

`WithProfiler` exposes `vm_jit_compiles_total{tier,outcome}`, `vm_jit_entries_total{tier}`, and `vm_jit_exits_total{kind}`.

## Related

- `architecture.md`
- `value-representation.md`
- `memory-model.md`
- `instruction-set.md`
- `testing.md`
- `jit-lessons.md`
