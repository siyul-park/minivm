# Verification

Static validation of untrusted bytecode.

## Ownership

| Concern | Owner |
|---|---|
| verifier | `program/verify.go` |
| opcode metadata | `instr/type.go` |
| opcode semantics | `instruction-set.md` |
| runtime checks | `interp/threaded.go` |

`program.New` and `interp.New` trust input. Verify external bytecode first.

## Rules

- Reject malformed bytecode before execution.
- Keep verification independent of interpretation.
- Validate instruction boundaries and operand widths.
- Validate control-flow targets and handler ranges.
- Track stack height and statically known kinds conservatively.
- Resolve dynamic stack effects only when required inputs are known.
- Reject statically definite mismatches; defer runtime-dependent cases to runtime guards.
- Keep `program/verify.go` independent of `analysis` and `pass`.

## Checks

1. **Structure** — decoding, operands, bounds, constants, types.
2. **Control flow** — branch targets, reachable blocks, handler ranges.
3. **Termination** — fallthrough, returns, throws, terminals.
4. **Stack** — height, fixed/dynamic effects, operand kinds.
5. **Calls** — callable shape, arity, return compatibility.

Dynamic arity instructions use only statically known counts/metadata. Otherwise verification rejects the program rather than guessing.

Verification errors identify the program location and violated rule. Runtime handles current heap values, dynamic references, and host state.

## Related

- `instruction-set.md`
- `testing.md`
- `architecture.md`
