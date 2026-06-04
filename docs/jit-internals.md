# JIT Internals

Contracts for `jit/`, `jit/arm64/`, and their interaction with `interp/`. Read before editing any of these packages.

## Checklist

Before editing: check opcode width in `instr/type.go`; preserve threaded/JIT parity; keep threaded fallback correct; read `profile.md` for ticks, thresholds, and hot-block choice; read `value-representation.md` for boxing/unboxing; read `memory-model.md` for refs and heap objects.

After editing: add or update tests in `jit/arm64/lowerer_test.go` for lowerer changes, `interp/interp_test.go` for interp wiring changes; run `go test ./jit/... ./interp/...`.

## Execution Model

```text
program.Program
  -> threadedCompiler -> []func(*Interpreter)    always, portable fallback
  -> jit.Compiler     -> *jit.Module             lazy, arch-gated
```

Both compilers read the same bytecode. `i.code[addr][ip]` is the primary dispatch table. JIT replaces selected entries after compilation. Rejected segments fall back cleanly to threaded code.

## jit.Compiler

`jit.Compiler` is the entry point. `Compiler.Compile(fn, addr, snap)` attempts two strategies in order:

1. **Whole-function Entry** (`compiler.whole`): all opcodes lower without rejection. On success, an `asm.Callable` is stored in `Module.Entry` and the function's slot is updated via `Slots.Set`. The `interp` side wraps `Entry` in a Go closure (`entry()`) that handles frame teardown after the native call returns.

2. **Segment compilation** (`compiler.segments`): processes hot IPs from `snap.Hot`. Each hot IP seeds a segment; successors are queued when forced by `BR` or by a rejected op with a safe structural successor. Each accepted segment is stored in `Module.Segments[ip]`.

Compilation is one-shot per function. There is no re-tier or deoptimization.

## jit.Lowerer Interface

```go
type Lowerer interface {
    Arch()     asm.Arch
    Prologue(c *Context, fn *types.Function)
    Epilogue(c *Context)
    Exit(c *Context, nextIP int)
    Lower(c *Context, op instr.Opcode) bool
}
```

- `Arch` returns the backend's `asm.Arch`.
- `Prologue` binds live-in ABI args to the shadow stack's initial VRegs. Called once per segment or whole-function attempt.
- `Epilogue` is currently a no-op; reserved for whole-function teardown (frame pop) when needed.
- `Exit` pins shadow stack values to ABI return registers, loads `nextIP` into `ScratchNext`, and emits `RET`. Called at the end of every non-closed segment.
- `Lower` dispatches one opcode. Returns `false` (without mutating `Context`) to reject. On success the driver advances `c.IP` by the opcode width; handlers must leave `c.IP` alone unless they also set `c.Stop` (e.g. branch/RETURN), in which case they own the resume position.

Backends register via `jit.Register(arch string, Lowerer)` from an `init()` in a blank-importable package.

## jit.Context

```go
type Context struct {
    Assembler *asm.Assembler
    Stack     []asm.VReg   // shadow stack of live boxed values
    Inputs    []asm.VReg   // ABI input VRegs (pinned to Arg slots)
    Args      []asm.PReg   // resolved ABI args (for signature)
    Returns   []asm.PReg   // resolved ABI returns (for signature)
    Scratch   []asm.PReg   // scratch physical registers
    Snap      Snapshot
    IP        int
    Start     int
    End       int
    Target    int
    Successor int
    Whole     bool
    Stop      bool
    Closed    bool
    Slots     *Slots
}
```

`Stack` is the segment-local shadow stack of boxed VRegs. `Inputs` are the live-in VRegs for a partial segment's declared ABI args. Whole-function Entry compilation must finish with no `Inputs`; Entry receives function params through VM stack scratch/local loads, not `asm.Signature.Args`. `Target` carries the static call target address set by `CONST_GET` of a Ref constant; `Lower` clears it before every opcode except `CALL`. `Whole` is true only during whole-function Entry compilation; `RETURN` must check this flag.

## RETURN Lowering Contract

`RETURN` lowers only when `c.Whole == true`. In segment mode it returns `false`, forcing threaded execution to handle frame teardown via the Go-side `entry()` wrapper. Violating this causes double frame teardown.

## Segment ABI

Scratch registers carry VM context. Only `ScratchCount` slots are declared in the callable ABI signature.

| Constant | ARM64 | Purpose |
| --- | --- | --- |
| `ScratchStack` (0) | X10 | `&i.stack[0]` input |
| `ScratchGlobals` (1) | X11 | `&i.globals[0]` input |
| `ScratchBP` (2) | X12 | current frame `bp` input |
| `ScratchNext` (3) | X13 | next interpreter IP output |

Stack values are passed as `asm.Signature.Args` (bottom-to-top) and returned as `asm.Signature.Returns`. The `interp` adapter pops declared inputs, calls `Callable.Call`, appends returned values, then reads `ScratchNext` to advance the frame IP.

## Direct-BL CALL

When `CONST_GET` of a Ref constant immediately precedes `CALL`, and the target is whole-function compiled or is the current function being compiled, the caller-JIT emits a direct native call. Self-recursive calls branch to the same Code's Entry label with `BL`; other targets use the slot table:

```asm
LDR  Xt, [slot_addr]   ; load entry pointer from slot
BLR  Xt                ; call callee Entry natively
```

`slot_addr` is a stable pointer from `Slots.For(addr)`, baked into the code at link time via a relocation. Initially the slot points at a fallback stub (sets `ScratchNext=0`, returns). When the callee's Entry is compiled, `Slots.Set(addr, Entry)` atomically updates the pointer.

BL/BLR clobbers X30 (LR). The lowerer saves LR on the system stack before native calls and restores it after.

Only functions with numeric-only signatures (no KindRef params/returns) are eligible for direct-BL. Closures and host functions always fall back to threaded. Self-recursive whole-function entries avoid the `LDI+LDR` slot indirection on recursive calls.

In whole-function Entry mode, values below the callee arguments on the caller shadow stack are spilled to scratch slots above the caller's locals before `BLR`, then reloaded after BP is restored. This preserves cross-call liveness for patterns such as `fib(n-1) + fib(n-2)`. Partial segments still reject survivor values across `CALL`.

## Branches

`BR` and `BR_IF` terminate traces. Branch offsets are signed int16 relative to instruction end. `BR` records its target as a forced successor and exits with the successor IP in `ScratchNext`. `BR_IF` emits both exit paths inline; it marks the context closed so `Exit` is not appended.

`BR` targets are forced successors unless the target is a `NOP` or outside the function. Rejected ops force only safe structural successors (`NOP` or `BR` following the rejected opcode). Rejected `CALL` does not force successors.

Whole-function block mode carries shadow-stack values across block boundaries when every reachable predecessor agrees on the same VReg stack shape. If predecessors need a merge/phi, block mode rejects and falls back to segment compilation.

## Globals

`GLOBAL_GET`/`GLOBAL_SET`/`GLOBAL_TEE` lower only for in-range non-ref globals whose slot offset fits the ARM64 unsigned LDR/STR immediate. Ref globals fall back to threaded code so retain/release ownership stays in the interpreter.

## Phase A Opcode Coverage

Fully lowered on ARM64:

- Stack: `NOP`, `DROP`, `DUP`, `SWAP`, `SELECT`
- Const: `I32_CONST`, `I64_CONST`, `F32_CONST`, `F64_CONST`, `CONST_GET`
- Arithmetic: all I32/I64/F32/F64 add/sub/mul/div, I32/I64 rem, bitwise, shifts, comparisons, eqz
- Conversions: `I32↔I64`, `I32↔F32/F64`, `F32↔F64`, `F→I`
- Branches: `BR`, `BR_IF`, `BR_TABLE`
- Locals + globals: `LOCAL_GET/SET/TEE`, `GLOBAL_GET/SET/TEE`
- `CALL` (direct-BL when target is jitted; threaded otherwise)
- `RETURN` (whole-function Entry path only)
- `UNREACHABLE`

## Phase B Opcode Coverage

Fast paths implemented on ARM64:

- `REF_NULL`: push `BoxedNull` constant.
- `REF_IS_NULL`: pop ref, compare full boxed word against `BoxedNull`, push BoxI32 result.
- `REF_EQ`: pop two boxed values, compare at 64-bit level, push BoxI32 result.

All other `REF_*` and `UPVAL_*` fall back to threaded.

## Value Boxing

Boxed values follow NaN-boxing (see `value-representation.md`). JIT maintains boxed representation on the shadow stack; no unboxing/reboxing unless a typed lane extraction is needed for arithmetic. Boxing constants used by the lowerer:

| Type | Tag bits [63:49] |
|---|---|
| F64 | raw IEEE bits (not NaN-boxed) |
| F32 | `0x7FF2_...` |
| I64 | `0x7FF4_...` |
| I32 | `0x7FF6_...` |
| Ref | `0x7FF8_...` |

`BoxedNull = BoxRef(0) = 0x7FF8_0000_0000_0000`.

## Segment Selection

Emit rules:

- accepted segment emits when lowered opcode count ≥ `c.cutoff` (default 8)
- hot profile IPs are initial compile entries; no profile falls back to entry IP 0
- hot IPs reachable inside an accepted trace become internal entries on the same native code object when their live stack maps to compatible ABI args
- `BR` targets are forced successors unless the target is `NOP` or out of range
- rejected non-`CALL` ops force successors only when next opcode is `NOP` or `BR`
- cold successors that are discovered but not forced count as skips
- each bytecode entry IP is installed at most once per JIT attempt

## Assembler Pipeline

```text
asm.New(arch)             allocate Assembler
  Reg(type, width)        allocate VReg
  Pin(vreg, preg)         bind to physical register
  Emit(instruction)       append to IR list
  Label() / Bind(label)   create/place branch targets
  Build(sig) → *Code      regalloc + encode + intra-Code reloc resolution
asm.LinkAll(buf, arch, []*Code, resolve) → []Linked
  patches external relocations, writes to Buffer, flushes icache
```

`asm.Assembler` is single-shot: build it, link it, discard it. `Code` is immutable after `Build`. The W^X discipline is maintained by `asm.Buffer`: code is installed during `LinkAll` and the buffer is sealed (executable, non-writable) afterward.

## Executable Buffer and Data Region

`asm.Buffer` is a W^X mmap region for native code. `asm.Data` is a plain writable mmap region for runtime-patched pointers (slot table). Slots in `asm.Data` are updated atomically; no re-link is needed when a callee compiles.

Apple Silicon (Darwin/ARM64) enforces W^X at the hardware level. `asm/icache_darwin_arm64.go` flushes the instruction cache after each `LinkAll`. Wrong seal/unseal ordering causes `SIGBUS` or `SIGILL`.

## ARM64 ABI Summary

AAPCS64: integer args/returns `X0–X7`, float args/returns `D0–D7`/`S0–S7`. Scratch `X10–X13` (ScratchStack–ScratchNext). Trampoline temporaries use `X8`, `X9`, `X15`.

The `abi_arm64.s` trampoline marshals `asm.Value` args into native registers, calls the native chunk, and collects return values back into `[]asm.Value`.
