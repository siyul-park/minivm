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

## Segment ABI

ARM64 JIT uses the normal `asm.Callable` ABI for stack values: segment
inputs consumed from the interpreter stack are declared as
`asm.Signature.Args`, and segment results are declared as
`asm.Signature.Returns`. Scratch is reserved for VM context and control
metadata only.

The interpreter adapter pops the declared stack inputs, passes them as
`asm.Value` args, appends returned `asm.Value` payloads to the VM stack,
then reads `nextIP` from scratch and updates the current frame.
Input args are ordered bottom-to-top, matching the interpreter stack slice.
Compilation first plans a segment to discover required stack inputs, then
emits the real segment with those inputs live from entry.

`jit.Scratch*` defines the shared slot layout. The compiler declares
only the first `jit.ScratchCount` architectural scratch registers in
the callable signature.

| Scratch | Purpose |
|---|---|
| `ScratchStack` (`X10`) | `&i.stack[0]` input, used for local loads/stores |
| `ScratchGlobals` (`X11`) | `&i.globals[0]` input |
| `ScratchBP` (`X12`) | current frame `bp` input |
| `ScratchNext` (`X13`) | next interpreter IP output |

## Branches And Globals

`BR` and `BR_IF` terminate traces. Branch offsets are signed i16 relative
to instruction end. `BR` records its target as a forced successor and lets
the common segment exit write that target to `ScratchNext`. `BR_IF` emits
both native exit paths inline: the not-taken path writes the fallthrough IP,
and the taken path writes the branch target.

Branch handlers that emit their own `RET` paths mark the context closed, so
the compiler does not append the common exit and overwrite branch-selected
`ScratchNext`.

Mutable globals have no declared runtime kind. `GLOBAL_SET` / `GLOBAL_TEE` infer kind from source register and store it in same-segment `s.facts`. `GLOBAL_GET` compiles only after same-segment store proves kind. Never specialize `GLOBAL_GET` from current global value; dynamic kind changes would need deopt stack reconstruction, which current JIT ABI lacks.

## Segment Selection

`jit.Compiler.Compile(fn)` receives hot IPs from the interpreter profile via
`jit.Snapshot.Hot`, compiles each hot entry at most once, and queues selected
successors discovered while lowering.

Emit rules:

- completed segment emits when lowered opcode count is `>= c.cutoff`
- hot profile IPs are initial compile entries; no profile falls back to entry `0`
- hot IPs reached inside an already accepted trace are emitted as internal
  entries on the same native code object when their live stack can be mapped
  to ABI args without pin conflicts
- `BR` targets are forced successors unless the target is a `NOP` or outside the function
- rejected `CALL` boundaries do not force cold successors
- other rejected opcodes force only safe structural successors (`NOP` or `BR`)
- cold successors that are discovered but not forced are counted as skips
- each bytecode entry IP is installed at most once per JIT attempt
- JIT makes one function-level compilation attempt; no later tier-up/retry

```text
block [A B X C D E F]  X unsupported
-> [A B]        count=2  abort
-> [C D E F]    count=4  abort unless forced by a linked branch or lowered cutoff
```

## Single-Compile Linking

Each accepted segment is compiled once and linked once through `asm.LinkAll`.
`asm.Link` exposes one primary callable per `asm.Code`. `asm.LinkAll` also
exposes `Code.Entries` as callables at their bound instruction offsets. JIT
uses those internal callables to install hot IPs that were merged into a
longer trace, avoiding duplicate native code for compatible fallthrough
entries.

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

`asm.Signature.Args` is the callable's input register layout, used to construct a `Caller`. Segment stack inputs use `asm.Signature.Args`; segment stack results use `asm.Signature.Returns`; VM context and `nextIP` use `asm.Signature.Scratch`. `asm.Caller` executes compiled chunks and returns typed `asm.Value` results while copying scratch inputs/outputs through the separate scratch slice.

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
