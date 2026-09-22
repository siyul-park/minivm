# JIT Internals

Current native runtime, compiler ownership, and execution contract.

`architecture.md` owns package boundaries; `coding-patterns.md` owns code design; `testing.md` owns test contracts; `jit-lessons.md` owns historical evidence.

## Status

The previous ARM64 JIT was removed (2026-09). Threaded execution and AOT optimization remain the semantic baseline; the native tier is implemented as one SSA-to-native pipeline. As of S2-P8, `interp.WithThreshold(n)` is opt-in and ARM64-only: it lets the interpreter enter compiled native code for a `*types.Function` after `n` calls to it. `ExitSafepoint` and `ExitRelease` resume the native activation; every other exit — a bridge, an unsupported call, or a terminator with no native form (`RETURN_CALL`, `YIELD`, `RESUME`) — deoptimizes it back to threaded execution (see Runtime below and `instruction-set.md` for per-opcode status). A Baseline address that is entered `native.promote`-many times submits an Optimized compile for the same function; an address that deoptimizes `native.refute`-many times is retired and its retiring tier is marked permanently failed (S2-P10), since the code that just refuted is the same unchanged compile a recompile would reproduce exactly, with no feedback yet to make it differ — a *different* tier (e.g. Baseline, after Optimized retires) may still warm up and compile once on its own. A Pool may share one published-code store, compile queue, and constant module across its interpreters. S3 work is tracked in GitHub issues.

As of S2-P10, `array.get`/`array.set`/`array.len`/`struct.get`/`struct.set`/`ref.is_null` lower on ARM64: `guard.shape` (`internal/jit/arm64`'s `shape`) checks a ref's heap object against the itab of the representation `ssa.Shape` admits — `types.TypedArray[T]` for a scalar element `Kind`, `*types.Array` for `KindRef`, `*types.Struct` (additionally matching `Shape.Type` when a specific one is named) — and deopts otherwise, so a null ref or a host array/struct is always caught, never crashed into. `transform/walk.go` no longer gates a declared array's element kind on the function being call-free (the guard makes the old gate unnecessary). The originally reported "SortStress wrong result after retire" was misdiagnosed as a retire/republish defect; the actual bug was a 4-byte `array.set` on a `TypedArray[int32]`/`TypedArray[int8]` shape (`internal/jit/arm64/container.go`'s `arraySet`) encoding a full 64-bit `STR` for an int32 element instead of the narrow `STRW` form, clobbering the following element — fixed by routing the 4-byte int case through `STRW`. `struct.set`'s `field` helper had a matching but independent divergence: an `i8` field was sign-extended across all 64 bits instead of sign-extending to 32 and zero-extending the rest, unlike `types.(*Struct).SetField`; fixed the same pass.

## Current owners

| Concern | Owner |
|---|---|
| Threaded execution | `interp/` |
| SSA IR | `internal/ssa/` |
| Bytecode to SSA and SSA passes | `transform/` |
| Machine encoding, executable memory, native execution | `internal/asm/` |
| ARM64 encoding | `internal/asm/arm64/` |
| Native runtime contract | `internal/jit/` |
| SSA lowering: layout, value registers, edge moves, loop budget | `internal/jit/compile/` |
| ARM64 lowering | `internal/jit/arm64/` |
| Profiling | `prof/` |

## Runtime contract

`internal/asm` owns the machine: how native code is entered, suspended, and resumed. `internal/jit` owns the policy: why native code left and what the interpreter does about it. Native code runs on a Go-allocated native stack owned by an `asm.State`, never on the goroutine stack, so a native activation can be suspended, worked on from Go, and resumed.

GC rule: `Context.Heap` addresses the interpreter heap (`[]types.Value`, `jit.SizeofValue` bytes — one interface word pair — per address), refreshed wherever `RC` is, since the heap moves when it grows. Native code reads only these interface words and the Go-runtime-stable field offsets of the object headers they point to (`jit.Offset*` in `internal/jit/heap.go`, `jit.Itab` for the expected word), and writes only non-pointer element/field words — never a Go pointer — so it can never leave the collector's own view of the heap referencing something it does not track.

| Symbol | Contract |
|---|---|
| `asm.State` | The machine state: the native stack, the Go registers saved while native code runs, and the native SP, PC, and register file at the last exit. Go reads and replaces saved registers through `Reg`/`SetReg`. Native code writes only scalar state; no Go pointer is stored on the native stack. |
| `asm.Enter(code, s)` / `asm.Resume(s)` | Switch to the native stack and call `code`, or continue the suspended activation (restore the register file, `SP = NSP`, `LR = PC`). Both report whether the activation is suspended at an exit rather than returned. |
| exit | Native code `BLR`s the stub at `asm.OffsetStub`. **An exit is a call**: the stub saves the register file, `LR → PC`, `SP → NSP`, and returns to Go; `Resume` is that call returning, so code that exits keeps its own LR in a frame like around any call. |
| `arm64.Ctx` (X26) | Holds the `*asm.State` while native code runs; pinned, never written by native code. X16/X17 are scratch for the exit protocol; X18/X28 are never touched. |
| `jit.Context` | Embeds `asm.State` as its first field — the pinned register names both — and adds what native code writes before an exit: the `Trap` and the exit identifier, at `jit.OffsetTrap`/`jit.OffsetExit`. `Exit()` reads the identifier; what it names is the code publisher's to say. The interpreter writes the bases `Stack`, `Heap`, `Globals`, `RC`, `Natives`, the VM stack's end `Top`, the call depth `Limit`, and the entry frame base `FB` before every `Enter`/`Resume`; `RC` and `Heap` move when the heap grows, so native code loads them at each use. Native code maintains `Depth` and `Records` (one `Record` per activation) and counts `Budget` down. Every field has an `Offset*` constant. |
| `jit.Trap` | `TrapReturn` (the activation returned; nothing to resume), `TrapDeopt` (abandon; the interpreter rebuilds state), `TrapBridge` (suspend; the interpreter acts, then resumes). |
| `jit.Enter(code, ctx)` / `jit.Resume(ctx)` | `asm.Enter`/`asm.Resume` on the context's state, reporting the `Trap`: `TrapReturn` when the activation returned, else what native code wrote. |
| `jit.Code` | The native code of one function at one tier, linked into an `asm.Buffer` of its own (`NewCode`). `Free` unmaps it exactly once; nothing may run it after. |
| `jit.Store` | Publishes native code and owns the natives table (`Context.Natives`) native calls read. `Publish` installs a higher tier than what is published, atomically, and retires what it replaces; a stale result is freed instead. `Retire` clears the natives entry directly. A retired code stays findable (`Find`) for suspended activations until `Reclaim` frees it, which runs only once no interpreter is bracketed inside native code (`Enter`/`Leave`). |

Nesting: while a state is suspended, `Enter` starts below the suspended frames (`NSP` is the exit SP) and the inner return restores `NSP`. Async preemption cannot land in native code, so native loops MUST reach a Go safepoint through a bridge within bounded time (a back-edge budget, added with the lowering).

### Register allocation

`asm.Assembler.Build` allocates every virtual register when the architecture implements `asm.Frame` (flow of each row, written operands, allocatable registers per bank, spill and reload rows). Allocation is linear scan over live intervals from block liveness, with no interval splitting: a value that cannot keep one register for its whole life — it is live across a `FlowCall` row, or a bank runs out — is spilled everywhere, reloaded into a fresh two-row register before a row that reads it and parked after a row that writes it — one register per value and row, so tied operands (`MOVK`) update what they reloaded. So every virtual register has exactly one `asm.Loc` for its whole life, a register or a spill slot, which is what an exit map records. Calls clobber every allocatable register. Spill slot `n` is at `SP + 8n`; a prologue reserves the area with the `asm.Slots()` operand, which `Build` sizes. ARM64 allocates X0–X15, X19–X25, X27 and D0–D31.

Instruction-cache maintenance for published code is user-mode ARM64 (`DC CVAU`/`IC IVAU`) in `icache_arm64.s`; no cgo is involved.

## Lowering

`transform.Translate` translates a whole function from one entry — ip 0 or a loop header — and gives every `OpExec`, return, and completion the interpreter state at its own instruction, so which operations a backend lowers and which it bridges is the backend's decision alone. S2 native compilation uses entry 0 only; loop-header entry and OSR are deferred to S3.

```text
bytecode → transform.Translate → internal/ssa → SSA passes → compile.Lower → asm.Assembler.Build → native code
```

`compile.Lower(f, machine, fn, objects, address)` lowers the entry-0 translation of `fn`, resolving the functions it calls through `objects`: one virtual register per SSA value by static type, blocks in reverse postorder, block parameters written by a parallel move on each edge (an edge that moves gets a stub of its own; a cycle goes through one scratch register per bank and width). `address` is the unit's own function address: a `CALL` whose resolved callee is `address` is its own recursion (see self calls below). A loop header counts `Budget` down before its first operation that carries a state, so a safepoint there has a complete interpreter state; a header without one is `ErrUnsupported`. `compile.Machine` is the target: it emits rows for the operations and terminators `compile` walks; it does not own SSA control flow.

A `CALL`'s constant callee is normally retained before the call and released after, matching the interpreter's own ownership of a pushed frame's reference. `compile.Lower` elides that pair — `compile.Call.Owned` is false and `Machine.Call` neither retains nor releases — when the callee's `OpConst` is retained exactly once and used exactly once, as that one call's callee: the constant pool already holds the callee alive for the call's own duration, so the pair is redundant. Any other shape (a second retain or use — e.g. a CSE-shared constant called from two sites) keeps the retain and release. The same fact rides the `ExitCall` map's `Owned` field: a materialized frame entered through a borrowed call has `release = false` on its own completion, and a deopt that replays a borrowed `ExitCall` threaded must `retainBox` the callee before pushing it, since the interpreter's own `CALL` releases what it adopts.

ARM64 activation ABI (`internal/jit/arm64`):

- X25 is the frame base, the address of the activation's VM slot 0, loaded from `Context.FB` by the prologue; parameters and locals are `[X25, #8i]`. X16 and X17 are scratch inside one row sequence; all three are reserved from allocation (`Assembler.Reserve`).
- The prologue pushes `Records[Depth] = {FB}`, stores its return address in `Record.PC`, increments `Depth`, and clears the locals after the parameters (the callee clears its own locals). Every return branches to one epilogue that decrements `Depth` and pops the frame.
- `OpReturn` first releases every slot whose kind can hold a reference (anything but `i32`/`f32`/`f64` after `Repr`) and clears it, as the interpreter's `RETURN` releases the frame it pops, then stores its results boxed from slot 0, where `RETURN` leaves them. A return over an owned operand that is not a result is `ErrUnsupported`. `OpComplete` stores its operands past the locals.
- A `CALL` of a constant function reference stores its arguments boxed at the callee's frame base (`X25 + 8*(slots + operands below)`), writes this activation's record `{SP, Exit}`, sets `Context.FB`, and `BLR`s `Context.Natives[address]` — or, when the callee is the unit's own address (a self call), `BL`s its own entry label directly, skipping the `Context.Natives` load and its missing-code check; the `Depth`/`Top` checks and the record writes stay either way, since they are the deopt contract. Afterwards it reloads `X25` from its record, releases the callee reference an owned call adopted (a borrowed call releases nothing), and loads the results from the callee's frame base. A callee without native code, `Depth` at `Context.Limit`, or a frame that would pass `Context.Top` takes an `ExitCall` instead. A call returning an `i64` is `ErrUnsupported`.
- An exit writes its id and trap to the `Context` and calls the exit stub (`X16` scratch). Deopt exits are never resumed; resumable exits return to the native activation only when the interpreter calls `Resume`.

Exits (`jit.Exit`, one map per exit id, returned by `compile.Lower`):

| Kind | Taken at | The interpreter | Native code then |
|---|---|---|---|
| `ExitDeopt` | a failed check — division by zero, an `i64` outside the inline range at a store or return, an `i64` slot word that is no inline integer — or an `OpExit` | rebuilds `Frames` and continues threaded | never resumes |
| `ExitBridge` | an `OpExec` the machine does not lower | materializes the exit and resumes the operation in exact threaded code, retaining the popped operands it does not `Adopts` | S2 never resumes native code; later bridge resumption is deferred |
| `ExitSafepoint` | a loop header, when `Budget` is spent | runs its safepoint and refills `Budget` | continues into the header |
| `ExitRelease` | an `OpRelease`, return, or call dropping a last reference | releases `Release`; the map has no `Frames` | continues |
| `ExitCall` | a `CALL` that cannot run natively | materializes the caller state and replays the `CALL` in exact threaded code | S2 never resumes native code |

Every value a map names is live up to its exit (an `asm/arm64` `USE` row) and has one `asm.Loc` for its life: a register the saved register file holds (`State.Reg`) or a spill slot (`State.Slot`). `Frames` read from the operation's `OpState`, except an `ExitCall`'s: the caller's state after the call, its operand stack cut below the arguments and its IP past the `CALL`. That map is also the one `Record.Exit` names while a native callee runs, and its values stay live across the call, so they sit in spill slots at `Record.SP`. A return or completion carries the state of its own instruction, so a wide `i64` result deopts there and the interpreter promotes it. An `i64` slot load is the slot word, unboxed only by its `OpGuardKind`; a word used any other way is `ErrUnsupported`, as are the shape, bounds, and value guards and `OpSuspend`.

The native tier MUST preserve threaded behavior as the semantic baseline. Native execution MUST use explicit runtime state rather than duplicate interpreter ownership.

### Tiers and the compile queue

`compile.Compile(u, m)` translates `u.Function` from `u.Module` at `u.Address`, verifies, runs `u.Tier`'s pass pipeline on a fresh `pass.Manager` — `jit.Baseline`: fold, dce; `jit.Optimized`: the O3 order (fold, promote, forward, cse, guard, hoist, dce) — verifies again, and lowers with `m` into a `jit.Code`. `compile.Queue` runs `Compile` on worker goroutines, each owning one machine, one unit per address in flight at a time: `Submit` refuses a second one until `Drain` collects the first; `Close` lets queued units finish and joins every worker. A `Job`'s `Code`, once handed to `Store.Publish`, is freed there if it turns out stale — a tier no higher than what is already published.

## Runtime

`interp.WithThreshold(n)` enables the JIT: `n >= 0` calls to one `*types.Function` before it compiles, only on `runtime.GOARCH == "arm64"`, and only without `WithHook`/`WithFuel` (their per-tick semantics need interpreter frames); `WithProfiler` is compatible. The `CALL` handler for a `*types.Function` — the dynamic and the fused `CONST_GET;CALL` path alike — calls into the interpreter's native runtime once, after its usual overflow/underflow checks and before it fills a frame; when native code runs the call to completion, no interpreter frame is pushed for it.

Compiles run asynchronously on `compile.Queue` (one unit per address in flight at a time); the interpreter drains finished jobs at its next call to that address and `Store.Publish`es a successful one. A failed compile marks that address's tier permanently unsupported for the interpreter's life; it is never resubmitted (see Tiers below for how a Baseline and an Optimized failure at the same address are tracked apart).

Only `ExitSafepoint` and `ExitRelease` resume native code: a safepoint checks the active `Run` context for cancellation and refills `Context.Budget`; a release performs `Interpreter.Release` on the reference `Context.Read` names for the exiting activation. Every other exit — `ExitDeopt`, `ExitBridge`, `ExitCall`, and a safepoint that finds the context cancelled — deoptimizes (S2-P6b): `native.deopt` materializes every activation `Context.Depth` counts as an interpreter frame, outermost first. The innermost activation's map is the exit that brought native code out; an outer activation's map is its own call-site record, `store.Find(Records[k+1].PC).Exits[Records[k].Exit]` (that PC lies in the outer activation's own code, since it is the return address the inner activation's prologue recorded).

A materialized frame runs code compiled exact rather than `i.code[addr]`: `i.code[addr]` may be fused (`threader.Compile`'s `fusions` table), and a fusion leaves no handler at the IPs it absorbs, so a frame resuming mid-fusion would have nothing to run at its own IP. `native` builds and caches this exact code per address on first use. Every operand-stack and promoted-local value the map names is boxed by its kind (`i32`/`i1`/`i8`/`f32` from the low 32 bits, `f64` and `ref` from the raw word unchanged, a wide `i64` promoted through `boxI64`, exactly as a slot store would); an operand not already `Owned` is retained after boxing, and a promoted local's old word is released before the new one is stored, matching `LOCAL_SET`. Every native activation but the outermost was entered by a native `CALL`, whose own `Owned` decision (see Lowering above) says whether it adopted its callee's reference: a materialized frame releases on return only when the call site that entered it was owned, read from that call site's own `ExitCall` map (`store.Find(Records[k+1].PC).Exits[Records[k].Exit].Owned`) — the same object the outer activation's own frame content comes from. The outermost activation's ownership is the entering call site's own instead (a fused `CONST_GET;CALL` borrows its target; a dynamic `CALL` owns it), the same asymmetry the entering hook's `release`/`advance` parameters already carry. An `ExitCall` additionally has the caller's arguments already boxed in memory at the callee's would-be frame base: the interpreter adopts them onto the operand stack, pushes the callee reference — `retainBox`ed first when that `ExitCall`'s own call site was borrowed, since the interpreter's replayed `CALL` releases what it adopts — and rewinds the innermost frame's `ip` from past the `CALL` to the `CALL` itself (one byte), so dispatch performs the call threaded next. An `ExitSafepoint` cancellation deoptimizes the same way and returns to threaded dispatch, whose next safepoint reports the context error as it always does — an error no guest handler can catch.

Because every unresumable exit deoptimizes, no guest code ever runs while a native activation is suspended: one native run at a time per interpreter, and `jit.Enter` never nests.

Tiers (S2-P6c): an address's Baseline code counts its own native entries; once that count reaches `native`'s `promote` threshold, the interpreter submits an Optimized unit for it (unless Optimized already failed there). `Store.Publish` installs the higher tier and retires Baseline once the Optimized compile finishes. A compile failure is tracked per (address, tier), so a failed Optimized attempt leaves Baseline running and is never resubmitted, and a failed Baseline attempt does not block a later Optimized one at the same address (there will not be one, since nothing runs natively to promote it, but the isolation is real either way).

Refute and retire (S2-P6c, revised S2-P10): every deoptimization counts against the address that deoptimized; once that count reaches `native`'s `refute` threshold, the interpreter retires the address's code (`Store.Retire`) once it is back outside native code (after `Store.Leave`). The retiring code's own tier is then marked permanently failed (`native.markFailed`, keyed by the tier the retired code was compiled at) before its call, entry, and deopt counts reset (`native.forget`): the code that just refuted is unchanged bytecode compiled the same way a recompile would reproduce exactly, and there is no feedback yet that could make that recompile differ, so `count`/`promote` never resubmit that (address, tier) again. A *different* tier at the same address is not the same compile and is not blocked by this — e.g. once Optimized retires and fails, a fresh Baseline compile may still warm up and run (and itself retire and fail independently, but no further). A permanent compile failure from an outright compile error was already unaffected by retirement, since it never depended on runtime behavior; retirement's own failure marking now follows the same permanence.

Pool sharing (S2-P6c): the JIT parts that hold no interpreter-specific state — the published-code `Store`, the compile `Queue`, and the constant `Module` — live in one reference-counted runtime a `Pool` may share across every interpreter it creates for one `Program`: the first interpreter's is adopted as the pool's, and each later one swaps onto it once its own freshly loaded constants match (interpreters of the same `Program` always do; a mismatch leaves that interpreter on its own runtime instead). `jit.Context` and the per-address tiering counters stay on each interpreter. Draining a shared compile queue may collect a job a different interpreter submitted; whichever interpreter drains it publishes it, since `Store.Publish` is safe and idempotent on a stale result. The runtime closes exactly once, when the last interpreter using it releases its reference — a `Pool` cannot always name the interpreter that closes last, since `Pool.Put` on a closed pool can defer an outstanding interpreter's `Close` to that call.

Metrics, only with `WithProfiler`: `vm_jit_compiles_total{tier,outcome=ok|unsupported|failed}`, `vm_jit_entries_total{tier}`, `vm_jit_exits_total{kind}`.

## Evidence

`jit-lessons.md` records the previous implementation's evidence and the design decisions derived from it.

## Related

- `architecture.md`
- `value-representation.md`
- `testing.md`
- `jit-lessons.md`
