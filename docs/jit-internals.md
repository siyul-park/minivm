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
- Registers: X25 frame base, X26 context, X16/X17 scratch, X18/X28 untouched. Allocatable: X0–X15, X19–X24, X27, D0–D31.
- Allocation: linear scan, no splitting; a value live across a call or under pressure spills for its whole life. Calls clobber every allocatable register. The exit stub preserves every allocatable register; `Machine.Exit` places its map's `USE` rows after `EXIT` so mapped values stay live through the stub.

## Pipeline

| Tier | Passes |
|---|---|
| `jit.Baseline` | fold, dce |
| `jit.Optimized` | fold, forward, cse, guard, hoist, dce, promote, dce |

- `Translate` gives every `OpExec`, return, and completion the interpreter state at its instruction. Block 0 never has predecessors.
- `Lower` assigns one register per SSA value by type, orders blocks in reverse postorder, and resolves block parameters by parallel moves on edges.
- A loop header needs a state-bearing operation before its budget check, or lowering fails.
- An OSR unit loads block-0 parameters (the operand stack at the header) in its prologue and clears no locals.
- A constant callee used once is borrowed: no retain/release around the call.

## ARM64 activation

- Prologue: push `Records[Depth]`, store return address in `Record.PC`, optionally count the entry at `Context.Entries[address]`, clear non-parameter locals (not on OSR).
- `OpStore` to a reference-capable slot (`Type` is `ssa.TypeRef`, which also represents a dynamically typed value) releases the slot's old occupant before overwriting it, matching threaded `LOCAL_SET`.
- `OpReturn` releases reference-capable slots, stores boxed results from slot 0. `OpComplete` stores results past the locals.
- `CALL` to a constant function: box args at the callee frame, `BLR Context.Natives[addr]`, or `BL` the unit's own entry for a self call. No native code, `Depth == Limit`, or frame past `Top` → `ExitCall`.

## Exits

| Kind | Taken at | Resumes native |
|---|---|---|
| `ExitDeopt` | failed check, `OpExit` | no |
| `ExitBridge` | unlowered `OpExec` | no |
| `ExitSafepoint` | loop header, `Budget` spent | yes |
| `ExitRelease` | dropping a last reference | yes |
| `ExitCall` | `CALL` that cannot run natively | no |

A non-resuming exit rebuilds every native activation as an interpreter frame, outermost first, and continues threaded. `jit.Enter` never nests.

## Store

- `Publish`: a non-OSR code installs into `Natives[addr]` if its tier is higher, retiring the old one; an OSR code installs into an `(address, ip)` map once.
- `Retire` / `RetireAt` unpublish; `Find(pc)` and `CodeAt` still see retired code.
- `Reclaim` frees retired code once no interpreter is inside native code.

## Tiers

- Baseline function prologues count `Context.Entries[address]`, including interpreted and native-to-native entries. When a published Baseline reaches `promote`, `drain` submits Optimized; Optimized and OSR entries do not pay the counter cost.
- Deopts reach `refute` → retire; that tier never recompiles for the address. An OSR site that reaches `refute` restores its threaded handler.
- Compiles are async on `compile.Queue`, one unit per address; the interpreter drains and publishes at its next call, header observation, or safepoint.
- A `Pool` shares `Store`, `Queue`, and module data; each interpreter has its own `jit.Context`.

## OSR

- Every loop header of every function known at construction, module included, is wrapped by an observer.
- Past the threshold it submits a unit directly at `jit.Optimized` and polls `Store.CodeAt` every 256 back edges.
- Entry reuses the current interpreter frame (`FB = bp`, `Depth = 0`). An exit rewrites that frame in place.

## Metrics

With `WithProfiler`: `vm_jit_compiles_total{tier,outcome}`, `vm_jit_entries_total{tier}`, `vm_jit_exits_total{kind}`.

## Limits

- No `i64` OSR block-0 parameter; no `i64` `CALL` result.
- Container ops lower behind `guard.shape`, which deopts on null or a mismatched representation.
- Unlowered opcodes bridge; see `instruction-set.md`. `RETURN_CALL`, `YIELD`, `RESUME` have no native form.

## Related

`architecture.md`, `value-representation.md`, `memory-model.md`, `instruction-set.md`, `testing.md`, `jit-lessons.md`
