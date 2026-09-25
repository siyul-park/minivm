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

`compile.Unit.OSR` / `jit.Code.OSR` mark OSR units; a header can sit at ip 0.

## Owners

| Concern | Owner |
|---|---|
| Threaded execution, tiering, exit handling | `interp/` (`native.go`, `osr.go`) |
| SSA IR | `internal/ssa/` |
| Bytecode to SSA, SSA passes | `transform/` |
| Encoding, allocation, executable memory, enter/resume | `internal/asm/`, `internal/asm/arm64/` |
| Runtime contract, code, store | `internal/jit/` |
| Lowering driver, compile queue | `internal/jit/compile/` |
| ARM64 lowering | `internal/jit/arm64/` |

## Runtime contract

| Symbol | Contract |
|---|---|
| `asm.State` | Native stack, saved Go registers, native SP/PC/register file at the last exit. No Go pointer on the native stack. |
| `asm.Enter` / `asm.Resume` | Run code on the native stack / continue a suspended activation. Report whether it stopped at an exit. |
| exit stub | Native code `BLR`s `asm.OffsetStub`; the stub saves registers and returns to Go. `Resume` returns from that call. |
| `jit.Context` | `asm.State` first, then `Trap`, exit id, bases (`Stack`, `Heap`, `Globals`, `RC`, `Natives`, `Entries`), `Top`, `Limit`, `FB`, `Depth`, `Records`, `Budget`. The interpreter writes bases before every `Enter`/`Resume`. |
| `jit.Trap` | `TrapReturn`, `TrapDeopt`, `TrapBridge`. |
| `jit.Code` | One unit's native code at one tier. `Free` unmaps once. |
| `jit.Store` | Published code and `Context.Natives`. |

- Native code never runs on a goroutine stack; async preemption cannot reach it, so loops poll `Budget`.
- Native code writes no Go pointer. It reads heap interface words through `Context.Heap` and object fields at `jit.Offset*`.
- Registers: X24 loop budget, X25 frame base, X27 activation depth, X26 context, X16/X17 scratch, X18/X28 untouched. Allocatable: X0–X15, X19–X23, D0–D31.
- X24 mirrors `Context.Budget`: exits store it, resumed exits and the Go entry stub load it.
- Allocation: linear scan, no splitting; a value live across a call or under pressure spills for its whole life. Calls clobber every allocatable register. Exit maps keep ordinary mapped values live through their stubs. Promoted locals are deopt-only state: a call keeps them live across itself; any other exit saves a non-live one on its cold path into a fixed spill home.
- In a function with a call, a scalar or ref constant no loop block uses is loaded at each use and at each exit stub instead of spilled; an `ExitCall` map keeps its constants live across the call, since a deeper trap reads them from spill slots. Other constants keep one register.

## Pipeline

| Stage | Contract |
|---|---|
| Translate | bytecode → SSA; attach interpreter state to each `OpExec`, return, and completion; block 0 has no predecessors |
| Baseline | fold → DCE |
| Optimized | fold → forward → CSE → guard → hoist → DCE → promote → DCE |
| Lower | assign registers by SSA type, order blocks in reverse postorder, resolve block parameters with edge moves |
| Build | assemble, allocate, encode, publish through `jit.Code` |

A loop header `MUST` have state before its budget check. An OSR unit loads block-0 parameters from the current operand stack and clears no locals.

A constant callee stays borrowed when every retain is paired with a call-site use; `guard.value` does not count as a use. A dynamic `CALL` with one recorded feedback target becomes a guarded constant call only for a borrowed callee.

## ARM64 activation

| Area | Contract |
|---|---|
| Prologue | Push `Records[Depth]`, save `Record.PC`, count `Entries[address]` when enabled, clear non-parameter locals; OSR skips clearing. |
| Store | Reference-capable `OpStore` releases the old slot value before overwrite, matching threaded `LOCAL_SET`. |
| Return | `OpReturn` releases ref slots, returns up to two register results in X0/X1, or stores boxed results; `OpComplete` writes past locals. |
| Call | Constant calls box arguments into the callee frame, then use `Context.Natives[addr]`; self-calls use the unit entry. Missing code, depth, or frame space takes `ExitCall`. |

### Call convention

| State | Contract |
|---|---|
| X25 | frame base; caller-maintained and adjusted by `8·Base` around calls |
| X27 | activation depth; prologue/epilogue step it; exits store exact depth to `Context.Depth` |
| X0/X1 results | one or two results, including i64; native-to-native i64 stays raw |
| X0/X1 arguments | one or two parameters, including i64; each still has a boxed slot |
| Go entry | loads X25/X27, reads argument slots, calls body, then boxes register results |

`Code.Native()` is the body at offset 0; `Code.Entry()` is the Go stub after the epilogue. i64 entry arguments are unboxed with `SBFX #0,#49`; a threaded caller whose slot holds a heap i64 ref declines native entry. OSR reads slots.

## Exits

| Kind | Taken at | Resumes native |
|---|---|---|
| `ExitDeopt` | failed check, `OpExit` | no |
| `ExitBridge` | unlowered `OpExec` | yes, when `bridgeable` |
| `ExitSafepoint` | loop header, `Budget` spent | yes |
| `ExitRelease` | dropping a last reference | yes |
| `ExitCall` | `CALL` that cannot run natively | no |
| `ExitBox` | a wide (> 49-bit) i64 at a store, slot return, call argument, or `OpComplete` | yes |

A non-resuming exit materializes native activations outermost-first, then continues threaded; `jit.Enter` never nests.

| Exit | Resume rule |
|---|---|
| Bridge | Only `STRUCT_NEW`, `STRUCT_NEW_DEFAULT`, `ARRAY_NEW_DEFAULT` run once through `native.bridge`; all other bridges deopt. |
| Release | `Exit.Word` identifies the last ref; interpreter owns reclamation, then native resumes. |
| Box | `Exit.Word` carries the wide i64; interpreter allocates a boxed value, then native resumes. |
| Trap | A bridge/box trap abandons its extra retains and follows normal deopt so the instruction executes once. |

A bridge receives only its lowered `SSA Args` through `Exit.Pops`; it uses a scratch stack and leaves native registers untouched. The materializer retains borrowed refs; a boxed wide i64 is a fresh owned heap value. Bridge/box resumption shares `resume`/`amortize`.

## Store and tiers

| Concern | Contract |
|---|---|
| Publish | Non-OSR code replaces only a lower tier at an address; OSR code installs once at `(address, ip)`. |
| Retire | `Retire`/`RetireAt` unpublish; retired code remains discoverable until safe to reclaim. |
| Reclaim | `Reclaim` frees code only after no interpreter remains native. |
| Promotion | Baseline entries count calls; a live Baseline reaching `jit.Promote` queues Optimized. Optimized/OSR entries do not count. |
| Failure | A deopt refutes that tier. A compile failure is permanent only when feedback is unchanged from its snapshot. |
| Bridges | Repeated unamortized bridges retire the site after `amortize` work is absent between resumes. |
| Async | `compile.Queue` compiles one unit per address; publication is drained at the next call, OSR observation, or safepoint. |
| Pool | `Pool` shares `Store`, `Queue`, and module data; each interpreter keeps its own `jit.Context`, feedback, counters, and failure marks. A pooled interpreter whose deopts refute shared code retires it for the pool and blocks only its own tier. |

## OSR

Every loop header, including module code, has an observer. After the threshold it queues an Optimized unit and checks `Store.CodeAt` every 256 back edges. Compile failure or refutation restores the threaded handler and disables that observer.

Entry reuses the current frame (`FB = bp`, `Depth = 0`); exits rewrite it in place. Materialized frames finish threaded execution without observers.

## Limits

- OSR block 0 does not accept i64 parameters.
- i64 results use X0/X1 only for one or two results; wider result sets stay boxed.
- Wide i64 values (>49 bits) use `ExitBox` at stores, slot returns, call arguments, and `OpComplete`.
- Container lowering requires `guard.shape`; null or mismatched representation deopts.
- Only the allowlisted bridge ops in Exits resume; `ExitCall`, `RETURN_CALL`, `YIELD`, and `RESUME` do not.
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
