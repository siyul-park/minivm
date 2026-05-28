# JIT Internals

Threaded interpreter and ARM64 JIT contracts. Read before editing `interp/threaded.go`, `interp/jit.go`, `interp/jit_arm64.go`, or `asm/`.

## Checklist

Before editing: check opcode width in `instr/type.go`; preserve threaded/JIT parity; keep threaded fallback correct; read `profile.md` for ticks, thresholds, or hot-block choice; read `value-representation.md` for boxing/unboxing/native values; read `memory-model.md` for refs, heap objects, host functions, or ref-holding locals/globals.

After editing: add/update nearby table-driven tests, usually `interp/interp_test.go`; run `go test ./interp` for interpreter changes; run `go test ./asm/... ./interp` for ARM64/JIT/assembler changes.

## Execution Model

```text
program.Program
  -> threadedCompiler -> []func(*Interpreter)        always, portable fallback
  -> jitCompiler      -> []func(*Interpreter)|nil    lazy, ARM64 only
```

Both compilers read same bytecode. `i.code[addr][ip]` remains fallback; JIT replaces only emitted entry IPs. Rejected or failed JIT segments must fall back cleanly to threaded code.

## Threaded Handlers

`threaded` is `[256]func(*threadedCompiler) func(*Interpreter)`, populated in `threaded.go:init()`.

Handler shape:

```go
instr.OPCODE: func(c *threadedCompiler) func(i *Interpreter) {
    offset := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
    width := 3
    c.ip += width

    return func(i *Interpreter) {
        f := &i.frames[i.fp-1]
        _ = offset
        f.ip += width
    }
},
```

Rules:

- compile time: decode operands, capture locals, advance `c.ip` before return
- runtime: advance `f.ip` by exact instruction width
- closure must not capture `c` or read `c.code`
- refs entering stack need `i.retain(addr)`; consumed refs need `i.release(addr)`
- closure errors panic; `Interpreter.Run` recovers and annotates `at=<ip>`

`NOP`: normal threaded compile emits one closure per NOP byte, but first closure skips whole consecutive NOP run. Dead-code padding costs one dispatch. `WithTick(1)` disables run skipping and preserves exact byte boundaries. JIT `NOP` advances `s.ip`, returns `true`, emits nothing, and counts toward segment length.

## JIT Handlers

`jit` is `[256]func(*jitSeg) bool`, populated in `jit_arm64.go`.

| Return | Meaning |
|---|---|
| `true` | current opcode lowered and `s.ip` advanced by its exact width |
| `false` | current opcode rejected; segment ends before it |

Handler shape:

```go
jit[instr.OPCODE] = func(s *jitSeg) bool {
    r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
    if !ok {
        return false
    }
    r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
    s.assembler.Emit(arm64.ADD(r1, r0, r0))
    s.Push(r1)
    s.ip++
    return true
}
```

Rules:

- type/width mismatch returns `false`; never coerce
- **validate-first contract**: a handler that returns `false` must leave `jitSeg` (ip, stack, params, facts) and the assembler unchanged. The segment loop does not snapshot/restore — rejection on a partly-mutated state will corrupt the surrounding compile.
- already lowered prefix code remains eligible when a following opcode rejects
- branch terminators return `true`; the basic-block/trace boundary ends compilation and skips `jitEpilogue`
- non-branch segments use `jitEpilogue`
- `jitPrologue` seeds next-IP scratch with `s.end`; `jitEpilogue` reloads final `s.end` and emits `RET`

## Scratch And Next IP

ARM64 JIT reserves `arch.Scratch = X10-X15` as metadata channels outside normal params/returns.

| Scratch | Purpose |
|---|---|
| `rStack` | `&i.stack[f.bp]` input |
| `rHeap` | heap pointer input |
| `rGlobals` | globals pointer input |
| `rNext` | next interpreter IP output |

`jitCompiler.closure()` writes scratch inputs, calls native code, receives typed `asm.Value` results, reads JIT-owned `rNext`, restores stack values, then sets `i.frames[fp-1].ip`.

## Branches And Globals

Branches (`BR`, `BR_IF`, `BR_TABLE`) terminate traces. Branch offsets are signed i16 relative to instruction end. Each lowered branch emits a deferred edge label and a local fallback `RET` stub. After all objects are compiled once, compatible targets alias the edge to the target entry; missing or incompatible targets alias it to the fallback stub.

Branch handlers must not fall through `jitEpilogue`, because that would overwrite branch-selected `rNext`.

Mutable globals have no declared runtime kind. `GLOBAL_SET` / `GLOBAL_TEE` infer kind from source register and store it in same-segment `s.facts`. `GLOBAL_GET` compiles only after same-segment store proves kind. Never specialize `GLOBAL_GET` from current global value; dynamic kind changes would need deopt stack reconstruction, which current JIT ABI lacks.

## Segment Selection

`jitCompiler.Compile(code)` builds basic blocks, scores each with `profile.Range(addr,start,end)`, selects hot blocks plus direct CFG successors, and builds JIT-only fallthrough traces. `analysis.BasicBlock` remains the CFG source of truth.

Emit rules:

- completed segment emits when `count >= c.cutoff` (default `8`) and segment range has a profile sample
- direct-successor entry segments may emit with one compilable instruction so linked branches can enter them
- truncated or branch-terminated segments emit only when they meet same cutoff and range from start to last compiled IP is sampled
- otherwise `assembler.Abort()` discards segment state
- adjacent eligible blocks joined only by natural fallthrough may emit as one object
- if the first opcode at an internal entry rejects, discard that merged attempt and compile the blocks separately
- each bytecode entry IP is installed at most once per JIT attempt
- JIT makes one function-level compilation attempt; no later tier-up/retry

```text
block [A B X C D E F]  X unsupported
-> [A B]        count=2  abort
-> [C D E F]    count=4  abort unless forced by a linked branch or lowered cutoff
```

## Single-Compile Linking

Each trace is compiled once into the real executable buffer.

1. Compile traces and collect primary/internal entry signatures and deferred branch edges.
2. Alias compatible edges to compiled entry labels; alias all other edges to their local fallback stubs.
3. Link relocations and install one interpreter-callable closure per accepted entry IP.

Natural fallthrough trace `A+B` records an internal callable entry at `B`. A cold or uncompiled additional predecessor does not prevent installing that entry, so threaded dispatch at `B` can still enter native code. If a compiled incoming edge has a mismatched type/width signature, the internal entry is not installed and that edge uses its fallback stub; merged fallthrough code remains valid for `A`.

## Assembler And JIT Segment

Two-layer IR emission:

**`asm.Assembler`** (low-level): allocate VRegs, emit instructions, declare ABI boundaries. No VM stack semantics.

| Method | Use |
|---|---|
| `NewVReg(type,width)` | allocate virtual register |
| `Emit(inst)` | append instruction |
| `NewLabel()` / `Bind(id)` | create/place branch targets |
| `Entry(label, live)` | mark internal callable entry and its ABI inputs |
| `Alias(label, target)` | resolve deferred edges at link time |
| `Scratch()` | allocate reserved metadata PReg |
| `Pin(vreg, preg)` | bind VReg to physical register (ABI slots) |
| `Site(idx, live)` | declare ABI boundary at instruction idx with live values |
| `Compile()` | allocate physical regs, encode, append buffer, return `RelocObject` |
| `Link(objects)` | patch cross-segment relocs, return native callers |
| `CallerAt(object, label)` | build caller for an internal entry |
| `Abort()` / `Reset()` | discard segment / reset function assembler state |

**`jitSeg`** (high-level): track VM stack shape, manage operands and results.

| Method | Use |
|---|---|
| `Take(type,width)` | pop matching stack value or create param when stack empty |
| `Top(i)` | inspect i-th value from top |
| `Push(reg)` / `Pop()` | push or unchecked pop |

`jitSeg.assembler` delegates IR emission to `Assembler`; `jitSeg.stack` and `jitSeg.params` track VM stack shape. `Site()` called at function entry and return points to expose ABI signatures.

`asm.Signature.Params` is the primary entry's input register layout, used to construct a `Caller`. `asm.RelocObject.Entries map[int]Entry{Offset, Params}` records internal callable entries keyed by assembler label. `asm.Signature.Returns` stores return signatures keyed by return-site instruction index. `asm.Caller` only executes a compiled chunk; signature inspection belongs to the `Signature` returned by `Compile()`. Negative labels are reserved and invalid for `Entry`.

Pipeline:

```text
per segment: jitSeg.Take/Push/Pop -> assembler.Emit() -> IR list
Compile(): IR list -> Assembler.Compile() -> RelocObject
Resolve: deferred edges -> Assembler.Alias()
Link(): []*RelocObject -> Assembler.Link()/CallerAt() -> callers
```

Intra-segment labels resolve in `Compile()`. Cross-segment labels become relocations patched by `Link()`.

## Executable Buffer

`asm.Buffer` wraps mmap memory and must alternate writable/executable states.

```text
NewBuffer(size) -> writable
Compile(): Unseal() -> Append(code) -> Seal()
Link():    Unseal() -> patch reloc bytes -> Seal()
Free():    munmap
```

Apple Silicon enforces W^X. Wrong `Unseal -> Append/Patch -> Seal -> Call` order can crash with `SIGBUS` or `SIGSEGV`. `Seal()` also flushes instruction cache on Darwin/ARM64; without that, reused executable buffer can intermittently execute stale bytes and crash with `SIGILL`.

## ARM64 ABI

`asm/arm64` follows AAPCS64: integer args/returns use `X0-X7`; float args/returns use `D0-D7` / `S0-S7`; metadata scratch uses `X10-X14`; trampoline bookkeeping reserves `X8`, `X9`, and header register `X15`.

Trampoline `argv` layout:

```text
argv[0]            header: nParams, nReturns, nReserved, type/width masks
argv[1..reserved]  scratch inputs/outputs
argv[reserved+1..] params in / returns out
```

`abi_arm64.s` marshals args from `argv`, copies `argv[0]` into `X15`, loads reserved `X10-X14`, calls native chunk via `BL`, copies `X15` back to `argv[0]`, then writes scratch outputs and return values back to `argv` using updated header.
