# Add an Opcode

End-to-end checklist. `instruction-set.md` owns semantics; this guide owns change order.

## Ownership

| Concern | Owner |
|---|---|
| opcode value | `instr/opcode.go` |
| mnemonic/width/stack effects | `instr/type.go` |
| verifier | `program/verify.go` |
| threaded lowering | `internal/codegen/` |
| fusion patterns | `internal/codegen/pattern.go` |
| generated handlers | `interp/threaded.go` |
| ARM64 lowering | `internal/jit/arm64/` |
| tests | `interp/*_test.go`, owner tests |
| reference | `instruction-set.md` |

## Opcode

Append to `instr/opcode.go`; never insert between existing values. `iota` is the encoded byte.

## Metadata

Add one entry to `instr/type.go`:

```go
I32_MY_OP: {Mnemonic: "i32.my_op", Pop: []Kind{KindI32}, Push: []Kind{KindI32}},
```

Declare fixed `Widths`. Leave stack effects dynamic when they depend on operands, constants, declared types, or runtime values.

## Verification

Change `checker.step` only when metadata cannot validate the instruction. Handle operand-dependent stack effects, type-indexed allocation, call/tail-call arity, counted constructors, and control/termination rules.

Reject statically malformed bytecode. Leave runtime-dependent behavior to runtime.

## Threaded Semantics

Add one domain emitter. Register it once in `internal/codegen/lower.go`, then run `make generate`.

The emitter serves standalone and fused lowering from one semantic implementation. Patterns select sequences and compile-time guards only.

Do not edit `interp/threaded.go` directly.

Preserve:

- compile-time IP advancement by the first absorbed instruction width;
- runtime IP advancement by exact instruction width;
- stack bounds checks;
- borrow/retain/release ownership;
- existing runtime error/panic conventions.

## JIT

Add ARM64 lowering only when guards and fallback are explicit:

- decline before mutating lowering state when unsupported;
- deopt before unsupported behavior executes;
- prefer terminal fallback over duplicated interpreter behavior;
- preserve stack/local/global/upvalue/ref ownership.

See `jit-internals.md`.

## Tests

Add runtime behavior to the existing opcode corpus when one row proves it. Add verifier cases for rejected bytecode and architecture-specific tests for native contracts. Follow `testing.md` for TDD and golden rules.

## Documentation

Update only owner docs:

| Change | Owner |
|---|---|
| semantics/status | `instruction-set.md` |
| verification | `verification.md` |
| ownership | `memory-model.md` |
| representation/kinds | `value-representation.md` |
| JIT contract | `jit-internals.md` |

## Validation

```bash
make check-generated
make check-tidy check-fmt vet
```

With JIT changes, run the relevant ARM64 tests/benchmarks on ARM64.

## Related

- `instruction-set.md`
- `verification.md`
- `jit-internals.md`
- `testing.md`
