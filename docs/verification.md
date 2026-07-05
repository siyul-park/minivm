# Verification

Static pre-execution validation for minivm bytecode.

The verifier lives in `program/verify.go`.

## Summary

`program.Verify` is the trust boundary for untrusted bytecode.

`program.New` and `interp.New` trust their input. They do not verify bytecode. Call `program.Verify` before constructing an interpreter when bytecode comes from files, users, plugins, network input, generated code, or any other untrusted source.

Default design rule:

* reject malformed bytecode before execution
* keep verification separate from interpretation
* keep structural checks strict
* keep stack checks conservative
* never reject when the verifier cannot know statically
* let runtime guards handle dynamic behavior
* use short, standard names such as `check`, `step`, `block`, `stack`, `kind`, and `target`

## Agent Fast Path

Read this before:

* changing `program/verify.go`
* adding an opcode
* changing opcode operands
* changing stack effects
* changing how untrusted bytecode is loaded
* changing CLI `run` behavior

When adding or changing an opcode:

| Change                   | Update                                                    |
| ------------------------ | --------------------------------------------------------- |
| fixed stack effect       | `instr.Type.Pop` and `instr.Type.Push` in `instr/type.go` |
| operand-dependent effect | case handling in `checker.step`                           |
| new operand width        | `instr/type.go` and verifier structure checks             |
| new branch behavior      | CFG construction and branch-target validation             |
| new type behavior        | stack-kind rules in `checker.step`                        |
| new runtime trap         | usually not a verifier error unless statically malformed  |

`program/verify.go` intentionally stays self-contained. It does not import `analysis` or `pass`, avoiding a dependency cycle through `program`.

## Why Verification Exists

minivm is designed for safe plugin, DSL, rules, and embedded script execution.

Like WebAssembly or the JVM, it should reject malformed or hostile bytecode before running it.

Verification prevents untrusted bytecode from:

* decoding past instruction bounds
* jumping into the middle of instructions
* using invalid pool, local, type, or upvalue indexes
* falling through from function bodies
* causing definite stack underflow
* causing definite operand type confusion
* reaching control-flow merges with incompatible stack heights

Runtime traps such as divide-by-zero, heap exhaustion, invalid array indexes, and fuel exhaustion are still runtime behavior, not verification failures.

## API

```go id="1o28i6"
if err := program.Verify(prog); err != nil {
    return err
}

vm := interp.New(prog)
```

`interp.New` does not verify. Verify first when the program is untrusted.

The CLI `run` command verifies loaded bytecode before constructing the interpreter.

## Verify Errors

Verification returns either `nil` or `*program.VerifyError`.

`VerifyError` reports:

| Field  | Meaning                                                     |
| ------ | ----------------------------------------------------------- |
| slot   | verifier slot; `0` is top-level code, `j+1` is constant `j` |
| offset | instruction byte offset                                     |
| opcode | opcode at the failure point                                 |
| cause  | wrapped sentinel error                                      |

Sentinel errors are compatible with `errors.Is`.

Common causes:

```text id="l2ox5m"
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

* decodes within bounds
* uses a known opcode
* has complete fixed-width or variable-width operands
* references valid constant indexes
* references valid type indexes
* references valid local indexes
* references valid upvalue indexes

Examples:

* `CONST_GET` must reference `Constants`
* `REF_TEST`, `REF_CAST`, `ARRAY_NEW`, `STRUCT_NEW`, and `MAP_NEW` must reference `Types`
* `LOCAL_*` must fit within params plus locals
* `UPVAL_*` must fit within captures

### 2. Control Flow

The verifier builds a CFG and validates branch targets.

It checks that each branch target lands on an instruction boundary.

Branch targets include:

* `BR`
* `BR_IF`
* `BR_TABLE`

### 3. Termination

Function bodies must terminate explicitly.

Every exit block in a function body must end with one of:

```text id="q9eh7q"
RETURN
RETURN_CALL
UNREACHABLE
```

Top-level code, slot `0`, is exempt. The interpreter finishes top-level code by running off the end.

### 4. Operand Stack

The verifier performs abstract interpretation over the CFG until a fixpoint.

It checks:

* stack underflow
* stack-height mismatch at merges
* definite type mismatch
* statically known call arity when recoverable

The abstract stack uses `types.Kind` plus a verifier-only top kind for unknown values.

At control-flow merges, differing compatible information widens to unknown. This keeps verification conservative: it rejects definite errors, not uncertain dynamic behavior.

## Kind Rules

Operand kinds are compared by representation.

This means `i1`, `i8`, and `i32` are compatible for `i32`-representation operands.

Examples:

```text id="kduhjr"
i8  accepted by i32 operand
i1  accepted by i32 operand
f32 not accepted by i32 operand
```

Most opcodes push the kind declared in `instr.Type.Push`.

Special case: `i32.and`, `i32.or`, and `i32.xor` are width-closed.

```text id="0m2hxj"
i8 & i8  → i8
i1 ^ i1  → i1
i8 & i32 → i32
```

This must match interpreter and JIT behavior.

## Dynamic Effects

Some opcodes cannot always be checked statically.

Examples:

* `CALL` through a dynamic `ref`
* `RETURN_CALL` through a dynamic `ref`
* stack-counted `MAP_NEW`
* `CLOSURE_NEW`
* extension or future dynamic operations

When stack effect cannot be determined statically, the stack pass stops without a verdict for that function.

Structural, control-flow, and termination checks still apply.

The interpreter then owns the remaining runtime checks.

This is intentional. The verifier should avoid false rejection.

## Calls

The verifier resolves `CALL` and `RETURN_CALL` arity when the callee type is statically recoverable.

Recoverable cases include:

* function constants
* closure constants
* typed params
* typed locals
* typed upvalues

If the callee is dynamic and its function type cannot be recovered, verification does not guess. Runtime call checks handle it.

## What Verification Does Not Check

These are runtime concerns, not verifier failures:

* global indexes, because globals can grow dynamically
* heap exhaustion
* fuel exhaustion
* context cancellation
* divide-by-zero
* array, string, or map index traps
* failed casts depending on runtime values
* host-function errors
* allocation failures
* dynamic call target mismatch

Do not move runtime policy into the verifier unless the error is statically malformed bytecode.

## Design Notes

The verifier is intentionally in `program`, not `interp`.

Reasons:

* bytecode can be checked before interpreter construction
* verification is a trust-boundary concern
* `interp.New` remains fast and trusting
* `program` avoids importing `analysis` or `pass`
* the verifier can keep its own small CFG logic without package cycles

Keep the verifier cohesive. Avoid scattering verification logic across packages unless there is a clear ownership reason.

## Agent Notes

When changing verification:

* keep structural validation strict
* keep stack validation conservative
* update opcode stack effects and verifier cases together
* keep `i1`/`i8`/`i32` representation compatibility
* reject definite malformed bytecode
* do not reject dynamic behavior merely because it is unknown
* preserve `program.Verify` as the trust boundary
* keep `interp.New` free of implicit verification
* test malformed bytecode, bad targets, bad indexes, stack underflow, and type mismatch separately
* keep verifier, instruction docs, interpreter, and tests in sync

The best verifier change is strict about bytecode shape, conservative about dynamic behavior, and simple enough to trust before execution.
