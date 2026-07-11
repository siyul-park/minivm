# Fusion

How minivm generates producer-consumer fusion for the threaded interpreter and ARM64 JIT.

## When to Read

Read this before changing fusion rules, generated fusion output, threaded lookahead, ARM64 fusion lowering, or ref ownership inside a fused sequence.

## Source of Truth

| Concern | File |
|---|---|
| Rule declarations and validation | `internal/cmd/genfusion/` |
| Threaded generated handlers | `interp/fusion_gen.go` |
| ARM64 generated lowering | `interp/jit_fusion_gen_arm64.go` |
| Standalone semantics | `interp/threaded.go` |
| Opcode metadata | `instr/type.go` |

## Model

Fusion rules are passive generator data. Products expand into concrete opcode patterns before validation and rendering. Generated matchers use opcode widths from `instr.Type`, choose the longest applicable specialization, and dispatch directly to closed renderers. Product identities and runtime match/action objects do not survive generation.

Standalone opcode execution is the semantic oracle. Fusion must preserve results, stack and frame state, instruction pointers, traps and check order, control flow, and final ownership. NOP run compaction remains local to the NOP handler because it is dispatch compaction, not semantic producer-consumer fusion.

## Support Matrix

| Pattern family | Threaded | ARM64 |
|---|---:|---:|
| Slot or non-string constant + `DROP` / `REF_IS_NULL` | yes | yes |
| `REF_NULL` or `DUP` + `DROP` / `REF_IS_NULL` | yes | yes |
| Constant direct `CALL` / `RETURN_CALL` | yes | yes |
| Constant `CLOSURE_NEW` | yes | no |
| Eligible numeric source + operation or comparison | yes | no |
| Numeric comparison + `BR_IF` | yes | no |
| Immediate or I32 constant + aggregate get | yes | no |

`BR_IF` may extend eligible threaded null and numeric comparisons. Integer division and remainder stay standalone because a mid-sequence trap must preserve the original instruction pointer and stack state. ARM64 supports null checks but leaves fused conditional branches and numeric families to ordinary trace lowering. Unsupported patterns always retain standalone behavior.

## Threaded Compilation

Threading calls one generated fusion matcher before standalone opcode dispatch. Matcher preflight uses a local cursor and does not mutate compiler state on a miss. A match installs one direct handler and advances compile-time IP by only the first opcode width. Absorbed offsets are still threaded separately, so branches into them execute standalone handlers. Exact threading disables fusion.

Compile-time specialization resolves operands, declared slot kinds, constants, heap objects, and cached coroutine metadata. Final handlers do not decode operands, dispatch rules, inspect concrete heap types, or rescan bytecode for yields.

## ARM64 Lowering

ARM64 matching runs after frame validation and loop-backedge handling, before standalone opcode lowering. Its result contract is:

| Result | Meaning |
|---|---|
| `(0, true)` | no specialization; continue standalone lowering |
| `(n, true)` | committed `n` recorded steps |
| `(0, false)` | selected commit failed; reject native compilation |

Preflight may read trace steps, bytecode, constants, heap values, targets, and metadata. It must not mutate symbolic values, allocate registers, emit instructions, create exits, or update journals. Commit begins only after selecting the longest applicable specialization; failure never falls back to a shorter rule. Existing loop-backedge lowering retains priority. Fused conditional lowering materializes its condition explicitly before using the shared branch path.

## Ref Ownership

RC elimination is local and proven by each closed renderer. A slot or constant source may be borrowed only when its ref is fully consumed inside the fused sequence. `REF_NULL` may omit its balanced retain/release, and `DUP` may avoid creating temporary ownership when its duplicate is consumed locally.

Borrowed refs never enter the VM stack, symbolic value stack, frame/global/upvalue storage, side exits, deoptimization snapshots, calls, yields, or control-flow boundaries. String constants remain standalone because loading interns them. Declared I64 slots retain numeric ownership rules even when a large current value is heap-promoted.

## Generation and Checks

`make generate` refreshes generated files. Generated output has stable ordering and contains no timestamp or absolute path. `make check-generated` reports stale output without rewriting it. `make check` also verifies module tidiness, formatting, vet, race tests, native build, and Linux ARM64 production/test compilation.

## Maintenance Notes

Add a pattern only when every expanded concrete combination has one supported renderer and locally obvious ownership. Reject ambiguous, shadowed, variable-width, backend-incomplete, or ownership-unsafe patterns during generation. Do not add callbacks, code strings, ownership annotations, synthetic opcodes, or runtime rule objects.

## Related Docs

- `docs/jit-internals.md` - trace lowering, side exits, and ARM64 contracts
- `docs/memory-model.md` - refcounts, heap roots, and ownership invariants
- `docs/instruction-set.md` - opcode semantics and operand widths
