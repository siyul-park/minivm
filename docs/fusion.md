# Fusion

How minivm generates producer-consumer fusion for the threaded interpreter.

## When to Read

Read this before changing threaded fusion patterns, generated handlers, lookahead, or ref ownership inside a fused sequence.

## Source of Truth

| Concern | File |
|---|---|
| Opcode lowering | `internal/cmd/geninterp/lower.go` |
| Fusion pattern catalog | `internal/cmd/geninterp/pattern.go` |
| Source generation | `internal/cmd/geninterp/generate.go` |
| Pattern validation | `internal/cmd/geninterp/validate.go` |
| Standalone and fused threaded handlers | `interp/threaded.go` |
| ARM64 trace lowering | `interp/jit_arm64.go` |
| Opcode metadata | `instr/type.go` |

## Model

Fusion patterns are concrete opcode sequences used only during generation. `catalog` builds and orders them before validation. `prepare` infers source kinds from opcode stack metadata and produces immutable `fact` values. `compose` walks those facts once with a `loweringState` and invokes each opcode's single entry in `lowerers`. `standalone` invokes the same table with a standalone state. Pattern declarations select sequences and compile-time guards only; no runtime pattern or action object survives generation.

Every valid opcode has exactly one `lowerers` entry and one semantic emitter. Pending `lowering` values describe compile-time checks, runtime evaluation, ownership cleanup, and optional stack materialization. Standalone execution materializes the same values that fusion can pass directly to a consumer. Numeric stack operands are consumed, while local, global, upvalue, and constant sources are borrowed until materialized. This keeps results, stack and frame state, instruction pointers, traps and check order, control flow, and ownership aligned with exact execution. NOP run compaction remains local to the NOP handler because it is dispatch compaction, not semantic producer-consumer fusion.

## Support Matrix

The generator validates the concrete patterns returned by `catalog` in `internal/cmd/geninterp/pattern.go`.

Patterns cover ref consumption, constant calls and closure creation, numeric operations and comparisons, conditional branches, and constant aggregate indexes. Trapping numeric operations materialize completed sources before evaluating the trap so stack ownership and instruction offsets match exact execution.

## Threaded Compilation

Threading checks the opcode-indexed fusion table before standalone opcode dispatch. Fusion preflight uses a local cursor and mutates nothing on a miss. A match installs one direct handler and advances compile-time IP by only the first opcode width. Absorbed offsets are still threaded separately, so branches into them execute standalone handlers. Exact threading disables fusion.

Compile-time specialization resolves operands, declared slot kinds, constants, heap objects, and cached coroutine metadata. Final handlers do not dispatch source functions, decode operands, inspect concrete heap types, or rescan bytecode for yields.

## JIT Separation

The generator does not emit architecture-specific code or tests. ARM64 trace fusion is ordinary lowering code in `interp/jit_arm64.go`, next to the standalone operations it combines. This keeps JIT selection, guards, symbolic stack mutation, and emitted instructions in one implementation instead of mirroring them through generator metadata.

Threaded patterns are not a cross-backend registry. An ARM64 specialization is added only when trace lowering benefits from it and is tested through JIT behavior.

## Ref Ownership

RC elimination is local and proven by each closed lowering. A slot or constant source may be borrowed only when its ref is fully consumed inside the fused sequence. `REF_NULL` may omit its balanced retain/release, and `DUP` may avoid creating temporary ownership when its duplicate is consumed locally.

Borrowed refs never enter the VM stack, frame/global/upvalue storage, calls, yields, or control-flow boundaries. String constants remain standalone because loading interns them. Declared I64 slots retain numeric ownership semantics even when a large current value is heap-promoted.

## Generation and Checks

`make generate` refreshes `interp/threaded.go`. This document is maintained manually. Generated output has stable ordering and contains no timestamp or absolute path. `make check-generated` reports stale output without rewriting it. `make check` also verifies module tidiness, formatting, vet, race tests, native build, and Linux ARM64 production/test compilation.

## Maintenance Notes

Add or change an opcode through its single `lowerers` entry in `lower.go`. The emitter must work with both standalone and composable `loweringState` values; do not add a standalone copy or a fusion-only renderer. Add a pattern in `pattern.go` only to select a concrete sequence and compile-time guards. Source kinds and stack limits are inferred from `instr.Type` metadata, with explicit handling only for dynamically typed call and aggregate-index consumers. Reject ambiguous, variable-width, stack-inconsistent, or ownership-unsafe patterns during generation. Do not add callbacks, code strings, synthetic opcodes, runtime pattern objects, or architecture-specific output.

Keep ARM64 trace fusion hand-written in `interp/jit_arm64.go`. Do not add architecture flags or backends to this generator.

## Related Docs

- `docs/jit-internals.md` - trace lowering, side exits, and ARM64 contracts
- `docs/memory-model.md` - refcounts, heap roots, and ownership invariants
- `docs/instruction-set.md` - opcode semantics and operand widths
