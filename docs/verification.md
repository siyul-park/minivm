# Verification

Static validation of untrusted minivm bytecode.

## When to Read

Read when changing `program.Verify`, opcode metadata, stack effects, operand widths, or bytecode admission.

## Source of Truth

| Concern | Owner |
|---|---|
| Verifier | `program/verify.go` |
| Opcode metadata | `instr/type.go` |
| Opcode semantics | `instruction-set.md` |
| Runtime checks | `interp/threaded.go` |

`program.New` and `interp.New` trust input. Call `program.Verify` before executing external bytecode.

## Rules

- Reject malformed bytecode before execution.
- Keep verification separate from interpretation.
- Validate instruction boundaries and operand widths.
- Validate control-flow targets and handler ranges.
- Track stack height and statically known kinds conservatively.
- Resolve dynamic stack effects only when their required inputs are known.
- Reject definite mismatches; leave runtime-dependent cases to runtime guards.
- Keep `program/verify.go` independent of `analysis` and `pass`.

## Checked

1. **Structure** — instruction decoding, operands, code bounds, constants, types.
2. **Control flow** — branch targets, reachable blocks, handler ranges.
3. **Termination** — valid fallthrough, returns, throws, and terminal instructions.
4. **Stack** — height, fixed effects, dynamic effects, and operand kinds.
5. **Calls** — callable shape, argument count, and return compatibility.

## Dynamic Effects

Instructions such as aggregate creation and calls may have runtime-dependent arity. Verification uses only statically available counts or resolved metadata; otherwise it rejects the program rather than guessing.

## Error Contract

Verification errors identify the program location and the violated rule. Runtime execution is responsible for behavior that depends on current heap values, dynamic references, or host state.

## Related Docs

- `instruction-set.md`
- `architecture.md`
- `testing.md`
