# Fusion

How minivm generates producer-consumer fusion for the threaded interpreter.

## When to Read

Read this before changing threaded fusion patterns, generated handlers, lookahead, or ref ownership inside a fused sequence.

## Source of Truth

| Concern | File |
|---|---|
| Fusion composition engine | `internal/codegen/lower.go` |
| Opcode lowering by domain | `internal/codegen/array.go`, `call.go`, `control.go`, `coroutine.go`, `map.go`, `numeric.go`, `ref.go`, `slot.go`, `string.go`, `struct.go`, `unary.go` |
| Fusion pattern catalog | `internal/codegen/pattern.go` |
| Source generation | `internal/codegen/generate.go`, `internal/codegen/threader.go` |
| Pattern validation | `internal/codegen/validate.go` |
| Standalone and fused threaded handlers | `interp/threaded.go` |
| ARM64 trace lowering | `internal/jit/arm64/` |
| Opcode metadata | `instr/type.go` |

## Model

Fusion patterns are concrete opcode sequences used only during generation. `catalog` builds and orders them before validation. `resolve` infers source kinds from opcode stack metadata and produces immutable `step` values. `compose` walks those steps once with a `state` and invokes each opcode's single entry in `lowerers`. `lower` invokes the same table with standalone state. Pattern declarations select sequences and compile-time guards only; no runtime pattern or action object survives generation.

Every valid opcode has exactly one `lowerers` entry and one semantic emitter. The state's virtual stack contains `value` entries describing compile-time checks, runtime evaluation, ownership cleanup, optional stack materialization, and the first absorbed opcode. Standalone execution materializes the same values that fusion can pass directly to a consumer. Resident stack operands are consumed, while local, global, upvalue, and constant sources are borrowed until materialized. The `head` opcode advances threaded compilation by the source instruction width even when a later consumer produces the final value. This keeps results, stack and frame state, instruction pointers, traps and check order, control flow, and ownership aligned with exact execution. NOP run compaction remains local to the NOP handler because it is dispatch compaction, not semantic producer-consumer fusion.

## Support Matrix

The generator validates the concrete patterns returned by `catalog` in `internal/codegen/pattern.go`.

Patterns cover ref consumption, constant calls and closure creation, numeric operations and comparisons, conditional branches, constant aggregate indexes, direct non-trapping arithmetic stores to typed locals, and typed-array constants or typed-array `LOCAL_GET`/`GLOBAL_GET`/`UPVAL_GET` containers indexed by scalar producers. A typed-array container is proven at threading time from its declared type (`*types.ArrayType.ElemKind`) — `localTypes`, `globalTypes`, or `captureTypes` depending on the source opcode — not from its current runtime value. The same three container sources also fuse `array.set`, with a scalar index producer and a scalar value producer, scoped to the six primitive `TypedArray[T]` element kinds (`bool`, `int8`, `int32`, `int64`, `float32`, `float64`) so no store ever overwrites a ref element and `docs/memory-model.md`'s retain-new/release-old rule never applies. A fused container is borrowed, not consumed: it was never pushed, so the fused store neither retains nor releases it and leaves the operand stack unchanged, which is the same net effect the unfused three-push/three-pop sequence has. Trapping numeric operations materialize completed sources before evaluating the trap so stack ownership and instruction offsets match exact execution.

A `struct.get` container may also be a `LOCAL_GET`, `GLOBAL_GET`, or `UPVAL_GET` whose declared type is a concrete `*types.StructType`, with a compile-time constant field index (`I32_CONST` or a `CONST_GET` constant). Unlike a typed array, a struct field's Kind depends on which `StructType` the container declares, not on a Go type the catalog can name ahead of time, so the generated handler resolves the declared type's field Kind with a switch once at threading time and specializes one closure per Kind — the resulting handler boxes the field directly with no switch of its own. The runtime guards this replaces (ref kind, heap value type, field bounds against the *runtime* struct, and the runtime field's actual Kind) still run on every execution in the same order the unfused sequence checks them; a mismatch on any of them, or a field index outside the *declared* type's own fields, rejects the fusion or traps exactly as the unfused sequence would. `struct.set` containers remain unfused.

A specialized-type miss on either fusion (e.g. a generic `*types.Array` heap value under a container declared with a concrete element type) falls back to `(*Interpreter).arrayGet`, `(*Interpreter).arraySet`, or `(*Interpreter).structField` in `interp/interp.go` instead of trapping a case the unfused handler accepts, regardless of which of the three container sources it came from. The standalone `ARRAY_GET`/`ARRAY_SET`/`STRUCT_GET` handler calls the same method unconditionally, so the generic aggregate-read dispatch is one hand-written runtime method shared by every fused and unfused caller instead of a switch duplicated into generated code.

## Threaded Compilation

Threading checks the opcode-indexed fusion table before standalone opcode dispatch. Fusion preflight uses a local cursor and mutates nothing on a miss. A match installs one direct handler and advances compile-time IP by only the first opcode width. Absorbed offsets are still threaded separately, so branches into them execute standalone handlers. Exact threading disables fusion.

Compile-time specialization resolves operands, declared slot kinds, constants, heap objects, and cached coroutine metadata. Final handlers do not dispatch source functions, decode operands, or rescan bytecode for yields. A typed-array constant load still validates the current heap value's concrete slice type and bounds on every execution; specialization removes temporary stack materialization and balanced container retain/release work, not those runtime guards.

## JIT Separation

The generator does not emit architecture-specific code or tests. ARM64 trace fusion is ordinary lowering code in `internal/jit/arm64`, next to the standalone operations it combines. This keeps JIT selection, guards, symbolic stack mutation, and emitted instructions in one implementation instead of mirroring them through generator metadata.

Threaded patterns are not a cross-backend registry. An ARM64 specialization is added only when trace lowering benefits from it and is tested through JIT behavior.

## Ref Ownership

RC elimination is local and proven by each closed lowering. A slot or constant source may be borrowed only when its ref is fully consumed inside the fused sequence. `REF_NULL` may omit its balanced retain/release, and `DUP` may avoid creating temporary ownership when its duplicate is consumed locally.

Borrowed refs never enter the VM stack, frame/global/upvalue storage, calls, yields, or control-flow boundaries. String constants remain standalone because the constant pool deduplicates them at load. Declared I64 slots retain numeric ownership semantics even when a large current value is heap-promoted.

## Generation and Checks

`make generate` refreshes `interp/threaded.go`. This document is maintained manually. Generated output has stable ordering and contains no timestamp or absolute path. `make check-generated` reports stale output without rewriting it. `make check` also verifies module tidiness, formatting, vet, race tests, native build, and Linux ARM64 production/test compilation.

## Maintenance Notes

Add or change an opcode through its single `lowerers` entry in `lower.go`. The emitter must work with both standalone and composable `state` values; do not add a standalone copy or a fusion-only renderer. Add a pattern in `pattern.go` only to select a concrete sequence and compile-time guards. Source kinds and stack limits come from `instr.Type` metadata, with explicit input rules only for ownership-sensitive ref consumers and dynamically typed call or aggregate-index consumers. Preserve the first absorbed step independently from the value-producing step. Reject ambiguous, variable-width, stack-inconsistent, unresolved, or ownership-unsafe patterns during generation. Do not add callbacks, code strings, synthetic opcodes, runtime pattern objects, or architecture-specific output.

Keep ARM64 trace fusion hand-written in `internal/jit/arm64/`. Do not add architecture flags or backends to this generator.

## Related Docs

- `docs/jit-internals.md` - trace lowering, side exits, and ARM64 contracts
- `docs/memory-model.md` - refcounts, heap roots, and ownership invariants
- `docs/instruction-set.md` - opcode semantics and operand widths
