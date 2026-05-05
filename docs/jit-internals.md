# JIT Internals

How to write threaded and JIT handlers. Read this before modifying `interp/threaded.go` or `interp/jit_arm64.go`.

## Two-Phase Compilation Model

Every opcode is compiled **twice**:

1. **Threaded compilation** (at `interp.New()` time, always): the `threadedCompiler` converts bytecode to Go closures.
2. **JIT compilation** (at runtime, lazily, ARM64 only): the `jitCompiler` replaces closures for hot blocks with native ARM64 code.

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

**Special case — NOP (threaded):** Each NOP scans forward to count all consecutive NOPs starting at its own position, then advances `c.ip` by 1. This means `n` consecutive NOPs produce `n` closures, but only the first is ever reached in execution — it jumps `n` bytes forward, bypassing the rest. The NOP-padding that `ConstantFoldingPass` inserts is therefore free at runtime.

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

**NOP in JIT:** The JIT handler for NOP increments `c.ip` and returns `true` without emitting any native instruction — contributing to `count` without adding code.

## JIT Handler Contract (ARM64)

The JIT table is `var jit = [256]func(c *jitCompiler) bool` populated in `jit_arm64.go`.

Each entry emits virtual-register (VReg) IR via the `asm.Assembler` and returns `true` on success or `false` to abort the current sub-block:

```go
jit[instr.OPCODE] = func(c *jitCompiler) bool {
    c.ip++  // MUST advance ip unconditionally, even on failure

    // Pop operands from the VReg stack
    r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
    if !ok { return false }  // type/width mismatch → abort sub-block

    // Allocate a result VReg
    r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)

    // Emit ARM64 IR instruction
    c.assembler.Emit(arm64.ADD(r1, r0, r0))

    // Push result onto VReg stack
    c.assembler.Push(r1)
    return true
}
```

**Critical invariants:**
- `c.ip++` must be the **first statement**, before any early returns.
- `Take` checks both `RegType` (Int vs Float) and `RegWidth` (32 vs 64) — a mismatch **must** return `false`, not be coerced.
- `return false` causes the JIT to split the block at this instruction. The current sub-block (if long enough) is emitted, and a new sub-block starts after.
- `_PROLOGUE` (ARM64): returns `true`, emits nothing — the ABI setup is handled by the trampoline.
- `_EPILOGUE` (ARM64): emits `RET` — the trampoline handles register teardown.

## Assembler API

The `asm.Assembler` maintains two VReg stacks:

| Stack | Purpose |
|---|---|
| `stack []VReg` | values currently in-flight (mirrors VM value stack within the sub-block) |
| `params []VReg` | VRegs taken from an empty stack — become native function's ABI inputs |

| Method | Description |
|---|---|
| `Take(typ, width) (VReg, bool)` | Three outcomes: (1) stack empty → allocate new param VReg, return `true`; (2) top matches type+width → pop and return `true`; (3) top mismatches → return `(VReg{}, false)` |
| `Top(i int) (VReg, bool)` | Peek at the i-th element from the top without popping |
| `Push(reg VReg)` | Push a VReg onto the in-flight stack |
| `Pop() (VReg, bool)` | Pop the top VReg without type checking |
| `NewVReg(typ, width) VReg` | Allocate a new VReg without touching the stack |
| `Emit(inst Instruction) int` | Append an IR instruction; returns its index |
| `Build() (Caller, error)` | Finalize: allocate registers, encode, write to buffer, return callable |
| `Reset()` | Clear all state (called between sub-blocks) |

**`Take` vs `Pop`:**
- `Take` is the standard operand consumer. When the stack is empty it promotes the missing operand to an ABI parameter (so it arrives as a native argument). When the top VReg's type or width does not match, it returns `false` — the JIT handler must immediately `return false` in that case.
- `Pop` removes from the stack without type enforcement. Use only when consuming a VReg whose type you already verified via `Top`.

## Sub-Block Selection

The JIT driver in `jit.go` selects which instruction sequences to compile:

1. `BasicBlocksPass` divides the bytecode into basic blocks.
2. For each block, the JIT iterates instructions left-to-right, calling `jit[opcode](c)`.
3. A run of consecutive `true` results is a candidate sub-block. `entryIP` is the byte offset (in `c.code`) of the first instruction that returned `true`.
4. When a `false` or end-of-block is hit: if `count > 8` (strictly greater), the sub-block is compiled via `assembler.Build()`.
5. A new sub-block candidate starts after each `false`.

The compiled closure is installed at `compiled[entryIP]` (not `b.Start`). When invoked at runtime:
1. Pops `nParams` values from the top of `i.stack` into a `[]uint64` buffer.
2. Calls `fn.Call(params)` — executes the native chunk.
3. Writes `nRets` results back into `i.stack`, adjusting `i.sp`.
4. Sets `f.ip = lastIP` (the offset of the last successfully compiled instruction, **not** `b.End` — execution continues in the threaded tier from there).

## Buffer Lifecycle

`asm.Buffer` wraps mmap'd executable memory and must alternate between writable and executable states:

```
NewBuffer(size)    → writable state (no Seal needed before first Append)

# First JIT compilation:
buffer.Append(code)      → write bytes
buffer.Seal()            → mprotect PROT_EXEC|PROT_READ → now callable

# Subsequent JIT compilations:
buffer.Unseal()          → mprotect PROT_WRITE → writable again
buffer.Append(code)      → write new bytes at current offset
buffer.Seal()            → mprotect PROT_EXEC|PROT_READ → callable again

buffer.Free()            → munmap
```

`Build()` always calls `Unseal → Append → Seal`. On the first call, `Unseal` on an already-writable buffer is a no-op. The shared `i.buffer` across all JIT compilations means interleaved `Build()` calls are not safe — but the JIT compiles one function at a time, so this is not an issue in practice.

Violating the Seal/Unseal order on Apple Silicon causes `SIGBUS` or `SIGSEGV` (due to W^X enforcement).

## ARM64 Register Usage

The ARM64 ABI in `asm/arm64/` follows AAPCS64:
- Integer arguments/returns: `X0`–`X7` (8 max)
- Float arguments/returns: `D0`–`D7` / `S0`–`S7` (8 max)

The `Caller` header encodes param/return counts and float-register bitmasks into a `uint64`, which the assembly trampoline in `abi_arm64.s` uses to marshal arguments.
