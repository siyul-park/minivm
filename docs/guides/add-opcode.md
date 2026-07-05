# Guide: Adding a New Opcode

End-to-end checklist for adding one bytecode instruction.

## When to Read

Use this guide when adding or changing an opcode. For canonical semantics, keep `docs/instruction-set.md` as the reference and link there instead of repeating full opcode behavior here.

## Source of Truth

| Concern | File |
|---|---|
| Opcode value and append order | `instr/opcode.go` |
| Mnemonic, operand widths, fixed stack effects | `instr/type.go` |
| Static validation | `program/verify.go` |
| Runtime semantics | `interp/threaded.go` |
| ARM64 native lowering | `interp/jit_arm64.go` |
| Public reference | `docs/instruction-set.md` |

Threaded support is required. JIT support is optional and should be added only when it can preserve exact threaded fallback semantics.

## Step 1 — Define the Opcode

Append the new value to the existing `const` block in `instr/opcode.go`.

```go
const (
    // existing opcodes
    I32_MY_OP
)
```

Do not insert new values between existing opcodes. The `iota` value is the encoded opcode byte.

## Step 2 — Declare Metadata

Add the opcode to the `types` map in `instr/type.go`.

```go
I32_MY_OP: {Mnemonic: "i32.my_op", Pop: []Kind{KindI32}, Push: []Kind{KindI32}},
```

Use `Widths` for bytecode operands.

| Width form | Meaning |
|---|---|
| omitted or empty | no operands |
| `[]int{1}` / `[]int{2}` / `[]int{4}` / `[]int{8}` | one fixed-width operand |
| `[]int{-2, 2}` | count byte plus `count * 2` bytes |

Declare `Pop` and `Push` when the stack effect is fixed. If the effect depends on operands, constants, declared types, or runtime values, leave it dynamic and handle it explicitly in `program/verify.go`.

## Step 3 — Update Verification

Update `checker.step` in `program/verify.go` when metadata alone is not enough.

Typical dynamic cases include:

- operand-dependent stack effects
- type-indexed allocation instructions
- call and tail-call arity
- stack-counted constructors
- branch or termination behavior

Verifier changes should reject only statically malformed bytecode. Runtime traps remain runtime behavior.

## Step 4 — Implement Threaded Semantics

Add the threaded handler in `interp/threaded.go` and mirror nearby handlers.

Checklist:

- advance `c.ip` by the exact instruction width during compilation
- advance `i.fr.ip` by the exact instruction width during execution
- check stack underflow and overflow where applicable
- retain refs when copying or exposing them
- release refs when consuming or overwriting them
- panic with the existing runtime sentinel errors and let `interp.Run` recover

Keep the handler explicit. Do not add helpers unless they remove real duplication or name a meaningful operation.

## Step 5 — Add JIT Support When Appropriate

Add ARM64 lowering in `interp/jit_arm64.go` only when the operation can be lowered with clear guards and correct fallback.

Rules:

- unsupported kinds, operands, or heap shapes must return `false` before mutating lowering state
- guard failures must deopt before executing behavior the JIT cannot fully own
- terminal fallback is preferable to duplicating complex interpreter behavior
- stack, local, global, upvalue, and ref ownership must match the threaded interpreter

For the lowering contracts, use `docs/jit-internals.md`; do not repeat the full JIT model here.

## Step 6 — Write Tests

Add runtime cases to the existing `runTests` table in `interp/interp_test.go` when the behavior fits one row.

```go
{
    program: program.New([]instr.Instruction{
        instr.New(instr.I32_CONST, 21),
        instr.New(instr.I32_MY_OP),
    }),
    values: []types.Value{types.I32(42)},
},
```

Use explicit subtests only when the behavior needs separate setup, such as coroutine resume behavior, debugger state, or host integration.

Also update verifier tests when malformed bytecode should be rejected before execution.

## Step 7 — Update Documentation

Update only the canonical docs that own the changed behavior.

| Change | Documentation |
|---|---|
| opcode semantics, stack effect, JIT status | `docs/instruction-set.md` |
| verifier behavior | `docs/verification.md` |
| ref ownership or GC behavior | `docs/memory-model.md` |
| boxing or kind rules | `docs/value-representation.md` |
| JIT contract | `docs/jit-internals.md` |
| contributor checklist | this guide |

Avoid repeating the same explanation in multiple documents. Link to the canonical document instead.

## Step 8 — Verify

```bash
make test
make lint
```

If JIT support was added, also run the relevant ARM64 tests or benchmarks on ARM64 hardware.

## Related Docs

- `docs/instruction-set.md` — opcode reference
- `docs/verification.md` — static validation rules
- `docs/jit-internals.md` — native lowering and fallback contracts
- `docs/memory-model.md` — ref ownership and heap lifecycle
