# JIT Internals

Current status and planned ownership for the JIT rebuild.

`architecture.md` owns package boundaries; `coding-patterns.md` owns code design; `testing.md` owns test contracts; `jit-lessons.md` owns historical evidence.

## Status

The previous ARM64 JIT was removed (2026-09). Threaded execution and AOT optimization remain the semantic baseline; the native tier is being rebuilt as one compiler pipeline. As of S2-P6b, `interp.WithThreshold` lets the interpreter enter compiled native code for a hot `*types.Function`: `ExitSafepoint` and `ExitRelease` resume it, every other exit deoptimizes it back to threaded execution (see Runtime below). Only the Baseline tier is ever submitted; tiering up, retiring refuted code, and pool sharing are S2-P6c.

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

| Symbol | Contract |
|---|---|
| `asm.State` | The machine state: the native stack, the Go registers saved while native code runs, and the native SP, PC, and register file at the last exit. Go reads and replaces saved registers through `Reg`/`SetReg`. Native code writes only scalar state; no Go pointer is stored on the native stack. |
| `asm.Enter(code, s)` / `asm.Resume(s)` | Switch to the native stack and call `code`, or continue the suspended activation (restore the register file, `SP = NSP`, `LR = PC`). Both report whether the activation is suspended at an exit rather than returned. |
| exit | Native code `BLR`s the stub at `asm.OffsetStub`. **An exit is a call**: the stub saves the register file, `LR → PC`, `SP → NSP`, and returns to Go; `Resume` is that call returning, so code that exits keeps its own LR in a frame like around any call. |
| `arm64.Ctx` (X26) | Holds the `*asm.State` while native code runs; pinned, never written by native code. X16/X17 are scratch for the exit protocol; X18/X28 are never touched. |
| `jit.Context` | Embeds `asm.State` as its first field — the pinned register names both — and adds what native code writes before an exit: the `Trap` and the exit identifier, at `jit.OffsetTrap`/`jit.OffsetExit`. `Exit()` reads the identifier; what it names is the code publisher's to say. The interpreter writes the bases `Stack`, `Globals`, `RC`, `Natives`, the VM stack's end `Top`, the call depth `Limit`, and the entry frame base `FB` before every `Enter`/`Resume`; `RC` moves when the heap grows, so native code loads it at each use. Native code maintains `Depth` and `Records` (one `Record` per activation) and counts `Budget` down. Every field has an `Offset*` constant. |
| `jit.Trap` | `TrapReturn` (the activation returned; nothing to resume), `TrapDeopt` (abandon; the interpreter rebuilds state), `TrapBridge` (suspend; the interpreter acts, then resumes). |
| `jit.Enter(code, ctx)` / `jit.Resume(ctx)` | `asm.Enter`/`asm.Resume` on the context's state, reporting the `Trap`: `TrapReturn` when the activation returned, else what native code wrote. |
| `jit.Code` | The native code of one function at one tier, linked into an `asm.Buffer` of its own (`NewCode`). `Free` unmaps it exactly once; nothing may run it after. |
| `jit.Store` | Publishes native code and owns the natives table (`Context.Natives`) native calls read. `Publish` installs a higher tier than what is published, atomically, and retires what it replaces; a stale result is freed instead. `Retire` clears the natives entry directly. A retired code stays findable (`Find`) for suspended activations until `Reclaim` frees it, which runs only once no interpreter is bracketed inside native code (`Enter`/`Leave`). |

Nesting: while a state is suspended, `Enter` starts below the suspended frames (`NSP` is the exit SP) and the inner return restores `NSP`. Async preemption cannot land in native code, so native loops MUST reach a Go safepoint through a bridge within bounded time (a back-edge budget, added with the lowering).

### Register allocation

`asm.Assembler.Build` allocates every virtual register when the architecture implements `asm.Frame` (flow of each row, written operands, allocatable registers per bank, spill and reload rows). Allocation is linear scan over live intervals from block liveness, with no interval splitting: a value that cannot keep one register for its whole life — it is live across a `FlowCall` row, or a bank runs out — is spilled everywhere, reloaded into a fresh two-row register before a row that reads it and parked after a row that writes it — one register per value and row, so tied operands (`MOVK`) update what they reloaded. So every virtual register has exactly one `asm.Loc` for its whole life, a register or a spill slot, which is what an exit map records. Calls clobber every allocatable register. Spill slot `n` is at `SP + 8n`; a prologue reserves the area with the `asm.Slots()` operand, which `Build` sizes. ARM64 allocates X0–X15, X19–X25, X27 and D0–D31.

Instruction-cache maintenance for published code is user-mode ARM64 (`DC CVAU`/`IC IVAU`) in `icache_arm64.s`; no cgo is involved.

## Lowering

`transform.Translate` translates a whole function from one entry — ip 0 or a loop header — and gives every `OpExec`, return, and completion the interpreter state at its own instruction, so which operations a backend lowers and which it bridges is the backend's decision alone.

```text
bytecode → transform.Translate → internal/ssa → SSA passes → compile.Lower → asm.Assembler.Build → native code
```

`compile.Lower(f, machine, fn, objects)` lowers the entry-0 translation of `fn`, resolving the functions it calls through `objects`: one virtual register per SSA value by static type, blocks in reverse postorder, block parameters written by a parallel move on each edge (an edge that moves gets a stub of its own; a cycle goes through one scratch register per bank and width). A loop header counts `Budget` down before its first operation that carries a state, so a safepoint there has a complete interpreter state; a header without one is `ErrUnsupported`. `compile.Machine` is the target: it emits rows for the operations and terminators `compile` walks; it does not own SSA control flow.

ARM64 activation ABI (`internal/jit/arm64`):

- X25 is the frame base, the address of the activation's VM slot 0, loaded from `Context.FB` by the prologue; parameters and locals are `[X25, #8i]`. X16 and X17 are scratch inside one row sequence; all three are reserved from allocation (`Assembler.Reserve`).
- The prologue pushes `Records[Depth] = {FB}`, stores its return address in `Record.PC`, increments `Depth`, and clears the locals after the parameters (the callee clears its own locals). Every return branches to one epilogue that decrements `Depth` and pops the frame.
- `OpReturn` first releases every slot whose kind can hold a reference (anything but `i32`/`f32`/`f64` after `Repr`) and clears it, as the interpreter's `RETURN` releases the frame it pops, then stores its results boxed from slot 0, where `RETURN` leaves them. A return over an owned operand that is not a result is `ErrUnsupported`. `OpComplete` stores its operands past the locals.
- A `CALL` of a constant function reference stores its arguments boxed at the callee's frame base (`X25 + 8*(slots + operands below)`), writes this activation's record `{SP, Exit}`, sets `Context.FB`, and `BLR`s `Context.Natives[address]`; afterwards it reloads `X25` from its record, releases the callee reference the call adopted, and loads the results from the callee's frame base. A callee without native code, `Depth` at `Context.Limit`, or a frame that would pass `Context.Top` takes an `ExitCall` instead. A call returning an `i64` is `ErrUnsupported`.
- An exit writes its id and trap to the `Context` and calls the exit stub (`X16` scratch). `ExitDeopt` never returns (a `BRK` follows); every other exit returns when the interpreter resumes it.

Exits (`jit.Exit`, one map per exit id, returned by `compile.Lower`):

| Kind | Taken at | The interpreter | Native code then |
|---|---|---|---|
| `ExitDeopt` | a failed check — division by zero, an `i64` outside the inline range at a store or return, an `i64` slot word that is no inline integer — or an `OpExit` | rebuilds `Frames` and continues threaded | never resumes |
| `ExitBridge` | an `OpExec` the machine does not lower | performs `Code` at the innermost frame, retaining the popped operands it does not `Adopts`, and writes its results to `Context.Results` raw by `Results` kind | loads the results and continues |
| `ExitSafepoint` | a loop header, when `Budget` is spent | runs its safepoint and refills `Budget` | continues into the header |
| `ExitRelease` | an `OpRelease`, return, or call dropping a last reference | releases `Release`; the map has no `Frames` | continues |
| `ExitCall` | a `CALL` that cannot run natively | calls `Callee` with the arguments at its frame base; the callee's frame owns them and the callee reference | loads the results from the callee's frame base and continues |

Every value a map names is live up to its exit (an `asm/arm64` `USE` row) and has one `asm.Loc` for its life: a register the saved register file holds (`State.Reg`) or a spill slot (`State.Slot`). `Frames` read from the operation's `OpState`, except an `ExitCall`'s: the caller's state after the call, its operand stack cut below the arguments and its IP past the `CALL`. That map is also the one `Record.Exit` names while a native callee runs, and its values stay live across the call, so they sit in spill slots at `Record.SP`. A return or completion carries the state of its own instruction, so a wide `i64` result deopts there and the interpreter promotes it. An `i64` slot load is the slot word, unboxed only by its `OpGuardKind`; a word used any other way is `ErrUnsupported`, as are the shape, bounds, and value guards and `OpSuspend`.

The rebuild MUST preserve threaded behavior as the semantic baseline. Native execution MUST resume through explicit runtime state rather than duplicate interpreter ownership.

### Tiers and the compile queue

`compile.Compile(u, m)` translates `u.Function` from `u.Module` at `u.Address`, verifies, runs `u.Tier`'s pass pipeline on a fresh `pass.Manager` — `jit.Baseline`: fold, dce; `jit.Optimized`: the O3 order (fold, promote, forward, cse, guard, hoist, dce) — verifies again, and lowers with `m` into a `jit.Code`. `compile.Queue` runs `Compile` on worker goroutines, each owning one machine, one unit per address in flight at a time: `Submit` refuses a second one until `Drain` collects the first; `Close` lets queued units finish and joins every worker. A `Job`'s `Code`, once handed to `Store.Publish`, is freed there if it turns out stale — a tier no higher than what is already published.

## Runtime

`interp.WithThreshold(n)` enables the JIT: `n >= 0` calls to one `*types.Function` before it compiles, only on `runtime.GOARCH == "arm64"`, and only without `WithHook`/`WithFuel` (their per-tick semantics need interpreter frames); `WithProfiler` is compatible. The `CALL` handler for a `*types.Function` — the dynamic and the fused `CONST_GET;CALL` path alike — calls into the interpreter's native runtime once, after its usual overflow/underflow checks and before it fills a frame; when native code runs the call to completion, no interpreter frame is pushed for it.

Compiles run asynchronously on `compile.Queue` (one Baseline unit per hot address); the interpreter drains finished jobs at its next call to that address and `Store.Publish`es a successful one. A failed compile marks the address permanently unsupported for the interpreter's life; it is never resubmitted.

Only `ExitSafepoint` and `ExitRelease` resume native code: a safepoint checks the active `Run` context for cancellation and refills `Context.Budget`; a release performs `Interpreter.Release` on the reference `Context.Read` names for the exiting activation. Every other exit — `ExitDeopt`, `ExitBridge`, `ExitCall`, and a safepoint that finds the context cancelled — deoptimizes (S2-P6b): `native.deopt` materializes every activation `Context.Depth` counts as an interpreter frame, outermost first. The innermost activation's map is the exit that brought native code out; an outer activation's map is its own call-site record, `store.Find(Records[k+1].PC).Exits[Records[k].Exit]` (that PC lies in the outer activation's own code, since it is the return address the inner activation's prologue recorded).

A materialized frame runs code compiled exact rather than `i.code[addr]`: `i.code[addr]` may be fused (`threader.Compile`'s `fusions` table), and a fusion leaves no handler at the IPs it absorbs, so a frame resuming mid-fusion would have nothing to run at its own IP. `native` builds and caches this exact code per address on first use. Every operand-stack and promoted-local value the map names is boxed by its kind (`i32`/`i1`/`i8`/`f32` from the low 32 bits, `f64` and `ref` from the raw word unchanged, a wide `i64` promoted through `boxI64`, exactly as a slot store would); an operand not already `Owned` is retained after boxing, and a promoted local's old word is released before the new one is stored, matching `LOCAL_SET`. Every native activation was entered by a `CALL` that adopted its callee reference, so every materialized frame releases on return except the outermost, whose ownership is the entering call site's own (a fused `CONST_GET;CALL` borrows its target; a dynamic `CALL` owns it) — the same asymmetry the entering hook's `release`/`advance` parameters already carry. An `ExitCall` additionally has the caller's arguments already boxed in memory at the callee's would-be frame base: the interpreter adopts them onto the operand stack, pushes the callee reference, and rewinds the innermost frame's `ip` from past the `CALL` to the `CALL` itself (one byte), so dispatch performs the call threaded next. An `ExitSafepoint` cancellation deoptimizes the same way and returns to threaded dispatch, whose next safepoint reports the context error as it always does — an error no guest handler can catch.

Because every unresumable exit deoptimizes, no guest code ever runs while a native activation is suspended: one native run at a time per interpreter, and `jit.Enter` never nests.

Metrics, only with `WithProfiler`: `vm_jit_compiles_total{tier,outcome=ok|unsupported|failed}`, `vm_jit_entries_total{tier}`, `vm_jit_exits_total{kind}`.

## Evidence

`jit-lessons.md` records the previous implementation's evidence and the design decisions derived from it.

## Related

- `architecture.md`
- `value-representation.md`
- `testing.md`
- `jit-lessons.md`
