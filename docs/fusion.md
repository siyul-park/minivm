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
| native execution | `interp/` |

## Model

`catalog` orders patterns. `resolve` derives source kinds from opcode metadata. `compose` and standalone `lower` use the same `lowerers` table. Patterns select sequences and compile-time guards; they `MUST NOT` define opcode semantics.

Each opcode `MUST` have one `lowerers` entry and one semantic emitter.

Fusion state records stack checks, evaluation, ownership, materialization, and the first absorbed opcode.

Standalone execution `MUST` materialize the same values that fusion passes directly. Resident stack refs are consumed; local/global/upvalue/constant refs are borrowed until materialization.

The absorbed head advances threaded compilation by its width. Runtime results, stack/frame state, IPs, traps (except resource limits; see Trap offsets), control flow, and ownership therefore match unfused execution. NOP run compaction is separate dispatch compaction.

## Trap offsets

A fused handler keeps the frame IP at its first instruction until it ends, so a trap is located after the fact and the non-faulting path writes nothing extra. A trace names the instruction that faulted, as unfused execution and the JIT do.

- Each `fusions` entry returns the offset of the pattern's trapping consumer with its handler; `Compile` keeps the nonzero offsets in `threader.traps`, indexed by IP, and `Interpreter.compile` hands them to `Interpreter.traps`. A pattern whose trapping sources `commit` already advances the IP per source, so it records zero.
- A panic that reaches `dispatch` takes that offset when the frame still runs the fused code. This covers the consumer's own checks and every helper it calls (`arrayGet`, `arraySet`, `structGet`, `alloc`, host functions).
- A fused producer that traps ahead of its consumer (stack overflow, slot bounds) panics `trapped` with its own offset instead; `lower.go` `fail` and `overflowAt` emit it. Standalone handlers panic plain.
- `dispatch` applies the offset once. A rethrow across an outer floor carries a zero offset.
- Fusion skips temporaries: a fused handler pushes no transient operands and reads a wide `i64.const` raw, without its box. Against `WithStack` and `WithHeapLimit`, it can therefore trap later than unfused execution, or not at all. Such a resource limit is the one exception to trap parity, and native code, which holds wide values unboxed, has the same exception: it reports the same frames at a possibly different instruction.

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

Threaded fusion remains in the interpreter. Native code consumes the resulting bytecode semantics and does not own fusion.

## Generation

`make generate` updates `interp/threaded.go`; `make check-generated` detects stale output. Generated output `MUST` contain no timestamps or absolute paths.

An opcode change `MUST` use one `lowerers` entry. Patterns `MAY` select sequences and compile-time guards but `MUST NOT` add runtime pattern objects, callbacks, synthetic opcodes, code strings, or target logic.

A fused source `MAY` borrow a ref only while the sequence fully consumes it. Borrowed refs `MUST NOT` cross stack, frame, global/upvalue, call, yield, or control-flow boundaries. `REF_NULL` `MAY` omit balanced ownership work; `DUP` `MAY` omit temporary ownership when locally consumed.

## Related

- `instruction-set.md`
- `memory-model.md`
- `jit-internals.md`
