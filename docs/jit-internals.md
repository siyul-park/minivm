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
| Machine encoding, executable memory, runtime contract | `internal/asm/` |
| ARM64 encoding | `internal/asm/arm64/` |
| Profiling | `prof/` |

## Runtime contract

`internal/asm` owns how native code is entered and how it reports back. Native code runs on a Go-allocated native stack owned by an `asm.Context`, never on the goroutine stack, so a native activation can be suspended, worked on from Go, and resumed.

| Symbol | Contract |
|---|---|
| `Context` | Shared state: saved Go registers, `NSP` (native SP), `PC`, `Trap`, `Exit`, `Regs`/`Fregs` (the register file at the last exit), the native stack. Native code writes only scalar fields; no Go pointer is ever stored on the native stack. |
| `Enter(code, ctx)` | Switches to the native stack at `NSP` and calls `code`. Returns `TrapReturn` when the activation returns; any other trap leaves it suspended. |
| `Resume(ctx)` | Continues the suspended activation: restores `Regs`/`Fregs`, `SP = NSP`, `LR = PC`, and returns into it. |
| exit | Native code writes `Exit` and `Trap`, then `BLR`s the stub at `OffsetStub`. **An exit is a call**: the stub saves the register file, `LR → PC`, `SP → NSP`, and returns to Go; `Resume` is that call returning, so code that exits keeps its own LR in a frame like around any call. |
| `arm64.Ctx` (X26) | Holds the `*Context` while native code runs; pinned, never written by native code. X16/X17 are scratch for the exit protocol; X18/X28 are never touched. |

Nesting: while a context is suspended, `Enter` starts below the suspended frames (`NSP` is the exit SP) and the inner return restores `NSP`. Async preemption cannot land in native code, so native loops MUST reach a Go safepoint through a bridge within bounded time (a back-edge budget, added with the lowering).

### Register allocation

`asm.Assembler.Build` allocates every virtual register when the architecture implements `asm.Frame` (flow of each row, written operands, allocatable registers per bank, spill and reload rows). Allocation is linear scan over live intervals from block liveness, with no interval splitting: a value that cannot keep one register for its whole life — it is live across a `FlowCall` row, or a bank runs out — is spilled everywhere, reloaded into a fresh two-row register before each read and parked after each write. So every virtual register has exactly one `asm.Loc` for its whole life, a register or a spill slot, which is what an exit map records. Calls clobber every allocatable register. Spill slot `n` is at `SP + 8n`; a prologue reserves the area with the `asm.Slots()` operand, which `Build` sizes. ARM64 allocates X0–X15, X19–X25, X27 and D0–D31.

Instruction-cache maintenance for published code is user-mode ARM64 (`DC CVAU`/`IC IVAU`) in `icache_arm64.s`; no cgo is involved.

## Planned rebuild

The rebuild targets one compiler pipeline:

```text
bytecode
  ↓
transform.Translate
  ↓
internal/ssa
  ↓
SSA passes
  ↓
machine lowering
  ↓
internal/asm
  ↓
ARM64 native code
```

The runtime contract above is current. The target-neutral compiler under a JIT implementation package and the target lowering under a native architecture package are planned, not current.

The rebuild MUST preserve threaded behavior as the semantic baseline. Native execution MUST resume through explicit runtime state rather than duplicate interpreter ownership.

## Evidence

`jit-lessons.md` records the previous implementation's evidence and the design decisions derived from it.

## Related

- `architecture.md`
- `value-representation.md`
- `testing.md`
- `jit-lessons.md`
