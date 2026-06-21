# Verification

Static, pre-execution validation of bytecode. Lives in `verify/`.

## Agent Usage

Read when changing the verifier, adding an opcode, or changing how untrusted
bytecode is admitted.

- Per-opcode stack effects: `verify/effect.go` (`signature`). A new opcode that
  is not handled case-by-case in `verify/verify.go` `checker.step` must get a
  row here.
- Abstract stack lattice: `verify/stack.go`.
- Driver and passes: `verify/verify.go`.
- Entry points: `interp.New` with `interp.WithVerify(true)`; `verify.Verify`
  directly. The `run` CLI subcommand verifies every loaded file.

## Why

minivm targets safe plugin / DSL / rules execution. Like WebAssembly and the
JVM, it must be able to reject malformed or hostile bytecode *before* running
it. `program.New` and `interp.New` (with verification off) trust the producer;
the verifier supplies the trust boundary for untrusted bytecode so a bad module
fails fast with a typed error instead of trapping mid-run, corrupting the
operand stack, or panicking the host.

## API

```go
err := verify.Verify(prog, verify.WithExtensions(ids...)) // nil or *verify.VerifyError

vm, err := interp.New(prog, interp.WithVerify(true))       // returns the VerifyError
```

`interp.New` always returns `(*Interpreter, error)`. With `WithVerify` off
(the default) the error is always nil and no verification cost is paid; enable
it for untrusted or externally loaded bytecode. `WithVerify` passes the
installed extension registry's ids through as `verify.WithExtensions`.

`VerifyError` locates the first violation by function slot (0 = top-level code,
`j+1` = constant `j`, matching the interpreter's compiled layout), instruction
byte offset, and opcode, and wraps a sentinel (`errors.Is`-compatible):
`ErrTruncated`, `ErrUnknownOpcode`, `ErrUnknownExtension`, `ErrIndexOutOfRange`,
`ErrStackUnderflow`, `ErrStackMismatch`, `ErrTypeMismatch`, `ErrFallThrough`,
plus `analysis.ErrInvalidJump` from the CFG pass.

## What it checks

Each function slot is verified in four passes (`checker.run`):

1. **Structure** — every instruction decodes within bounds (no truncated
   trailing instruction, no variable-width count byte past the code), names a
   defined opcode (`instr.Valid`), and carries in-range operand indices:
   `CONST_GET` into `Constants`; type-index ops (`REF_TEST`/`REF_CAST`,
   `ARRAY_NEW*`/`STRUCT_NEW*`/`MAP_NEW*`) into `Types`; `LOCAL_*` into the
   param+local list; `UPVAL_*` into `Captures`; `EXT` against the known
   extension ids when supplied.
2. **Control flow** — `analysis.BasicBlocksAnalysis` builds the CFG, which also
   proves every branch target lands on an instruction boundary.
3. **Termination** — every exit block of a *function body* ends in `RETURN`,
   `RETURN_CALL`, or `UNREACHABLE`. Top-level code (slot 0) is exempt: the
   interpreter ends it by running off the end of the code.
4. **Operand stack** — an abstract interpretation over the CFG to a fixpoint
   checks for underflow, operand type confusion, and stack-height disagreement
   at control-flow merges. Kinds reuse `types.Kind` plus a top element
   (`anyKind`) for statically-unknown values; merges widen to `anyKind`, so the
   verifier rejects only on a *definite* concrete mismatch, never on an unknown.

## Limits (by design)

minivm's bytecode is not fully statically stack-typed the way WebAssembly is
(no type operand on `CALL`, no block types, a stack-counted `MAP_NEW`). The
verifier resolves a `CALL`/`RETURN_CALL` arity from the callee's static
`*types.FunctionType` when it is recoverable (a function/closure constant, or a
typed param/local/upval). When an effect cannot be determined statically — a
call through a dynamic `ref` slot, the stack-counted `MAP_NEW`, `CLOSURE_NEW`,
or an extension op — the stack pass stops without a verdict for that function.
The structural passes (1–3) still hold, and the interpreter guards the rest at
runtime. Consequently the stack pass never produces a false rejection.

Out of scope, because they are already runtime-guarded or dynamic: `GLOBAL_*`
indices (globals grow dynamically and are bounds-checked at run time), heap
exhaustion, fuel, divide-by-zero, and array/map index traps. These are
intentional runtime traps, not verification failures.
