# JIT Internals

Current status and planned ownership for the JIT rebuild.

`architecture.md` owns package boundaries; `coding-patterns.md` owns code design; `testing.md` owns test contracts; `jit-lessons.md` owns historical evidence.

## Status

The previous ARM64 JIT was removed (2026-09). Threaded execution and AOT optimization are the current implementation; a native tier is being rebuilt as one compiler pipeline.

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
| `jit.Context` | Embeds `asm.State` as its first field — the pinned register names both — and adds what native code writes before an exit: the `Trap` and the exit identifier, at `jit.OffsetTrap`/`jit.OffsetExit`. `Exit()` reads the identifier; what it names is the code publisher's to say. The interpreter writes the bases `Stack`, `Globals`, `RC`, `Natives` and the entry frame base `FB` before every `Enter`/`Resume`; `RC` moves when the heap grows, so native code loads it at each use. Native code maintains `Depth` and `Records` (one `Record` per activation) and counts `Budget` down. Every field has an `Offset*` constant. |
| `jit.Trap` | `TrapReturn` (the activation returned; nothing to resume), `TrapDeopt` (abandon; the interpreter rebuilds state), `TrapBridge` (suspend; the interpreter acts, then resumes). |
| `jit.Enter(code, ctx)` / `jit.Resume(ctx)` | `asm.Enter`/`asm.Resume` on the context's state, reporting the `Trap`: `TrapReturn` when the activation returned, else what native code wrote. |

Nesting: while a state is suspended, `Enter` starts below the suspended frames (`NSP` is the exit SP) and the inner return restores `NSP`. Async preemption cannot land in native code, so native loops MUST reach a Go safepoint through a bridge within bounded time (a back-edge budget, added with the lowering).

### Register allocation

`asm.Assembler.Build` allocates every virtual register when the architecture implements `asm.Frame` (flow of each row, written operands, allocatable registers per bank, spill and reload rows). Allocation is linear scan over live intervals from block liveness, with no interval splitting: a value that cannot keep one register for its whole life — it is live across a `FlowCall` row, or a bank runs out — is spilled everywhere, reloaded into a fresh two-row register before each read and parked after each write. So every virtual register has exactly one `asm.Loc` for its whole life, a register or a spill slot, which is what an exit map records. Calls clobber every allocatable register. Spill slot `n` is at `SP + 8n`; a prologue reserves the area with the `asm.Slots()` operand, which `Build` sizes. ARM64 allocates X0–X15, X19–X25, X27 and D0–D31.

Instruction-cache maintenance for published code is user-mode ARM64 (`DC CVAU`/`IC IVAU`) in `icache_arm64.s`; no cgo is involved.

## Lowering

`transform.Translate` translates a whole function from one entry — ip 0 or a loop header — and gives every `OpExec` the interpreter state at its own instruction, so which operations a backend lowers and which it bridges is the backend's decision alone.

```text
bytecode → transform.Translate → internal/ssa → SSA passes → compile.Lower → asm.Assembler.Build → native code
```

`compile.Lower(f, machine, params, locals)` lowers an entry-0 function: one virtual register per SSA value by static type, blocks in reverse postorder, block parameters written by a parallel move on each edge (an edge that moves gets a stub of its own; a cycle goes through one scratch register per bank and width). A loop header counts `Budget` down before its first operation that carries a state, so a safepoint there has a complete interpreter state; a header without one is `ErrUnsupported`. `compile.Machine` is the target: it emits rows and reports whether it lowers an operation; it never sees control flow.

ARM64 activation ABI (`internal/jit/arm64`):

- X25 is the frame base, the address of the activation's VM slot 0, loaded from `Context.FB` by the prologue; parameters and locals are `[X25, #8i]`. X16 and X17 are scratch inside one row sequence; all three are reserved from allocation (`Assembler.Reserve`).
- The prologue pushes `Records[Depth] = {FB}`, increments `Depth`, and clears the locals after the parameters (the callee clears its own locals). Every return branches to one epilogue that decrements `Depth` and pops the frame.
- `OpReturn` stores its results boxed from slot 0, where the interpreter's `RETURN` leaves them; `OpComplete` stores its operands past the locals.
- A failed check (division by zero, an `i64` outside the inline range at a store, a spent budget) branches to `BRK` until exits exist.

The rebuild MUST preserve threaded behavior as the semantic baseline. Native execution MUST resume through explicit runtime state rather than duplicate interpreter ownership.

## Evidence

`jit-lessons.md` records the previous implementation's evidence and the design decisions derived from it.

## Related

- `architecture.md`
- `value-representation.md`
- `testing.md`
- `jit-lessons.md`
