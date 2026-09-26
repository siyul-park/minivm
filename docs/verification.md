# Verification

Static validation of untrusted bytecode.

## Ownership

| Concern | Owner |
|---|---|
| verifier | `program/verify.go` |
| opcode metadata | `instr/type.go` |
| opcode semantics | `instruction-set.md` |
| runtime checks | `interp/threaded.go` |

`program.New` and `interp.New` trust input. External bytecode therefore requires `program.Verify` before execution.

## Contract

| Rule | Requirement |
|---|---|
| Admission | Malformed bytecode `MUST` be rejected before execution. |
| Independence | Verification `MUST NOT` depend on interpretation, `analysis`, or `pass`. |
| Structure | Boundaries, widths, targets, handler ranges, and termination `MUST` be valid. |
| Stack | Height and statically known kinds `MUST` be tracked conservatively. |
| Dynamic effects | Resolve only from known operands/metadata; otherwise reject rather than guess. |
| Runtime-dependent cases | Definite static mismatches `MUST` be rejected; remaining checks belong to runtime guards.

## Checks

Order: structure → control flow → termination → stack → calls.

Dynamic arity `MUST` use statically known counts/metadata; otherwise verification `MUST` reject rather than guess. Errors `MUST` identify the location and violated rule. Heap, dynamic refs, and host state remain runtime concerns.

## Related

- `instruction-set.md`
- `testing.md`
- `architecture.md`
