# Add an Opcode

End-to-end checklist.

`instruction-set.md` owns semantics; this guide owns change order.

## Ownership

| Concern | Owner |
|---|---|
| opcode value | `instr/opcode.go` |
| mnemonic/width/stack effects | `instr/type.go` |
| verifier | `program/verify.go` |
| threaded lowering | `internal/codegen/` |
| fusion patterns | `internal/codegen/pattern.go` |
| generated handlers | `interp/threaded.go` |
| ARM64 encoding | `internal/asm/arm64/` |
| tests | `interp/*_test.go`, owner tests |
| reference | `instruction-set.md` |

## Opcode

The agent `MUST` append to `instr/opcode.go` and `MUST NOT` insert between existing values. `iota` is the encoded byte.

## Metadata

The agent `MUST` add one entry to `instr/type.go`:

```go
I32_MY_OP: {Mnemonic: "i32.my_op", Pop: []Kind{KindI32}, Push: []Kind{KindI32}},
```

The entry `MUST` declare fixed `Widths`. The agent `MUST` leave stack effects dynamic when they depend on operands, constants, declared types, or runtime values.

## Verification

The agent `MUST` change `checker.step` only when metadata cannot validate the instruction. It `MUST` handle operand-dependent stack effects, type-indexed allocation, call/tail-call arity, counted constructors, and control/termination rules.

The verifier `MUST` reject statically malformed bytecode and `MUST` leave runtime-dependent behavior to runtime.

## Threaded Semantics

The agent `MUST` add one domain emitter, register it once in `internal/codegen/lower.go`, then run `make generate`.

The emitter serves standalone and fused lowering from one semantic implementation. Patterns select sequences and compile-time guards only.

The agent `MUST NOT` edit `interp/threaded.go` directly.

The agent `MUST` preserve:

- compile-time IP advancement by the first absorbed instruction width;
- runtime IP advancement by exact instruction width;
- stack bounds checks;
- borrow/retain/release ownership;
- existing runtime error/panic conventions.

## JIT

The agent `MUST` add ARM64 lowering only when guards and fallback are explicit:

- decline before mutating lowering state when unsupported;
- deopt before unsupported behavior executes;
- prefer terminal fallback over duplicated interpreter behavior;
- preserve stack/local/global/upvalue/ref ownership.

See `jit-internals.md`.

## Tests

The agent `MUST` add runtime behavior to the existing opcode corpus when one row proves it. It `MUST` add verifier cases for rejected bytecode and architecture-specific tests for native contracts. It `MUST` follow `testing.md` for TDD and golden rules.

## Documentation

The agent `MUST` update only owner docs:

| Change | Owner |
|---|---|
| semantics/status | `instruction-set.md` |
| verification | `verification.md` |
| ownership | `memory-model.md` |
| representation/kinds | `value-representation.md` |
| JIT contract | `jit-internals.md` |

## Validation

The agent `MUST` run:

```bash
make check-generated
make check-tidy check-fmt vet
```

With the native rebuild, the agent MUST run the relevant ARM64 tests/benchmarks on ARM64.

## Related

- `instruction-set.md`
- `verification.md`
- `jit-internals.md`
- `testing.md`
