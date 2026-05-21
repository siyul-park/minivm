# Guide: Adding a New Opcode

End-to-end checklist for adding a new instruction.

## Agent Summary

Usually edit, in order:

1. `instr/opcode.go`
2. `instr/type.go`
3. `interp/threaded.go`
4. `interp/jit_arm64.go` if ARM64 JIT support is practical
5. `instr/*_test.go`, `interp/interp_test.go`, this guide/reference docs

Threaded support is mandatory. JIT support is optional.

## Before You Start

Read `docs/jit-internals.md` for threaded/JIT contracts and `docs/instruction-set.md` for opcode patterns.

## Step 1 — Define the Opcode

File: `instr/opcode.go`

Append inside existing `const` block, in the right category.

```go
const (
    // ...existing opcodes...

    I32_MY_OP
)
```

`iota` value is opcode byte. Order matters; never insert between existing opcodes.

## Step 2 — Declare Operand Width

File: `instr/type.go`

Add to `types` map.

```go
var types = map[Opcode]Type{
    // ...existing entries...

    I32_MY_OP: {"i32.my_op", []int{}},       // no operands
    I32_MY_OP: {"i32.my_op", []int{4}},      // one 4-byte operand
    I32_MY_OP: {"i32.my_op", []int{2}},      // one 2-byte operand
    I32_MY_OP: {"i32.my_op", []int{-2, 2}}, // count byte + count×2-byte values
}
```

Fixed widths: `1`, `2`, `4`, `8`. Negative `-n` means length-prefixed array with `n`-byte elements.

## Step 3 — Implement Threaded Handler

File: `interp/threaded.go`

Add entry to `threaded [256]func` table in `init()`. Mirror nearby opcode style.

```go
instr.I32_MY_OP: func(c *threadedCompiler) func(i *Interpreter) {
    operand := int32(*(*int32)(unsafe.Pointer(&c.code[c.ip+1])))
    c.ip += 5 // 1 opcode + 4 operand bytes

    return func(i *Interpreter) {
        f := &i.frames[i.fp-1]

        if i.sp == 0 {
            panic(ErrStackUnderflow)
        }

        val := i.stack[i.sp-1]
        if val.Kind() == types.KindRef {
            i.release(val.Ref())
        }
        i.sp--

        result := types.BoxI32(val.I32() + int32(operand))

        if i.sp == len(i.stack) {
            panic(ErrStackOverflow)
        }
        i.stack[i.sp] = result
        i.sp++

        f.ip += 5
    }
},
```

Checklist:

- [ ] advance `c.ip` before returning
- [ ] advance `f.ip` by exact instruction width
- [ ] check stack bounds with `ErrStackUnderflow` / `ErrStackOverflow`
- [ ] call `i.retain(addr)` when `KindRef` enters stack
- [ ] call `i.release(addr)` when `KindRef` is consumed
- [ ] do not catch panics; let `interp.Run` recover

## Step 4 — Implement JIT Handler

File: `interp/jit_arm64.go`

Optional, ARM64 only. Skip if opcode needs hard-to-compile interpreter access; threaded fallback remains correct.

```go
jit[instr.I32_MY_OP] = func(s *jitSeg) (bool, bool) {
    s.ip++ // before every return path

    r0, ok := s.Take(asm.RegTypeInt, asm.Width32)
    if !ok {
        return false, false
    }

    r1 := s.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
    s.assembler.Emit(arm64.ADD(r1, r0, r0))
    s.Push(r1)

    return true, false
}
```

Return values are `(ok, stop)`:

| Return | Meaning |
|---|---|
| `true, false` | compiled; continue |
| `false, false` | not compilable; end segment |
| `true, true` | compiled branch terminator; emits own `RET` |

Checklist:

- [ ] advance `c.ip` before every return
- [ ] call `Take` with correct `RegType` and `RegWidth`
- [ ] return `false, false` on type/width mismatch
- [ ] use only VRegs from `Take` or `NewVReg`
- [ ] push result VReg with `Push`
- [ ] use `return false, false`, not `panic`, when emission is impossible
- [ ] non-branch instructions return `(ok, false)`; only branch terminators return `(true, true)`

## Step 5 — Write Tests

File: `interp/interp_test.go`

Add cases to package-level `var tests`, not a new test function.

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

Cover normal behavior, edge cases such as zero and min/max values, and error cases such as stack underflow when applicable.

## Step 6 — Verify

```bash
make test
make lint
```

If adding JIT support, test on ARM64 hardware or runner. JIT tests use `//go:build arm64` and only run there.
