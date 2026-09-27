# JIT Internals

Native tier: ownership, runtime contract, lifecycle.

`architecture.md` owns package boundaries; `jit-lessons.md` owns prior design evidence.

## Status

- Opt-in via `interp.WithThreshold(n)` (`n >= 0`). ARM64 only. Disabled with `WithHook`/`WithFuel`.
- Threaded execution is the semantic baseline.

```text
bytecode → transform.Translate → SSA passes (per tier) → compile.Lower → asm.Assembler.Build → jit.Code
```

| Entry | Trigger | Root |
|---|---|---|
| `CALL` | `n` interpreted calls to a `*types.Function` | ip 0 |
| OSR | `n` back edges at a loop header, module code included | the header |
| Module entry | `max(n, 2)` runs of loop-free module code | ip 0 |

`compile.Unit.OSR` / `jit.Code.OSR` mark OSR units, module entry included; a header can sit at ip 0.

## Owners

`architecture.md`'s Package Ownership table owns package boundaries. Within `interp/`, threaded execution, tiering, and exit handling specifically live in `native.go` and `osr.go`.

## Runtime contract

| Symbol | Contract |
|---|---|
| `asm.State` | Native stack, saved Go registers, native SP/PC/register file at the last exit. No Go pointer on the native stack. |
| `asm.Enter` / `asm.Resume` | Run code on the native stack / continue a suspended activation. Report whether it stopped at an exit. |
| exit stub | Native code `BLR`s `asm.OffsetStub`; the stub saves registers and returns to Go. `Resume` returns from that call. |
| `jit.Context` | `asm.State` first, then `Trap`, exit id, then `Heap`, `Globals`, `RC`, `Natives`, `Entries`, `Top`, `FB`, `Depth`, `Limit`, `Budget`, `Results`, `Records`. `Results` stages a bridge or box exit's result words for native code to reload on resume. The interpreter writes every base before `Enter`; only `Heap` and `RC` are rewritten before each `Resume`, since serving an exit can only relocate those two append-grown slices. |
| `jit.Trap` | `TrapReturn`, `TrapDeopt`, `TrapBridge`. |
| `jit.Code` | One unit's native code at one tier. `Free` unmaps once. |
| `jit.Store` | Published code and `Context.Natives`. |

- Native code never runs on a goroutine stack; async preemption cannot reach it, so back edges, calls, and returns spend `Budget` explicitly. Back edges and calls branch to safepoint on exhaustion; returns only credit the work.
- Native code writes no Go pointer. It reads heap interface words through `Context.Heap` and object fields at `jit.Offset*`.
- Registers: X24 budget, X25 frame base, X27 activation depth, X26 context, X16/X17 scratch, X18/X28 untouched. Allocatable: X0–X15, X19–X23, D0–D31.
- X24 mirrors `Context.Budget`: exits store it, resumed exits reload it, and a normal Go entry stores it back after native return.
- Allocation: linear scan, no splitting; a value live across a call or under pressure spills for its whole life. Calls clobber every allocatable register. Exit maps keep ordinary mapped values live through their stubs. Promoted locals are deopt-only state: calls and safepoints keep them live across resumption; other exits save a non-live one on their cold path into a fixed spill home.
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

Translate never retains a constant callee: the pool keeps it alive, so its call state does not own it and the native call neither retains nor releases it. Lower takes `Call.Owned` from that state. A dynamic `CALL` with one recorded feedback target becomes a guarded constant call only for a callee operand its state does not own.

A reference parameter its function never writes (`transform.Borrows`) is borrowed: a native caller lends a local- or constant-backed argument without a retain, and releases an owned one after the call.

## ARM64 activation

| Area | Contract |
|---|---|
| Prologue | Push `Records[Depth]`, save `Record.PC`, count `Entries[address]` when enabled, start non-parameter locals at their zeros (`value-representation.md`), loading each distinct zero once; OSR starts none. |
| Store | Reference-capable `OpStore` releases the old slot value before overwrite, matching threaded `LOCAL_SET`. |
| Return | `OpReturn` releases reference slots; borrowed parameters only at depth 1 (the Go-entered activation), returns up to two register results in X0/X1, or stores boxed results; `OpComplete` writes past locals. |
| Call | Constant calls box arguments into the callee frame, then use `Context.Natives[addr]`; self-calls use the unit entry. Missing code, depth, or frame space takes `ExitCall`. |

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
| `ExitDeopt` | failed check, `OpExit` | no |
| `ExitBridge` | unlowered `OpExec` | yes, when `bridgeable` |
| `ExitSafepoint` | loop header or native call when `Budget` is spent | yes |
| `ExitRelease` | dropping a last reference | yes |
| `ExitCall` | `CALL` that cannot run natively | no |
| `ExitBox` | a wide (> 49-bit) i64 at a store, slot return, call argument, or `OpComplete` | yes |

A deopt stub sits out of line after the next terminator that does not fall through, so the values its map names stay live only that far. Safepoint, release, box, and call stubs follow the body; a bridge and `OpExit` exit inline.

`jit.Kind.Resumes` owns the "Resumes native" column above; `compile.stub`/`function` and `arm64.Machine.Exit` all read it instead of comparing kinds themselves. A non-resuming exit's stub ends in `BRK` and skips the `X24` (`Budget`) reload, since native code never runs past it; it materializes native activations outermost-first, then continues threaded, and `jit.Enter` never nests.

| Exit | Resume rule |
|---|---|
| Bridge | Only `STRUCT_NEW`, `STRUCT_NEW_DEFAULT`, `ARRAY_NEW_DEFAULT`, `STRING_NEW_UTF32`, `STRING_ENCODE_UTF32`, `STRING_LEN`, `STRING_CONCAT` run once through `native.bridge`; all other bridges deopt. |
| Release | `Exit.Word` identifies the last ref; interpreter owns reclamation, then native resumes. |
| Box | `Exit.Word` carries the wide i64; interpreter allocates a boxed value, then native resumes. |
| Trap | A bridge/box trap drops the retains its handler did not consume, restoring each operand's count, and follows normal deopt so the instruction executes once. |

A bridge receives only its lowered `SSA Args` through `Exit.Pops`; it uses a scratch stack and leaves native registers untouched. A bridged op adopts no operand (`transform` adopts only a stored value, for a heap write with no result): the bridge retains every ref operand for its handler, and native code releases the ones it owns afterwards. A handler that panics may already have released operands; the retains keep them alive. The materializer retains borrowed refs; a boxed wide i64 is a fresh owned heap value. Bridge/box resumption shares `resume`/`amortize`.

`Exit.Lent` slots are retained when a callee is materialized or an ExitCall replayed.

## Store and tiers

| Concern | Contract |
|---|---|
| Publish | Non-OSR code replaces only a lower tier at an address; OSR code installs once at `(address, ip)`. |
| Retire | `Retire`/`RetireAt` unpublish; retired code remains discoverable until safe to reclaim. |
| Reclaim | `Reclaim` frees code only after no interpreter remains native. |
| Promotion | Baseline entries count calls; a live Baseline reaching the interpreter's graduate threshold queues Optimized. Optimized/OSR entries do not count. |
| Failure | Repeated deopts retire that tier once they reach the interpreter's refute threshold. A compile failure is permanent only when feedback is unchanged from its snapshot. |
| Bridges | Each code address/site keeps its unamortized bridge count and native work across entries. A bridge is amortized when work since the previous served bridge reaches `amortize` (4); TrapReturn finalizes the last work segment. That clears the site's count and the bridged function's own count. Calls, returns, back edges, and native callees all contribute to the work. |
| Async | `compile.Queue` compiles one unit per address; publication is drained at the next call, OSR observation or entry, or safepoint. |
| Pool | `Pool` shares `Store`, `Queue`, module data, and the Baseline promotion candidate list; each interpreter keeps its own `jit.Context`, feedback, counters, and failure marks. A pooled interpreter whose entries reach the graduate threshold requests Optimized even if a different interpreter drained its Baseline job. A pooled interpreter whose deopts refute shared code retires it for the pool and blocks only its own tier. |

## OSR

Every loop header, including module code, has an observer. After the threshold it queues an Optimized unit and checks `Store.CodeAt` every 256 back edges. Compile failure or refutation restores the threaded handler and disables that observer.

Loop-free module code has one observer at ip 0 instead, sharing the same entry, exit, retirement, and refutation. It counts runs, queues on the second run at the earliest, and checks `Store.CodeAt` every run. Loop-free code reaches no safepoint, so its entry declines a cancelled run, leaving threaded code to report it. Module code with a loop has no ip-0 observer: its unit would take the address's one queue slot ahead of the header units.

Entry reuses the current frame (`FB = bp`, `Depth = 0`); exits rewrite it in place. Materialized frames finish threaded execution without observers.

## Limits

- OSR block 0 does not accept i64 parameters.
- i64 results use X0/X1 only for one or two results; wider result sets stay boxed.
- A wide (>49-bit) i64 takes `ExitBox` at the sites listed in Exits above.
- Container lowering requires `guard.shape`; null or mismatched representation deopts.
- Only the allowlisted bridge ops in Exits (above) resume; `ExitCall` and `RETURN_CALL`'s `ExitDeopt` do not. `YIELD` and `RESUME` never reach an exit at all: they make the translator decline the whole unit at compile time.
- Closures/host functions are not speculated at dynamic CALL sites; owned callees are not candidates.

## Metrics

`WithProfiler` exposes `vm_jit_compiles_total{tier,outcome}`, `vm_jit_entries_total{tier}`, and `vm_jit_exits_total{kind}`.

## Related

- `architecture.md`
- `value-representation.md`
- `memory-model.md`
- `instruction-set.md`
- `testing.md`
- `jit-lessons.md`
