# JIT Internals

How to write threaded and JIT handlers. Read this before modifying `interp/threaded.go` or `interp/jit_arm64.go`.

## Agent Checklist

Before editing:
- Confirm opcode width in `instr/type.go`.
- Check threaded and JIT behavior for interpreter/JIT parity.
- Read [profile.md](profile.md) before changing hotness thresholds, sampling, or profile-guided segment selection.
- Read [value-representation.md](value-representation.md) before unboxing, boxing, or passing JIT values.
- Read [memory-model.md](memory-model.md) before touching refs, heap objects, locals/globals that may hold refs, or host functions.

After editing:
- Add or update table-driven cases in `interp/interp_test.go`.
- On ARM64/JIT changes, run `go test ./asm/... ./interp`; otherwise run `go test ./interp`.
- Keep the threaded fallback correct even when JIT rejects a segment.

## Two-Phase Compilation Model

Every opcode is compiled **twice**:

1. **Threaded compilation** (at `interp.New()` time, always): the `threadedCompiler` converts bytecode to Go closures.
2. **JIT compilation** (at runtime, lazily, ARM64 only): the `jitCompiler` replaces closures for hot segments with native ARM64 code.

Both compilation models read the same bytecode from the same byte slice. The threaded closure at index `ip` in `i.code[addr]` is the authoritative fallback; the JIT overwrites selected entries.

## Threaded Handler Contract

The threaded table is `var threaded = [256]func(c *threadedCompiler) func(*Interpreter)` populated in `init()` in `threaded.go`.

Each entry is a **two-phase function**:

```go
instr.OPCODE: func(c *threadedCompiler) func(i *Interpreter) {
    // ── COMPILE TIME ──────────────────────────────────────────────
    // Read operand bytes using unsafe.Pointer casts.
    // Widths must match the declaration in instr/type.go.
    offset := int(*(*uint16)(unsafe.Pointer(&c.code[c.ip+1])))
    width := 3  // 1 opcode byte + 2 operand bytes

    c.ip += width  // MUST advance c.ip before returning

    // ── RUNTIME ───────────────────────────────────────────────────
    // Return a closure that captures compile-time values.
    // The closure must NOT read c.code — it may no longer be valid.
    return func(i *Interpreter) {
        f := &i.frames[i.fp-1]
        // ... perform the operation ...
        f.ip += width  // MUST advance f.ip by the exact instruction width
    }
},
```

**Critical invariants:**
- `c.ip` must be advanced **before** the closure is returned, not inside it.
- `f.ip` must be advanced by **exactly** the instruction width — not more, not less.
- The closure must not capture `c` or read `c.code` (use local variables).
- Reference counting: call `i.retain(addr)` when a ref enters the stack, `i.release(addr)` when consumed.
- Do not catch errors inside closures — `panic(ErrX)` and let `interp.Run`'s `recover` handle it.

**Special case — NOP (threaded):** Each NOP scans forward to count all consecutive NOPs starting at its own position, then advances `c.ip` by 1. This means `n` consecutive NOPs produce `n` closures, but only the first is reached in execution — it jumps `n` bytes forward, bypassing the rest. The NOP-padding that `ConstantFoldingPass` inserts therefore takes one dispatch for the whole run.

```go
instr.NOP: func(c *threadedCompiler) func(i *Interpreter) {
    skip := 0
    for c.ip+skip < len(c.code) && instr.Opcode(c.code[c.ip+skip]) == instr.NOP {
        skip++
    }
    c.ip++  // advance by 1; outer loop will call this handler again for each NOP
    return func(i *Interpreter) {
        i.frames[i.fp-1].ip += skip  // only executed for the FIRST NOP in a run
    }
},
```

**NOP in JIT:** The JIT handler for NOP increments `c.ip` and returns `true, false` without emitting any native instruction — contributing to `count` without adding code.

## JIT Handler Contract (ARM64)

The JIT table is `var jit = [256]func(c *jitCompiler) (bool, bool)` populated in `jit_arm64.go`.

Each entry emits virtual-register (VReg) IR via the `asm.Assembler` and returns two bools:

| Return | Meaning |
|--------|---------|
| `true, false` | Instruction compiled; continue to next instruction |
| `false, false` | Instruction not compilable; end this segment here |
| `true, true` | Instruction compiled **and** the block terminates (branch); end this segment |

`true, true` is returned by branch handlers (`BR`, `BR_IF`, `BR_TABLE`) which emit their own `RET`. The second `true` tells `segment()` to skip emitting `_EPILOGUE` — doing so would overwrite the scratch register (next IP) that the branch already set.

```go
jit[instr.OPCODE] = func(c *jitCompiler) (bool, bool) {
    c.ip++  // MUST advance ip unconditionally before returning

    // Pop operands from the VReg stack
    r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
    if !ok { return false, false }  // type/width mismatch → end segment

    // Allocate a result VReg
    r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)

    // Emit ARM64 IR instruction
    c.assembler.Emit(arm64.ADD(r1, r0, r0))

    // Push result onto VReg stack
    c.assembler.Push(r1)
    return true, false
}
```

**Critical invariants:**
- `c.ip` must be advanced before any return path, including failure paths. Fixed-width no-operand handlers usually do this as the first statement; operand-reading handlers advance after reading their operands.
- `Take` checks both `RegType` (Int vs Float) and `RegWidth` (32 vs 64) — a mismatch **must** return `false, false`, not be coerced.
- `return false, false` ends the current segment. Segments cut short by an unsupported instruction are kept only after more than 4 compiled instructions; otherwise they are aborted.
- Branch handlers (`BR`, `BR_IF`, `BR_TABLE`) return `true, true` and must emit their own exit (LDI + RET or a label branch). The `_EPILOGUE` is never emitted after `true, true`.
- `_PROLOGUE` (ARM64): emits `LDI(scratch, c.end)` — loads the fallthrough IP into the scratch register before any instructions.
- `_EPILOGUE` (ARM64): emits `LDI(scratch, c.end)` + `RET` — updates the scratch register with the (possibly truncated) exit IP and returns.

## Reserved Registers and Next-IP Mechanism

ARM64 JIT uses **scratch registers** (allocated via `c.assembler.Scratch()` from `arch.Scratch` = X10–X15) as in/out metadata channels outside normal params and returns.

- `scratch[0]`: frame-local stack pointer (`&i.stack[f.bp]`) input.
- `scratch[1]`: heap pointer input.
- `scratch[2]`: next interpreter IP output.
- `_PROLOGUE` loads `c.end` into `scratch[2]`.
- `_EPILOGUE` reloads `c.end` (which may be truncated if the segment was cut short) into `scratch[2]`, then returns.
- Branch handlers load the **branch target IP** into `scratch[2]` before returning.

The Go closure in `jitCompiler.closure()` initializes scratch inputs before `fn.Call()`, then reads `scratch[2]` after the call and writes it to `i.frames[fp-1].ip`, advancing the interpreter to the correct position regardless of whether the segment ran to segment end or exited via a branch.

## Segment Selection

`jitCompiler.Compile(code)` computes each basic block's profile heat with `Stats.Range(addr, start, end)`, skips blocks with no samples, then compiles hotter blocks first. Within each hot block, `compile` iterates `segment(code, start, end)` to extract **multiple independent compilable segments**:

```
block [A, B, X, C, D, E, F]   (X = non-compilable)
→ segment 1: [A, B]           count=2, below threshold → aborted
→ segment 2: [C, D, E, F]     count=4, emitted if the block ends here and WithCutoff is 4
```

Completed segments emit when their compiled instruction count is at least `WithCutoff` (default 4) and their own byte range has at least one profile sample. If a segment stops because the next opcode is not compilable, the current implementation keeps it only when `count > 4`; shorter truncated segments are aborted via `assembler.Abort()`. Cold segments inside a sampled block are skipped without emitting native code. The JIT does not currently recompile or tier-up code after the first function-level compilation attempt.

### Two-Pass Compilation

Blocks that **terminate** (end with BR/BR_IF/BR_TABLE) need to know the signatures of their branch targets to decide whether to emit a direct label branch or an exit stub. A two-pass strategy handles this:

1. **Pass 1**: Compile all blocks. Non-terminated segments are immediately added to the link queue. Terminated blocks have their segments compiled (for signature extraction) but held in a `branches` list.
2. **Pass 2**: Recompile terminated blocks now that all non-terminated block signatures are known. The branch handlers can now make correct `linkable()` decisions.

`linkable(targetIP)` returns true when the current assembler's return VRegs exactly match the target block's `Signature.Params` (type and width). If linkable, a direct `BLabel` is emitted; otherwise a `LDI + RET` stub is emitted with the target IP in scratch.

## Assembler API

The `asm.Assembler` maintains two VReg stacks:

| Stack | Purpose |
|---|---|
| `stack []VReg` | values currently in-flight (mirrors VM value stack within the segment) |
| `params []VReg` | VRegs taken from an empty stack — become native function's ABI inputs |

| Method | Description |
|---|---|
| `Take(typ, width) (VReg, bool)` | Three outcomes: (1) stack empty → allocate new param VReg, return `true`; (2) top matches type+width → pop and return `true`; (3) top mismatches → return `(VReg{}, false)` |
| `Top(i int) (VReg, bool)` | Peek at the i-th element from the top without popping |
| `Push(reg VReg)` | Push a VReg onto the in-flight stack |
| `Pop() (VReg, bool)` | Pop the top VReg without type checking |
| `NewVReg(typ, width) VReg` | Allocate a new VReg without touching the stack |
| `Emit(inst Instruction) int` | Append an IR instruction; returns its index |
| `Emits(insts ...Instruction)` | Append multiple IR instructions |
| `NewLabel() int` | Allocate a symbolic label ID (resolved at Compile/Link time) |
| `Place(id int)` | Mark the current position as the target of label `id` |
| `Scratch() PReg` | Allocate one scratch physical register from `arch.Scratch` for metadata |
| `Compile() (*RelocObject, error)` | Finalize: allocate registers, two-pass encode, write to buffer, return relocatable object |
| `Link(objects) ([]Caller, error)` | Patch cross-segment label relocations; return callable `Caller` per object |
| `Abort()` | Discard current segment state without writing to buffer |
| `Reset()` | Full reset including global label table (call between functions, not between segments) |

**`Take` vs `Pop`:**
- `Take` is the standard operand consumer. When the stack is empty it promotes the missing operand to an ABI parameter. When the top VReg's type or width does not match, it returns `false` — the JIT handler must immediately `return false, false`.
- `Pop` removes from the stack without type enforcement. Use only when consuming a VReg whose type you already verified via `Top`.

**`Compile` vs `Build`:**
`Build()` no longer exists. The new pipeline is:
1. Emit instructions, call `Compile()` per segment → produces a `RelocObject`.
2. After all segments of a function are compiled, call `Link([]*RelocObject)` → patches cross-segment branches and returns `[]Caller`.

Labels within a single segment are resolved inside `Compile()`. Labels that span segments become `Relocation` entries in the `RelocObject`, patched by `Link()`.

## Buffer Lifecycle

`asm.Buffer` wraps mmap'd executable memory and must alternate between writable and executable states:

```
NewBuffer(size)    → writable state

# Per-segment Compile():
buffer.Unseal()          → mprotect PROT_WRITE
buffer.Append(code)      → write bytes
buffer.Seal()            → mprotect PROT_EXEC|PROT_READ

# Link():
buffer.Unseal()          → mprotect PROT_WRITE  (for patching relocations)
writeBytes(addr, patch)  → overwrite relocation sites
buffer.Seal()            → mprotect PROT_EXEC|PROT_READ

buffer.Free()            → munmap
```

Violating the Seal/Unseal order on Apple Silicon causes `SIGBUS` or `SIGSEGV` (W^X enforcement).

## ARM64 Register Usage

The ARM64 ABI in `asm/arm64/` follows AAPCS64:
- Integer arguments/returns: `X0`–`X7` (8 max)
- Float arguments/returns: `D0`–`D7` / `S0`–`S7` (8 max)
- Scratch registers: `X10`–`X15` (allocated by `Scratch()`; X8 and X9 are left free for the trampoline's own bookkeeping)

The `argv` buffer passed to the assembly trampoline has the layout:
```
argv[0]:              header (nParams, nReturns, nReserved, type masks)
argv[1..nReserved]:   scratch inputs/outputs — loaded before the native call and written back on return
argv[nReserved+1..]:  params in / returns out
```

The trampoline in `abi_arm64.s` marshals arguments from `argv`, loads reserved register inputs (X10–X15), calls the native chunk via `BL`, then reads scratch register values back into `argv[1..nReserved]` and reads return values into `argv[nReserved+1..]`.
