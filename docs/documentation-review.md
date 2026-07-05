# Documentation Review Plan

This document records the documentation review criteria used for this pass and the follow-up plan for keeping the docs aligned with the codebase.

## Scope

Review repository documentation against the implementation and contributor-facing routing docs, especially:

- top-level `README.md` and `README.ko.md`
- `AGENTS.md` and agent/task routing guidance
- topic docs under `docs/`
- code sources that act as documentation anchors, such as `instr/type.go`, `docs/architecture.md`, `docs/benchmarks.md`, and `go.mod`

## Findings

### Stale or incomplete public summaries

The top-level README instruction-set summary had fallen behind the implemented opcode surface. The implementation exposes additional opcode families for `return_call`, coroutines, upvalues, ref cells, string iterators/encoding, array append/delete/slice, maps, closures, and structured errors.

The Korean README also lagged behind the English README in its JIT explanation and follow-up links. It omitted recent trace-continuation wording, error-related trace-terminal notes, and the profile documentation link.

### Missing entry-point guidance

The README introduced execution and optimization, but did not show the verifier entry point for untrusted or externally produced bytecode. Since the verifier is separate from `interp.New`, public docs should make the explicit `program.Verify` step easy to find.

### Cross-document navigation gaps

`AGENTS.md` has a useful documentation index and task router, while the README only linked a few docs opportunistically. The README should stay short, but it should point readers to the canonical opcode reference when its summary is intentionally compressed.

### Format drift

The top-level English and Korean READMEs should keep the same section shape and link set. When one language version receives updated capability or JIT wording, the other should receive the matching change in the same PR.

### Overly dense explanations

The JIT and performance sections carry high-value details, but some paragraphs mix product positioning, implementation internals, and benchmark interpretation. Keep the README concise and move deeper explanation to `docs/jit-internals.md` and `docs/benchmarks.md` when the text grows further.

## Applied in this PR

- Expanded the README capability summary to include maps, coroutines, and structured errors.
- Added a short verifier usage section for untrusted bytecode.
- Updated the instruction-set summary to match the implemented opcode families.
- Linked the README opcode summary to `docs/instruction-set.md` as the canonical reference.
- Brought the Korean README back in sync with the English README shape, JIT wording, verifier guidance, opcode summary, and profile link.

## Follow-up Plan

### P0 — Keep top-level docs accurate

- Keep `README.md` and `README.ko.md` structurally aligned.
- Keep README opcode families synchronized with `instr/type.go`.
- Keep the README as a concise overview, not the source of truth for per-opcode semantics.

### P1 — Make canonical docs easier to validate

- Treat `docs/instruction-set.md` as the source of truth for stack effects, operand widths, and JIT status.
- Consider generating or checking the opcode table from `instr/type.go` to prevent future drift.
- Ensure verifier docs mention which checks are static, best-effort, or runtime-only.

### P1 — Normalize topic-doc shape

For long-lived topic docs, prefer this shape:

1. what the doc covers
2. when to read it
3. source-of-truth code paths
4. key invariants or APIs
5. links to adjacent docs

### P2 — Reduce duplication

- Keep benchmark numbers in `docs/benchmarks.md`; let the README quote only the headline comparison.
- Keep detailed trace-JIT mechanics in `docs/jit-internals.md`; let the README describe behavior at a high level.
- Keep agent workflow details in `AGENTS.md`; link from topic docs only when task routing matters.

### P2 — Improve document graph

- Add short “Related docs” sections where topic docs currently stand alone.
- Prefer links between architecture, memory model, value representation, JIT internals, instruction set, verification, profiling, and host integration over repeating the same explanation.
