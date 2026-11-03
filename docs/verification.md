# Verification

Static validation of untrusted bytecode.

## Ownership

| Concern | Owner |
|---|---|
| verifier | `program/verify.go` |
| opcode metadata | `instr/type.go` |
| opcode semantics | `instruction-set.md` |
| runtime checks | `interp/threaded.go` |

`program.New` trusts input. `interp.New` and `interp.NewPool` verify a `program.Program.Clone` snapshot of their input and build from that snapshot, returning the `*program.VerifyError` on rejection; later mutation of the input program, including its function constants, does not change execution. Type objects (`Types`, function signatures) are shared with the snapshot and `MUST NOT` be mutated. `interp.MustNew` is the panicking form for known-good programs. `program.Verify` rejects a nil program with `ErrNil`.

## Contract

| Rule | Requirement |
|---|---|
| Admission | Malformed bytecode `MUST` be rejected before execution. |
| Independence | Verification `MUST NOT` depend on interpretation, `analysis`, or `pass`. |
| Structure | Boundaries, widths, targets, handler ranges, and termination `MUST` be valid. |
| Stack | Height and statically known kinds `MUST` be tracked conservatively. |
| Dynamic effects | Resolve only from known operands/metadata; otherwise reject rather than guess. |
| Runtime-dependent cases | Definite static mismatches `MUST` be rejected; remaining checks belong to runtime guards. |

## Checks

Order: structure → control flow → termination → stack → calls.

Dynamic arity `MUST` use statically known counts/metadata and never a guessed count. An op whose pop count is a runtime value (`array.new`, `map.new`, `closure.new`, `struct.new` of a non-struct type) ends stack tracking on its path; its runtime underflow guard covers the rest. Every function reachable from a constant is verified, including one nested in an array, map, or struct literal; it reports slot `j+1` of its top-level constant `j`. Errors `MUST` identify the location and violated rule. Heap, dynamic refs, and host state remain runtime concerns.

## Related

- `instruction-set.md`
- `testing.md`
- `architecture.md`
