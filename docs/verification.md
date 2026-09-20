# Verification

Static validation of untrusted bytecode.

## Ownership

| Concern | Owner |
|---|---|
| verifier | `program/verify.go` |
| opcode metadata | `instr/type.go` |
| opcode semantics | `instruction-set.md` |
| runtime checks | `interp/threaded.go` |

`program.New` and `interp.New` trust input. The agent `MUST` verify external bytecode before execution.

## Rules

- The verifier `MUST` reject malformed bytecode before execution.
- Verification `MUST` stay independent of interpretation.
- The verifier `MUST` validate instruction boundaries and operand widths.
- The verifier `MUST` validate control-flow targets and handler ranges.
- The verifier `MUST` track stack height and statically known kinds conservatively.
- The verifier `MUST` resolve dynamic stack effects only when required inputs are known.
- The verifier `MUST` reject statically definite mismatches and `MUST` defer runtime-dependent cases to runtime guards.
- `program/verify.go` `MUST NOT` depend on `analysis` or `pass`.

## Checks

The verifier `MUST` perform these checks in order:

1. **Structure** — decoding, operands, bounds, constants, types.
2. **Control flow** — branch targets, reachable blocks, handler ranges.
3. **Termination** — fallthrough, returns, throws, terminals.
4. **Stack** — height, fixed/dynamic effects, operand kinds.
5. **Calls** — callable shape, arity, return compatibility.

Dynamic arity instructions `MUST` use only statically known counts/metadata. Otherwise verification `MUST` reject the program rather than guessing.

Verification errors `MUST` identify the program location and violated rule. The agent `MUST` leave current heap values, dynamic references, and host state to runtime handling.

## Related

- `instruction-set.md`
- `testing.md`
- `architecture.md`
