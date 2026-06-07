# JIT Internals

Contracts for native JIT files in `interp/` and their interaction with `asm/`. Read before editing `interp/jit.go`, `interp/jit_arm64.go`, or asm callable ABI code.

## Checklist

Before editing: check opcode width in `instr/type.go`; preserve threaded/JIT parity; keep threaded fallback correct; read `profile.md` for ticks, thresholds, and hot-block choice; read `value-representation.md` for boxing/unboxing; read `memory-model.md` for refs and heap objects.

After editing: add or update tests in `asm/assembler_test.go` for callable ABI changes and `interp/interp_test.go` for native lowering/wiring changes; run `go test ./asm/... ./interp/...`.

## Execution Model

```text
program.Program
  -> threadedCompiler -> []func(*Interpreter)    always, portable fallback
  -> jitCompiler      -> *jitModule              lazy, ARM64-gated
```

Both compilers read the same bytecode. `i.code[addr][ip]` is the primary dispatch table. JIT replaces selected entries after compilation. Rejected segments fall back cleanly to threaded code.

## jitCompiler

`jitCompiler` is private to `interp` and lives in `jit.go`. `Compile(i, addr, fn)` reads the current interpreter state directly and attempts two strategies in order:

1. **Whole-function Entry** (`compiler.whole`): all opcodes lower without rejection. On success, an `asm.Callable` is stored in `jitModule.entry`. The `interp` side wraps `entry` in a Go closure that handles frame teardown after the native call returns.

2. **Segment compilation** (`compiler.segments`): processes hot IPs from the interpreter profiler. Each hot IP seeds a segment; successors are queued when forced by `BR` or by a rejected op with a safe structural successor. Each accepted segment is stored in `jitModule.segments[ip]`.

Compilation is one-shot per function. There is no re-tier or deoptimization.

## Emitter (`lowerer`)

`jit.go` is architecture-neutral. It depends on the `lowerer` interface — the
architecture-specific opcode emitter — and never names a concrete backend.
`jitCompiler` holds a `lowerer` field, set by the arch-specific
`newJITCompiler`; every native-code emission in the pipeline goes through it.

`jit_arm64.go` provides the only concrete `lowerer` today (`arm64JIT`), wired
on arm64. On other architectures `jit_stub.go` returns a nil `jitCompiler`
(JIT unavailable), so no `lowerer` is constructed. A second ISA is added by
implementing `lowerer` in a new arch file and wiring it in that file's
`newJITCompiler` — `jit.go` needs no change.

The interface is four operations:

- `prologue` loads declared live-ins from the VM stack into the shadow stack's initial VRegs.
- `enter` emits the function entry sequence (frame/link save) for a whole-function target reached as its own callable.
- `lower` dispatches one opcode. It returns `false` to reject and fall back to threaded execution.
- `exitIP` materializes shadow stack values into `i.stack`, writes `scratchSP` and `scratchNext`, and emits `RET`.

## jitContext

```go
type jitContext struct {
    assembler *asm.Assembler
    code      []byte
    constants []types.Boxed
    globals   []types.Boxed
    locals    []types.Kind
    scratch   []asm.PReg
    stack     []asm.VReg   // shadow stack of live boxed values
    inputs    []asm.VReg   // live-in VRegs loaded from VM stack
    ip        int
    start     int
    end       int
    successor int
    whole     bool
    stop      bool
    closed    bool
}
```

`stack` is the segment-local shadow stack of boxed VRegs. `inputs` are the live-in VRegs loaded from `i.stack` at segment entry. Whole-function Entry compilation must finish with no `inputs`; Entry receives function params through VM stack/local loads. `whole` is true only during whole-function Entry compilation; `RETURN` must check this flag.

## RETURN Lowering Contract

`RETURN` lowers only when `c.whole == true`. In segment mode it returns `false`, forcing threaded execution to handle frame teardown via the Go-side `entry()` wrapper. Violating this causes double frame teardown.

## Segment ABI

Scratch registers carry VM context through the single `argv []uint64` callable ABI. `Callable.Call(argv)` loads `argv[0..4]` into X10..X14, calls native code, stores X10..X14 back into the same slots, and reports caller-side argument errors.

| Constant | ARM64 | Purpose |
| --- | --- | --- |
| `scratchStack` (0) | X10 | `&i.stack[0]` input |
| `scratchGlobals` (1) | X11 | `&i.globals[0]` input |
| `scratchBP` (2) | X12 | current frame `bp` input |
| `scratchSP` (3) | X13 | interpreter `sp` in/out |
| `scratchNext` (4) | X14 | next interpreter IP output |

Native code loads stack inputs from `scratchStack`/`scratchSP`, keeps operands in registers, and writes stack results back only on exit or fallback. The `interp` adapter does not marshal params or returns; it calls `Callable.Call(argv)` and copies back `scratchSP`/`scratchNext`.

Guard fallbacks set the high bit of `scratchNext`. The segment wrapper clears that bit, restores `fr.ip`, and runs the original threaded handler for that opcode once, avoiding native re-entry loops at the segment start.

`i64.add` lowers only the boxable fast path. If the result is outside the 49-bit boxed i64 range, native materializes the pre-op stack and uses the guard fallback path so threaded execution can allocate the heap-spilled `types.I64`.

JIT i64 ops require inline 49-bit operands — they read the value lane directly and skip the retain/release the threaded path does. An i64 local or global can still hold a heap-promoted `KindRef` at runtime (typed i64, but out of range). So every i64 slot **load** (`LOCAL_GET`, `GLOBAL_GET`) and **store** (`LOCAL_SET`, `GLOBAL_SET`, `LOCAL_TEE`, `GLOBAL_TEE`) emits a `requireInlineI64` tag check: a load tests the loaded value, a store tests the old slot value (the one a `*_SET` would `releaseBox`); if it is not the inline `KindI64` tag, native takes the guard fallback at the current IP so the interpreter — which owns heap i64 and refcounting — handles it. The check is emitted only for slots statically typed i64.

## CALL Boundaries

Direct `CONST_GET function; CALL` sites do not lower to native `BL` today.
They stay threaded so frame limits, stack bounds, closure/host dispatch, ref
retain/release, and observer-visible frame metadata remain interpreter-owned.
The complete-JIT path rejects functions containing direct calls; supported
prefix/suffix segments around the call may still be installed.

When a function is not complete-JIT eligible, supported prefix/suffix segments
are still installed independently, including an `ip=0` partial entry.

Complete-JIT rejects any reachable native guard fallback. Guard fallback is
still valid in segment mode because the segment wrapper restores frame metadata
and runs the original threaded handler for the fallback IP.

## Branches

`BR` and `BR_IF` terminate traces. Branch offsets are signed int16 relative to instruction end. `BR` records its target as a forced successor and exits with the successor IP in `scratchNext`. `BR_IF` emits both exit paths inline; it marks the context closed so `Exit` is not appended.

`BR` targets are forced successors unless the target is a `NOP` or outside the function. Rejected ops force only safe structural successors (`NOP` or `BR` following the rejected opcode). A rejected direct `CONST_GET function; CALL` queues the post-call successor so supported work after the call remains eligible for JIT.

Whole-function block mode carries shadow-stack values across block boundaries when every reachable predecessor agrees on the same VReg stack shape. If predecessors need a merge/phi, block mode rejects and falls back to segment compilation.

## Globals

`GLOBAL_GET`/`GLOBAL_SET`/`GLOBAL_TEE` lower only for in-range globals whose
compile-time value is non-ref and whose slot offset fits the ARM64 unsigned
LDR/STR immediate. Native code also checks the runtime old/value slot and the
stored value; if either is a ref, it takes the guard fallback so retain/release
ownership stays in the interpreter.

## Phase A Opcode Coverage

Fully lowered on ARM64:

- Stack: `NOP`, `DROP`, `DUP`, `SWAP`, `SELECT`
- Const: `I32_CONST`, `I64_CONST`, `F32_CONST`, `F64_CONST`, `CONST_GET`
- Arithmetic: I32 add/sub/mul/div/rem/bitwise/shifts/comparisons/eqz; I64 add/sub/mul/div/rem/shifts/comparisons/eqz; F32/F64 add/sub/mul/div/comparisons
- Conversions: `I32→I64`, `I64→I32`, `I32/I64→F32/F64`, `F32/F64→I32/I64`, `F32↔F64`
- Branches: `BR`, `BR_IF`, `BR_TABLE`
- Locals + globals: `LOCAL_GET/SET/TEE`, `GLOBAL_GET/SET/TEE`
- `RETURN` (whole-function Entry path only)
- `UNREACHABLE`

## Phase B Opcode Coverage

Fast paths implemented on ARM64:

- `REF_NULL`: push `BoxedNull` constant.
- `REF_IS_NULL`: pop ref, compare full boxed word against `BoxedNull`, push BoxI32 result.
- `REF_EQ`: pop two boxed values, compare at 64-bit level, push BoxI32 result.
- `REF_NE`: pop two boxed values, compare at 64-bit level, push BoxI32 result.

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
asm.Link(buf, arch, []*Code, resolve) → []Linked
  patches external relocations, writes to Buffer, flushes icache
```

`asm.Assembler` is single-shot: build it, link it, discard it. `Code` is immutable after `Build`. The W^X discipline is maintained by `asm.Buffer`: code is installed during `Link` and the buffer is sealed (executable, non-writable) afterward.

## Executable Buffer

`asm.Buffer` is a W^X mmap region for native code.

Apple Silicon (Darwin/ARM64) enforces W^X at the hardware level. `asm/icache_darwin_arm64.go` flushes the instruction cache after each `Link`. Wrong seal/unseal ordering causes `SIGBUS` or `SIGILL`.

## ARM64 ABI Summary

AAPCS64: native VM callables use only scratch `X10–X14` (`scratchStack` through `scratchNext`) across the Go trampoline.

The `abi_arm64.s` trampoline loads `argv[0..4]` into X10..X14, calls the native chunk, and stores X10..X14 back into `argv[0..4]`.
