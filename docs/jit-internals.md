# JIT Internals

How to write threaded and JIT handlers. Read before editing `interp/threaded.go` or `interp/jit_arm64.go`.

## Agent Checklist

Before editing:

- confirm opcode width in `instr/type.go`
- check threaded/JIT parity
- read `profile.md` before changing thresholds, sampling, or profile-guided segment selection
- read `value-representation.md` before unboxing, boxing, or passing JIT values
- read `memory-model.md` before touching refs, heap objects, ref-holding locals/globals, or host functions

After editing:

- add/update table-driven cases in `interp/interp_test.go`
- ARM64/JIT changes: `go test ./asm/... ./interp`; otherwise `go test ./interp`
- keep threaded fallback correct even when JIT rejects a segment

## Two-Phase Compilation Model

Every opcode is compiled twice:

1. **Threaded compilation**: at `interp.New()`, always; bytecode → Go closures.
2. **JIT compilation**: runtime, lazy, ARM64 only; hot closures replaced with native code.

Both read the same bytecode slice. The threaded closure at `i.code[addr][ip]` is the authoritative fallback; JIT overwrites selected entries.

## Threaded Handler Contract

`threaded` is `var threaded = [256]func(c *threadedCompiler) func(*Interpreter)`, populated in `threaded.go:init()`.

Each entry has compile-time operand decoding and runtime execution:

```go
instr.OPCODE: func(c *threadedCompiler) func(i *Interpreter) {
    offset := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
    width := 3

    c.ip += width // must advance before return

    return func(i *Interpreter) {
        f := &i.frames[i.fp-1]
        _ = offset
        f.ip += width // exact instruction width
    }
},
```

Invariants:

- advance `c.ip` before returning the closure
- advance `f.ip` by exact instruction width
- closure must not capture `c` or read `c.code`; capture locals only
- retain refs entering the stack with `i.retain(addr)`
- release consumed refs with `i.release(addr)`
- do not catch closure errors; `panic(ErrX)` and let `interp.Run` recover

### NOP

Normal threaded `NOP` scans consecutive NOPs from its own position, advances compile-time `c.ip` by `1`, and returns a closure that jumps over the whole run. Thus `n` NOPs produce `n` closures, but only the first executes, so DCE padding costs one dispatch.

With `WithTick(1)`, threaded compilation preserves exact instruction boundaries; each NOP advances one byte.

```go
instr.NOP: func(c *threadedCompiler) func(i *Interpreter) {
    skip := 0
    for c.ip+skip < len(c.code) && instr.Opcode(c.code[c.ip+skip]) == instr.NOP {
        skip++
    }
    c.ip++
    return func(i *Interpreter) {
        i.frames[i.fp-1].ip += skip
    }
},
```

JIT `NOP` increments `c.ip`, returns `true, false`, emits no native instruction, and still contributes to segment `count`.

## JIT Handler Contract

`jit` is `var jit = [256]func(c *jitCompiler) (bool, bool)`, populated in `jit_arm64.go`.

| Return | Meaning |
|---|---|
| `true, false` | compiled; continue |
| `false, false` | not compilable; end segment |
| `true, true` | compiled branch terminator; end segment |

Branch handlers (`BR`, `BR_IF`, `BR_TABLE`) return `true, true`, emit their own `RET`, and skip `_EPILOGUE`; otherwise `_EPILOGUE` would overwrite the branch-set scratch next IP.

```go
jit[instr.OPCODE] = func(c *jitCompiler) (bool, bool) {
    c.ip++ // before every return path

    r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
    if !ok {
        return false, false
    }

    r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
    c.assembler.Emit(arm64.ADD(r1, r0, r0))
    c.assembler.Push(r1)
    return true, false
}
```

Invariants:

- advance `c.ip` before every return, including failures
- operand-reading handlers advance after reading operands
- `Take` checks type and width; mismatches return `false, false`, never coerce
- `false, false` ends the segment
- unsupported-truncated segments are kept only when `count > 4`; otherwise aborted
- branches return `true, true` and emit their own exit
- `_PROLOGUE`: `LDI(scratch, c.end)` loads fallthrough IP into scratch
- `_EPILOGUE`: `LDI(scratch, c.end)` + `RET`

## Reserved Registers and Next IP

ARM64 JIT uses scratch registers from `arch.Scratch = X10–X15` as metadata channels outside normal params/returns.

| Scratch | Purpose |
|---|---|
| `scratch[0]` | frame-local stack pointer `&i.stack[f.bp]` input |
| `scratch[1]` | heap pointer input |
| `scratch[2]` | globals pointer input |
| `scratch[3]` | next interpreter IP output |

`_PROLOGUE` loads `c.end` into the next-IP scratch; `_EPILOGUE` reloads possibly truncated `c.end`; branch handlers load branch target IP. `jitCompiler.closure()` initializes scratch inputs, calls `fn.Call()`, reads the next-IP scratch, and writes `i.frames[fp-1].ip`.

Mutable globals have no declared type. JIT `GLOBAL_SET`/`GLOBAL_TEE` use the source register kind, and JIT `GLOBAL_GET` compiles only when an earlier same-segment store proves the kind. Do not specialize `GLOBAL_GET` from the current global value; dynamic kind changes would need deopt stack reconstruction, which the current JIT ABI does not provide.

## Segment Selection

`jitCompiler.Compile(code)` computes basic-block heat with `Stats.Range(addr,start,end)`, skips unsampled blocks, and compiles hotter blocks first. Within a hot block, `compile` repeatedly calls `segment(code,start,end)` to extract independent compilable runs.

```text
block [A, B, X, C, D, E, F]  (X = non-compilable)
→ segment 1 [A, B]           count=2, below cutoff → aborted
→ segment 2 [C, D, E, F]     count=4, emitted if block ends here and cutoff is 4
```

Completed segments emit when `count >= WithCutoff` default `4` and the segment range has at least one profile sample. If stopped by a non-compilable opcode, the segment is kept only when `count > 4`; shorter ones call `assembler.Abort()`. Cold segments inside sampled blocks are skipped. JIT does not recompile or tier-up after the first function-level compilation attempt.

### Two-Pass Compilation

Branch-terminated blocks need target signatures to choose direct label branch vs exit stub.

1. **Pass 1**: compile all blocks; enqueue non-terminated segments; hold terminated segments in `branches` for signature extraction.
2. **Pass 2**: recompile terminated blocks after non-terminated signatures are known.

`linkable(targetIP)` is true when current return VRegs exactly match target `Signature.Params` by type and width. If linkable, emit direct `BLabel`; otherwise emit `LDI + RET` with target IP in scratch.

## Assembler API

`asm.Assembler` maintains:

| Stack | Purpose |
|---|---|
| `stack []VReg` | in-flight values mirroring VM stack within the segment |
| `params []VReg` | VRegs taken from empty stack; native ABI inputs |

| Method | Description |
|---|---|
| `Take(typ,width)` | if stack empty, create param; if top matches, pop; if mismatch, return false |
| `Top(i)` | peek i-th from top |
| `Push(reg)` | push VReg |
| `Pop()` | pop without type check |
| `NewVReg(typ,width)` | allocate VReg |
| `Emit(inst)` | append IR, return index |
| `Emits(insts...)` | append IRs |
| `NewLabel()` | allocate symbolic label |
| `Place(id)` | mark label target |
| `Scratch()` | allocate metadata scratch PReg |
| `Compile()` | allocate regs, encode, write buffer, return `RelocObject` |
| `Link(objects)` | patch cross-segment relocations, return `Caller`s |
| `Abort()` | discard current segment state |
| `Reset()` | full reset including global labels; use between functions, not segments |

`Take` is the standard operand consumer: empty stack becomes ABI param; type/width mismatch returns false and the JIT handler must return `false, false`.

`Pop` skips type checks; use only after verifying with `Top`.

`Build()` no longer exists. Pipeline:

1. emit IR, `Compile()` per segment → `RelocObject`
2. after all function segments, `Link([]*RelocObject)` → patch branches and return `[]Caller`

Intra-segment labels resolve in `Compile()`. Cross-segment labels become `Relocation`s patched by `Link()`.

## Buffer Lifecycle

`asm.Buffer` wraps mmap executable memory and must alternate writable/executable states.

```text
NewBuffer(size) → writable

Compile():
  buffer.Unseal() → PROT_WRITE
  buffer.Append(code)
  buffer.Seal()   → PROT_EXEC|PROT_READ

Link():
  buffer.Unseal()
  writeBytes(addr, patch)
  buffer.Seal()

buffer.Free() → munmap
```

Violating Seal/Unseal order on Apple Silicon causes `SIGBUS` or `SIGSEGV` due to W^X enforcement.

## ARM64 Register Usage

`asm/arm64/` follows AAPCS64:

- integer args/returns: `X0`–`X7`
- float args/returns: `D0`–`D7` / `S0`–`S7`
- scratch: `X10`–`X15` via `Scratch()`
- `X8`, `X9` reserved for trampoline bookkeeping

Trampoline `argv` layout:

```text
argv[0]:             header: nParams, nReturns, nReserved, type masks
argv[1..nReserved]:  scratch inputs/outputs
argv[nReserved+1..]: params in / returns out
```

`abi_arm64.s` marshals args from `argv`, loads reserved inputs `X10–X15`, calls native chunk via `BL`, then writes scratch outputs to `argv[1..nReserved]` and return values to `argv[nReserved+1..]`.
