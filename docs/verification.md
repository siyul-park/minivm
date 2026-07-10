# Verification

Static pre-execution validation for minivm bytecode.

## When to Read

Use this document when changing `program.Verify`, adding an opcode, changing operand widths or stack effects, or changing how external bytecode is admitted.

`program.Verify` is the trust boundary for bytecode that comes from files, users, plugins, network input, generated code, or any other source outside the current trusted builder path.

## Source of Truth

| Concern | File |
|---|---|
| verifier implementation | `program/verify.go` |
| opcode metadata | `instr/type.go` |
| opcode semantics | `docs/instruction-set.md` |
| runtime fallback checks | `interp/threaded.go` |

`program.New` and `interp.New` trust their input. They do not verify bytecode. Call `program.Verify` before constructing an interpreter when the program is not already trusted.

## API

```go
if err := program.Verify(prog); err != nil {
    return err
}

vm := interp.New(prog)
```

The CLI `run` command verifies loaded bytecode before constructing the interpreter.

## Design Rules

- reject malformed bytecode before execution
- keep verification separate from interpretation
- keep structural checks strict
- keep stack checks conservative
- reject definite errors, not uncertain dynamic behavior
- let runtime guards handle dynamic behavior
- keep `program/verify.go` self-contained to avoid a dependency cycle through `program`

## What Verification Checks

Verification prevents external bytecode from:

- decoding past instruction bounds
- using unknown opcodes
- jumping into the middle of instructions
- using invalid constant, type, local, upvalue, or global indexes
- falling through from function bodies
- causing definite stack underflow
- causing definite operand type mismatch
- reaching control-flow merges with incompatible stack heights
- declaring invalid handler ranges or catch targets

Runtime traps such as divide-by-zero, heap exhaustion, invalid array indexes, fuel exhaustion, failed runtime casts, and host-function errors are not verification failures.

## Verify Errors

Verification returns either `nil` or `*program.VerifyError`.

`VerifyError` reports:

| Field | Meaning |
|---|---|
| slot | verifier slot; `0` is top-level code, `j+1` is constant `j` |
| offset | instruction byte offset |
| opcode | opcode at the failure point |
| cause | wrapped sentinel error |

Sentinel errors are compatible with `errors.Is`.

Common causes:

```text
ErrTruncated
ErrUnknownOpcode
ErrIndexOutOfRange
ErrStackUnderflow
ErrStackMismatch
ErrTypeMismatch
ErrFallThrough
ErrInvalidJump
ErrHandlerRange
ErrHandlerTarget
```

## Verification Passes

Each function slot is checked in four passes.

### 1. Structure

The verifier checks that every instruction:

- decodes within bounds
- uses a known opcode
- has complete operands
- references valid constant indexes
- references valid type indexes
- references valid local indexes
- references valid upvalue indexes

Examples:

- `CONST_GET` must reference `Constants`
- type-indexed instructions must reference `Types`
- `LOCAL_*` must fit within params plus locals
- `UPVAL_*` must fit within captures
- `GLOBAL_*` must fit within declared `.globals`

### 2. Control Flow

The verifier builds a small CFG and validates branch targets.

Branch targets include:

- `BR`
- `BR_IF`
- `BR_TABLE`

Each target must land on an instruction boundary. Top-level code may also
target the past-the-end offset, which exits the program just like ordinary
top-level fall-through. Function bodies must target an instruction and still
terminate explicitly.

Target offsets for `BR`, `BR_IF`, and `BR_TABLE` are computed by the shared
`instr.Targets(code, ip)` helper, also used by `analysis.BasicBlocksAnalysis`,
so the verifier and the optimizer/JIT CFG agree on how a target is derived
from an instruction. `program.Verify` remains the only place that decides
whether the past-the-end target is *legal* for a given slot.

### 3. Termination

Function bodies must terminate explicitly.

Every exit block in a function body must end with one of:

```text
RETURN
RETURN_CALL
UNREACHABLE
```

Top-level code, slot `0`, is exempt. The interpreter finishes top-level code by running off the end.

### 4. Operand Stack

The verifier abstractly interprets the CFG until a fixpoint.

It checks:

- stack underflow
- stack-height mismatch at merges
- definite type mismatch
- statically known call arity when recoverable

The abstract stack uses `types.Kind` plus a verifier-only top kind for unknown values. At control-flow merges, differing compatible information widens to unknown.

## Kind Rules

Operand kinds are compared by representation.

`i1`, `i8`, and `i32` are compatible for `i32`-representation operands.

```text
i8  accepted by i32 operand
i1  accepted by i32 operand
f32 not accepted by i32 operand
```

Most opcodes push the kind declared in `instr.Type.Push`.

`i32.and`, `i32.or`, and `i32.xor` are width-closed when both operands share the same narrow kind.

```text
i8 & i8  → i8
i1 ^ i1  → i1
i8 & i32 → i32
```

This must match interpreter and JIT behavior.

## Dynamic Effects

Some effects cannot always be checked statically.

Examples:

- `CALL` through a dynamic `ref`
- `RETURN_CALL` through a dynamic `ref`
- stack-counted `MAP_NEW`
- `CLOSURE_NEW`
- future dynamic operations

When stack effect cannot be determined statically, the stack pass stops without a final stack verdict for that function. Structural, control-flow, and termination checks still apply. The interpreter then owns the remaining runtime checks.

This is intentional. The verifier should avoid false rejection.

## Calls

The verifier resolves `CALL` and `RETURN_CALL` arity when the callee type is statically recoverable.

Recoverable cases include:

- function constants
- closure constants
- typed params
- typed locals
- typed upvalues

If the callee is dynamic and its function type cannot be recovered, verification does not guess. Runtime call checks handle it.

## What Verification Does Not Check

These are runtime concerns, not verifier failures:

- heap exhaustion
- fuel exhaustion
- context cancellation
- divide-by-zero
- array, string, or map index traps
- failed casts depending on runtime values
- host-function errors
- allocation failures
- dynamic call target mismatch

Do not move runtime policy into the verifier unless the error is statically malformed bytecode.

## Maintenance Notes

When changing verification:

- keep structural validation strict
- keep stack validation conservative
- update opcode stack effects and verifier cases together
- keep `i1`/`i8`/`i32` representation compatibility
- reject definite malformed bytecode
- do not reject dynamic behavior merely because it is unknown
- preserve `program.Verify` as the trust boundary
- keep `interp.New` free of implicit verification
- test malformed bytecode, bad targets, bad indexes, stack underflow, and type mismatch separately

## Related Docs

- `docs/instruction-set.md` — opcode semantics and stack effects
- `docs/guides/add-opcode.md` — opcode change checklist
- `docs/value-representation.md` — kind representation rules
- `docs/architecture.md` — package boundary rationale
