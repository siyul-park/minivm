# Fusion

Generated producer-consumer fusion for threaded execution.

## Ownership

| Concern | Owner |
|---|---|
| composition | `internal/codegen/lower.go` |
| domain lowerers | `internal/codegen/*.go` |
| patterns | `internal/codegen/pattern.go` |
| generation | `internal/codegen/generate.go`, `threader.go` |
| validation | `internal/codegen/validate.go` |
| runtime handlers | `interp/threaded.go` |
| ARM64 fusion | `interp/` |

## Model

`catalog` orders patterns. `resolve` derives source kinds from opcode metadata. `compose` and standalone `lower` use the same `lowerers` table. Patterns select sequences and compile-time guards; they `MUST NOT` define opcode semantics.

Each opcode `MUST` have one `lowerers` entry and one semantic emitter.

Fusion state records stack checks, evaluation, ownership, materialization, and the first absorbed opcode.

Standalone execution `MUST` materialize the same values that fusion passes directly. Resident stack refs are consumed; local/global/upvalue/constant refs are borrowed until materialization.

The absorbed head advances threaded compilation by its width. Runtime results, stack/frame state, IPs, traps, control flow, and ownership therefore match unfused execution. NOP run compaction is separate dispatch compaction.

## Supported Patterns

Current patterns include:

- primitive constants/locals feeding primitive operations;
- constant calls and closure creation;
- conditional branches and scalar comparisons;
- constant aggregate indexes;
- non-trapping arithmetic stored into typed locals;
- typed-array `LOCAL_GET`/`GLOBAL_GET`/`UPVAL_GET` containers feeding `array.get`/`array.set`;
- typed-array constants feeding `array.get`;
- concrete `StructType` containers feeding constant-index `struct.get`.

Typed-array stores cover `bool`, `int8`, `int32`, `int64`, `float32`, `float64`; refs remain unfused. Containers are borrowed, so fused stores perform no container retain/release.

`struct.get` specializes the declared field kind at threading time. Runtime checks still validate ref kind, concrete type, bounds, and field kind.

Specialized-type misses `MUST` use the same interpreter methods as standalone handlers.

## Compilation

Threading checks the opcode-indexed table with a local cursor. A miss `MUST` mutate nothing. A match installs one direct handler and advances compile-time IP by the first opcode width; absorbed offsets remain separately threaded.

Exact mode disables fusion. Runtime guards retain bounds, segmentation, type, and underflow checks. Trapping numeric operations `MUST` materialize completed sources before the trap.

## JIT

Threaded fusion remains in the interpreter. Native fusion can be reconsidered as part of the planned rebuild.

## Ownership

A fused source `MAY` borrow a ref only while the sequence fully consumes it.

Borrowed refs `MUST NOT` cross stack, frame, global/upvalue, call, yield, or control-flow boundaries. `REF_NULL` `MAY` omit balanced ownership work. `DUP` `MAY` avoid temporary ownership when locally consumed.

## Generation

`make generate` updates `interp/threaded.go`; `make check-generated` detects stale output. Generated code contains no timestamps or absolute paths.

The agent `MUST` change an opcode through one `lowerers` entry. Patterns `MAY` select sequences and compile-time guards but `MUST NOT` add runtime pattern objects, callbacks, synthetic opcodes, code strings, or target-specific logic.

## Related

- `instruction-set.md`
- `memory-model.md`
- `jit-internals.md`
