# Guide: Adding a New Opcode

Step-by-step checklist for adding a new instruction end-to-end.

## Before You Start

Read [docs/jit-internals.md](../jit-internals.md) for the threaded/JIT handler contracts.  
Read [docs/instruction-set.md](../instruction-set.md) for existing opcode patterns to mirror.

---

## Step 1 — Define the Opcode

**File: `instr/opcode.go`**

Append the constant inside the existing `const ( ... )` block, in the appropriate category group:

```go
const (
    // ...existing opcodes...

    I32_MY_OP  // ← add here, in the i32 section
)
```

The iota value is the opcode byte. Order within the block matters — do not insert between existing opcodes.

---

## Step 2 — Declare Operand Width

**File: `instr/type.go`**

Add an entry to the `types` map:

```go
var types = map[Opcode]Type{
    // ...existing entries...

    I32_MY_OP: {"i32.my_op", []int{}},        // no operands
    // or:
    I32_MY_OP: {"i32.my_op", []int{4}},       // one 4-byte operand
    // or:
    I32_MY_OP: {"i32.my_op", []int{2}},       // one 2-byte operand
    // or (BR_TABLE pattern):
    I32_MY_OP: {"i32.my_op", []int{-2, 2}},  // count byte + count×2-byte values
}
```

Width values: `1`, `2`, `4`, `8` for fixed-width operands. Negative `-n` means a length-prefixed array with `n`-byte elements.

---

## Step 3 — Implement the Threaded Handler

**File: `interp/threaded.go`**

Add an entry to the `threaded [256]func` table in `init()`. Mirror the style of adjacent opcodes.

```go
instr.I32_MY_OP: func(c *threadedCompiler) func(i *Interpreter) {
    // COMPILE TIME: read operand bytes, advance c.ip
    // (omit operand reads if there are no operands)
    operand := int32(*(*int32)(unsafe.Pointer(&c.code[c.ip+1])))
    c.ip += 5  // 1 opcode + 4 operand bytes

    // RUNTIME: return closure capturing compile-time values
    return func(i *Interpreter) {
        f := &i.frames[i.fp-1]

        // bounds-check (match the style in threaded.go: i.sp == 0, not i.sp < 1)
        if i.sp == 0 {
            panic(ErrStackUnderflow)
        }

        // pop operand (release heap ref if KindRef)
        val := i.stack[i.sp-1]
        if val.Kind() == types.KindRef {
            i.release(val.Ref())
        }
        i.sp--

        // compute result and push (retain if result is KindRef)
        result := types.BoxI32(val.I32() + int32(operand))
        if i.sp == len(i.stack) {
            panic(ErrStackOverflow)
        }
        i.stack[i.sp] = result
        i.sp++

        f.ip += 5  // advance by exact instruction width
    }
},
```

**Checklist for the threaded handler:**
- [ ] `c.ip` advanced before returning
- [ ] `f.ip` advanced by the exact instruction width inside the closure
- [ ] Stack bounds checked (`ErrStackUnderflow`, `ErrStackOverflow`)
- [ ] `i.retain(addr)` called when a `KindRef` enters the stack
- [ ] `i.release(addr)` called when a `KindRef` is consumed
- [ ] No panic caught — let `interp.Run`'s `recover` handle it

---

## Step 4 — Implement the JIT Handler (Optional, ARM64 only)

**File: `interp/jit_arm64.go`**

Add an entry to the `jit [256]func` table inside the `init()` function. If JIT is not feasible (e.g. the opcode requires access to `*Interpreter` fields), skip this step — the threaded fallback is always correct.

```go
jit[instr.I32_MY_OP] = func(c *jitCompiler) bool {
    c.ip++  // MUST be first, even if returning false

    // Pop operands from the VReg stack
    r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
    if !ok { return false }

    // Allocate result VReg
    r1 := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)

    // Emit ARM64 instructions
    c.assembler.Emit(arm64.ADD(r1, r0, r0))  // example

    // Push result
    c.assembler.Push(r1)
    return true
}
```

**Checklist for the JIT handler:**
- [ ] `c.ip++` is the first statement
- [ ] `Take` called with correct `RegType` and `RegWidth` — returns `false` on mismatch
- [ ] All VRegs used in `Emit` were obtained from `Take` or `NewVReg`
- [ ] Result VReg pushed with `Push`
- [ ] `return false` used (not `panic`) when emission is not possible

---

## Step 5 — Write Tests

**File: `interp/interp_test.go`**

Add test cases to the package-level `var tests` slice (not a new test function). Each case is a `program` and the expected `values` remaining on the stack after execution:

```go
{
    program: program.New(
        []instr.Instruction{
            instr.New(instr.I32_CONST, 21),
            instr.New(instr.I32_MY_OP),
        },
    ),
    values: []types.Value{types.I32(42)},
},
```

Cover: normal case, edge cases (zero, min/max int32), error cases (stack underflow) if applicable.

---

## Step 6 — Verify

```bash
make test          # all tests, race detector
make lint          # goimports + go vet
```

If you added a JIT handler, test on ARM64 hardware (or an ARM64 runner) — the JIT tests have a `//go:build arm64` tag and only run on that platform.
