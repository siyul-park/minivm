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

| Tier | Passes |
|---|---|
| `jit.Baseline` | fold, dce |
| `jit.Optimized` | fold, forward, cse, guard, hoist, dce, promote, dce |

- `Translate` gives every `OpExec`, return, and completion the interpreter state at its instruction. Block 0 never has predecessors.
- `Lower` assigns one register per SSA value by type, orders blocks in reverse postorder, and resolves block parameters by parallel moves on edges.
- A loop header needs a state-bearing operation before its budget check, or lowering fails.
- An OSR unit loads block-0 parameters (the operand stack at the header) in its prologue and clears no locals.
- A constant callee is borrowed, no retain/release around any of its calls, when its retains and uses equal its call-site count: this also covers one CSE'd callee shared by several call sites, each with its own retain-before-call pair.

## ARM64 activation

- Prologue: push `Records[Depth]`, store return address in `Record.PC`, optionally count the entry at `Context.Entries[address]`, clear non-parameter locals (not on OSR).
- `OpStore` to a reference-capable slot (`Type` is `ssa.TypeRef`, which also represents a dynamically typed value) releases the slot's old occupant before overwriting it, matching threaded `LOCAL_SET`.
- `OpReturn` releases reference-capable slots, then moves results to X0/X1 (see Call convention) or stores them boxed from slot 0. `OpComplete` stores results past the locals.
- `CALL` to a constant function: box args at the callee frame, `BLR Context.Natives[addr]`, or `BL` the unit's own entry for a self call. No native code, `Depth == Limit`, or frame past `Top` → `ExitCall`.

### Call convention

- X25 (frame base) and X27 (`Context.Depth`) are pinned and caller-maintained: a call adds `8·Base` to X25 around `BL`/`BLR`; prologue and epilogue step X27; only exits store it to `Context.Depth`, which is exact at every trap.
- `Code.Native()` is the body at offset 0, installed in `Natives`. `Code.Entry()` is a Go entry stub after the epilogue: it loads X25/X27 from `Context.FB`/`Context.Depth`, calls the body, and boxes register results into the frame.
- A function with one or two results, i64 included (`compile.registers`), returns them in X0/X1; its callers read them after a `DEF` row. Native-to-native, an i64 result is the raw word; only the Go entry stub boxes it, narrow inline or heap, matching threaded `RETURN`. `OpComplete` never uses registers.
- A function with one or two parameters, i64 included (`compile.arguments`), also receives them in X0/X1; every argument still has its boxed slot. A block-0 load of such a parameter reads the register until a store to its slot; every other load reads the slot. Its guard.kind (i64 only) moves instead of unboxing: the register already holds the raw payload. Native-to-native, the caller moves the callee's already-guarded raw i64 into X0/X1, same as any other register argument. The Go entry stub instead loads it from the parameter's slot and unboxes it inline (`SBFX #0,#49`): `interp.native.call` declines native entry when an i64 register argument's slot holds a heap ref (`jit.Code.Arguments`), running the call threaded instead, uncounted. OSR units read slots.

## Exits

| Kind | Taken at | Resumes native |
|---|---|---|
| `ExitDeopt` | failed check, `OpExit` | no |
| `ExitBridge` | unlowered `OpExec` | yes, when `bridgeable` |
| `ExitSafepoint` | loop header, `Budget` spent | yes |
| `ExitRelease` | dropping a last reference | yes |
| `ExitCall` | `CALL` that cannot run natively | no |
| `ExitBox` | a wide (> 49-bit) i64 at a store, slot return, call argument, or `OpComplete` | yes |

A non-resuming exit rebuilds every native activation as an interpreter frame, outermost first, and continues threaded. `jit.Enter` never nests.

`interp.bridgeable` (`STRUCT_NEW`, `STRUCT_NEW_DEFAULT`, `ARRAY_NEW_DEFAULT`) names the `ExitBridge` opcodes whose threaded handler `native.bridge` may run once, in place: it boxes `Exit.Pops` trailing operands from the exit map (retaining a borrowed one, or one native itself will separately release, per `Exit.Adopts`), runs the handler against a scratch stack past the frame's declared locals, and on success unboxes its results into `Context.Results` and resumes. Promoted locals and other live values are untouched — `asm.Resume` already restores every allocatable register regardless. A trap declines instead of unwinding: it undoes its own extra retains and falls to the unchanged deopt path, so the failed instruction runs exactly once, under `dispatch`'s own recover, with correct frame state. Any other `ExitBridge` opcode (containers reaching a `*HostArray`/`*HostMap`/`*HostStruct`, which can call back into the interpreter, and `STRING_CONCAT`, whose handler releases its operands before its own only possible panic) still deopts. `Exit.Pops` (an `ExitBridge`-only field alongside `Code`/`Adopts`) is the count of `Frame.Stack`'s own trailing entries the resumed op reads as its arguments — set at compile time from the lowered op's own SSA `Args`, since a bridge must not touch the rest of the operand stack a full deopt's `Frame.Stack` also carries.

`ExitRelease` and `ExitBox` share `Exit.Word`: the reference to release, or the raw i64 to box. `arm64.box`'s wide path exits through `ExitBox`; the interpreter allocates `types.I64(v)` into `Context.Results[0]`, which the stub reloads into the register the inline path produces. An allocation panic declines and the exit deopts through its full state map. Resumed boxes and bridges share one `resume`/`amortize` policy (`native.serve`).

## Store

- `Publish`: a non-OSR code installs `Code.Native()` into `Natives[addr]` if its tier is higher, retiring the old one; an OSR code installs into an `(address, ip)` map once.
- `Retire` / `RetireAt` unpublish; `Find(pc)` and `CodeAt` still see retired code.
- `Reclaim` frees retired code once no interpreter is inside native code.

## Tiers

- Baseline function prologues count `Context.Entries[address]`, including interpreted and native-to-native entries. When a published Baseline reaches `jit.Promote`, `drain` submits Optimized; Optimized and OSR entries do not pay the counter cost.
- Promotion is checked only for addresses whose published code is still Baseline.
- Deopts reach `refute` → retire; that tier never recompiles for the address. A `CALL` to an address whose Baseline failed costs one check.
- `resume` consecutive bridges unamortized by real native work (fewer than `amortize` back edges since the last amortized one) also retire, exactly like a refuted deopt: the round trip a resumed bridge pays for is only worth staying native when other native work offsets it.
- Compiles are async on `compile.Queue`, one unit per address; the interpreter drains and publishes at its next call, header observation, or safepoint.
- A `Pool` shares `Store`, `Queue`, and module data; each interpreter has its own `jit.Context`.

## OSR

- Every loop header of every function known at construction, module included, is wrapped by an observer.
- Past the threshold it submits a unit directly at `jit.Optimized` and polls `Store.CodeAt` every 256 back edges.
- A site whose unit fails to compile or reaches `refute` restores its threaded handler and is never polled again.
- Entry reuses the current interpreter frame (`FB = bp`, `Depth = 0`). An exit rewrites that frame in place. A materialized frame finishes its call threaded without observers.

## Metrics

With `WithProfiler`: `vm_jit_compiles_total{tier,outcome}`, `vm_jit_entries_total{tier}`, `vm_jit_exits_total{kind}`.

## Limits

- No `i64` OSR block-0 parameter. A `CALL` argument is register-passed when its callee has one or two parameters (i64 included); the Go entry stub's inline unbox means a threaded caller whose i64 argument slot holds a heap ref must decline native entry (`interp.native.call`) rather than cross it. A `CALL` result is register-passed when its callee has one or two results (i64 included); a callee with three or more still refuses an i64 one. `guard.kind` reads a heap-promoted i64 as threaded `borrowI64` does; only a ref to a non-`I64` object deopts. Promote guards each promoted i64 local once, in block 0, against the unit's entry state (`ssa.Function.Entry`). A wide (> 49-bit) i64 at a store, slot return, call argument, or `OpComplete` boxes through `ExitBox`; a store releases an i64 slot's old heap occupant, as threaded `LOCAL_SET` does.
- Container ops lower behind `guard.shape`, which deopts on null or a mismatched representation.
- Unlowered opcodes bridge; see `instruction-set.md`. Only `STRUCT_NEW`, `STRUCT_NEW_DEFAULT`, and `ARRAY_NEW_DEFAULT` resume (see Exits); every other bridge still deopts, `ExitCall` still deopts (no nested `jit.Enter`; the callee runs interpreted and the caller resumes threaded). `RETURN_CALL`, `YIELD`, `RESUME` have no native form.
- A resumed bridge round-trips through Go, so a site whose bridges recur with fewer than `amortize` back edges between them (no intervening loop work to pay for the trip) retires after `resume` such bridges in a row, same as a refuted deopt (see Tiers/OSR).

## Related

`architecture.md`, `value-representation.md`, `memory-model.md`, `instruction-set.md`, `testing.md`, `jit-lessons.md`
